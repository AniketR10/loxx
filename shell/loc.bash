# loc shell integration for bash (bash 4.4+). Load it at the END of ~/.bashrc:
#   eval "$(loc init bash)"
#
# After every command, `loc record` runs in the background, so the prompt never
# waits for it. Commands starting with a space are not recorded. bash has no
# native preexec hook, so this uses bash-preexec (below), which relies on
# PROMPT_COMMAND and, before bash 5.3, the DEBUG trap.

_loc_bin=__LOC_BIN__

# bash-preexec 0.7.0, vendored from https://github.com/rcaloras/bash-preexec
__BASH_PREEXEC_LICENSE__
# It is sourced from a here-document so its top-level `return` statements end
# only this block, never the rest of ~/.bashrc.
if [[ -z "${bash_preexec_imported:-}${__bp_imported:-}" ]]; then
source /dev/stdin <<'__LOC_BASH_PREEXEC__'
__BASH_PREEXEC__
__LOC_BASH_PREEXEC__
fi

# bash-preexec removes "ignorespace" from HISTCONTROL so it can see every
# command. That would make bash save space-prefixed commands to its history
# file, so remember whether the user asked for ignorespace, and _loc_preexec
# deletes those commands from bash's history itself.
if declare -F __bp_adjust_histcontrol >/dev/null && ! declare -F _loc_bp_adjust_histcontrol >/dev/null; then
  _loc_def=$(declare -f __bp_adjust_histcontrol)
  eval "_loc_bp_adjust_histcontrol ${_loc_def#__bp_adjust_histcontrol }"
  unset _loc_def
  __bp_adjust_histcontrol() {
    case ":${HISTCONTROL:-}:" in
      *:ignorespace:* | *:ignoreboth:*) _loc_ignorespace=1 ;;
    esac
    _loc_bp_adjust_histcontrol
  }
fi

_loc_session="$$-${EPOCHREALTIME:-$(printf '%(%s)T' -1)}"
_loc_cmd='' _loc_start='' _loc_cwd='' _loc_t=''

# _loc_now sets _loc_t to the current time: microseconds on bash 5+
# ($EPOCHREALTIME), whole seconds on bash 4.4, without starting a process.
_loc_now() {
  if [[ -n ${EPOCHREALTIME:-} ]]; then
    _loc_t=$EPOCHREALTIME
  else
    printf -v _loc_t '%(%s)T' -1
  fi
}

_loc_preexec() {
  _loc_cmd=$1
  _loc_now
  _loc_start=$_loc_t
  _loc_cwd=$PWD
  if [[ -n ${_loc_ignorespace:-} && $1 == ' '* ]]; then
    local entry
    entry=$(HISTTIMEFORMAT='' builtin history 1)
    entry=${entry#"${entry%%[![:space:]]*}"}
    builtin history -d "${entry%%[!0-9]*}" 2>/dev/null
  fi
}

_loc_precmd() {
  local exit_code=$? # must be first: anything else overwrites $?
  [[ -n $_loc_cmd ]] || return 0 # empty line, or the first prompt
  _loc_now
  # The subshell keeps bash from printing job-control messages.
  ("$_loc_bin" record --exit "$exit_code" --start "$_loc_start" --end "$_loc_t" \
    --cwd "$_loc_cwd" --session "$_loc_session" -- "$_loc_cmd" >/dev/null 2>&1 &)
  _loc_cmd=''
}

# If the shell exits while a command is still running (e.g. the terminal
# window is closed during ssh or tmux), precmd never runs; record the command
# on exit instead, with its exit code and duration unknown.
_loc_exit() {
  [[ -n $_loc_cmd ]] || return 0
  "$_loc_bin" record --start "$_loc_start" --cwd "$_loc_cwd" \
    --session "$_loc_session" -- "$_loc_cmd" </dev/null >/dev/null 2>&1
  _loc_cmd=''
}
if [[ -z ${_loc_exit_installed:-} ]]; then
  _loc_exit_installed=1
  # Keep any EXIT trap that was already set.
  eval "_loc_trap_argv=($(trap -p EXIT))"
  _loc_prev_exit_trap=${_loc_trap_argv[2]:-}
  unset _loc_trap_argv
  trap '_loc_exit; eval "$_loc_prev_exit_trap"' EXIT
fi

[[ " ${preexec_functions[*]-} " == *" _loc_preexec "* ]] || preexec_functions+=(_loc_preexec)
[[ " ${precmd_functions[*]-} " == *" _loc_precmd "* ]] || precmd_functions+=(_loc_precmd)
