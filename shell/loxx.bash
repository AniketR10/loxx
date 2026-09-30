# loxx shell integration for bash (bash 4.4+). Load it at the END of ~/.bashrc:
#   eval "$(loxx init bash)"
#
# After every command, `loxx record` runs in the background, so the prompt never
# waits for it. Commands starting with a space are not recorded. bash has no
# native preexec hook, so this uses bash-preexec (below), which relies on
# PROMPT_COMMAND and, before bash 5.3, the DEBUG trap.

_loxx_bin=__LOXX_BIN__

# bash-preexec 0.7.0, vendored from https://github.com/rcaloras/bash-preexec
__BASH_PREEXEC_LICENSE__
# It is sourced from a here-document so its top-level `return` statements end
# only this block, never the rest of ~/.bashrc.
if [[ -z "${bash_preexec_imported:-}${__bp_imported:-}" ]]; then
source /dev/stdin <<'__LOXX_BASH_PREEXEC__'
__BASH_PREEXEC__
__LOXX_BASH_PREEXEC__
fi

# bash-preexec removes "ignorespace" from HISTCONTROL so it can see every
# command. That would make bash save space-prefixed commands to its history
# file, so remember whether the user asked for ignorespace, and _loxx_preexec
# deletes those commands from bash's history itself.
if declare -F __bp_adjust_histcontrol >/dev/null && ! declare -F _loxx_bp_adjust_histcontrol >/dev/null; then
  _loxx_def=$(declare -f __bp_adjust_histcontrol)
  eval "_loxx_bp_adjust_histcontrol ${_loxx_def#__bp_adjust_histcontrol }"
  unset _loxx_def
  __bp_adjust_histcontrol() {
    case ":${HISTCONTROL:-}:" in
      *:ignorespace:* | *:ignoreboth:*) _loxx_ignorespace=1 ;;
    esac
    _loxx_bp_adjust_histcontrol
  }
fi

_loxx_session="$$-${EPOCHREALTIME:-$(printf '%(%s)T' -1)}"
_loxx_cmd='' _loxx_start='' _loxx_cwd='' _loxx_t=''

# _loxx_now sets _loxx_t to the current time: microseconds on bash 5+
# ($EPOCHREALTIME), whole seconds on bash 4.4, without starting a process.
_loxx_now() {
  if [[ -n ${EPOCHREALTIME:-} ]]; then
    _loxx_t=$EPOCHREALTIME
  else
    printf -v _loxx_t '%(%s)T' -1
  fi
}

_loxx_preexec() {
  _loxx_cmd=$1
  _loxx_now
  _loxx_start=$_loxx_t
  _loxx_cwd=$PWD
  if [[ -n ${_loxx_ignorespace:-} && $1 == ' '* ]]; then
    local entry
    entry=$(HISTTIMEFORMAT='' builtin history 1)
    entry=${entry#"${entry%%[![:space:]]*}"}
    builtin history -d "${entry%%[!0-9]*}" 2>/dev/null
  fi
}

_loxx_precmd() {
  local exit_code=$? # must be first: anything else overwrites $?
  [[ -n $_loxx_cmd ]] || return 0 # empty line, or the first prompt
  _loxx_now
  # The subshell keeps bash from printing job-control messages.
  ("$_loxx_bin" record --exit "$exit_code" --start "$_loxx_start" --end "$_loxx_t" \
    --cwd "$_loxx_cwd" --session "$_loxx_session" -- "$_loxx_cmd" >/dev/null 2>&1 &)
  _loxx_cmd=''
}

# If the shell exits while a command is still running (e.g. the terminal
# window is closed during ssh or tmux), precmd never runs; record the command
# on exit instead, with its exit code and duration unknown.
_loxx_exit() {
  [[ -n $_loxx_cmd ]] || return 0
  "$_loxx_bin" record --start "$_loxx_start" --cwd "$_loxx_cwd" \
    --session "$_loxx_session" -- "$_loxx_cmd" </dev/null >/dev/null 2>&1
  _loxx_cmd=''
}
if [[ -z ${_loxx_exit_installed:-} ]]; then
  _loxx_exit_installed=1
  # Keep any EXIT trap that was already set.
  eval "_loxx_trap_argv=($(trap -p EXIT))"
  _loxx_prev_exit_trap=${_loxx_trap_argv[2]:-}
  unset _loxx_trap_argv
  trap '_loxx_exit; eval "$_loxx_prev_exit_trap"' EXIT
fi

# Typing `loxx` with no arguments opens the search panel. bash cannot pre-fill
# the next prompt, so the chosen command goes into history: one Up arrow away,
# ready to edit. It is never run for you. With arguments, `loxx` is the loxx
# binary (`loxx search …`, `loxx import`, …).
loxx() {
  if (($#)); then
    LOXX_HOOK=bash "$_loxx_bin" "$@" # tells `loxx status` the hook is active here
    return
  fi
  local chosen
  chosen=$("$_loxx_bin" panel --cwd "$PWD" --session "$_loxx_session") || return 0
  builtin history -s -- "$chosen"
  printf '\033[2m↑ to use: %s\033[0m\n' "$chosen" >&2
}

[[ " ${preexec_functions[*]-} " == *" _loxx_preexec "* ]] || preexec_functions+=(_loxx_preexec)
[[ " ${precmd_functions[*]-} " == *" _loxx_precmd "* ]] || precmd_functions+=(_loxx_precmd)
