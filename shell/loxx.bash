# loxx hook for bash 4.4+. Load at the end of ~/.bashrc: eval "$(loxx init bash)"
# bash-preexec (below) provides the preexec hook bash lacks.

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

# bash-preexec turns ignorespace off; keep space-prefixed commands out of history.
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

# Shell exiting mid-command (window closed): record it without exit code.
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

# `loxx` alone opens the search panel; with arguments it's the binary.
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

# Enter on a line that isn't a command opens the list with it as the search;
# the choice replaces the line. Off with LOXX_SUGGEST=0.
_loxx_enter() {
  local line=$READLINE_LINE word chosen query
  bind '"\e[1920~": accept-line'
  if [[ $line == loxx ]]; then
    query='' # plain `loxx`: open the list here too, so the choice lands on the line
  else
    [[ ${LOXX_SUGGEST:-1} != 0 && -n $line && $line != [[:space:]]* ]] || return 0
    [[ $line != "${_loxx_declined:-}" ]] || return 0 # Esc'd once: run it as typed
    word=${line%%[[:space:]]*}
    # Paths, assignments and shell syntax are bash's to handle.
    [[ $word == *[/=\$\`\(\)\{\}\<\>\|\&\;\!\'\"\\~*?\[]* ]] && return 0
    type -t -- "$word" >/dev/null && return 0
    query=$line
  fi
  bind '"\e[1920~": redraw-current-line'
  if chosen=$("$_loxx_bin" panel --cwd "$PWD" --session "$_loxx_session" --query "$query" 2>/dev/null) && [[ -n $chosen ]]; then
    READLINE_LINE=$chosen
    READLINE_POINT=${#chosen}
    _loxx_declined=''
  elif [[ -n $query ]]; then
    _loxx_declined=$line
  else
    READLINE_LINE='' # cancelled plain `loxx`: leave an empty line
    READLINE_POINT=0
  fi
}
_loxx_bind_enter() {
  local map
  for map in emacs vi-insert; do
    # Only where Enter is still accept-line.
    bind -m "$map" -p 2>/dev/null | grep -qx '"\\C-m": accept-line' || continue
    bind -m "$map" -x '"\e[1919~": _loxx_enter'
    bind -m "$map" '"\e[1920~": accept-line'
    bind -m "$map" '"\C-m": "\e[1919~\e[1920~"'
  done
}
[[ $- == *i* ]] && _loxx_bind_enter 2>/dev/null

# "● loxx" at the right of the prompt, drawn before it (PS1 untouched).
_loxx_ran=''
_loxx_badge() {
  [[ ${LOXX_QUIET:-0} == 1 || ! -t 1 ]] && return 0
  [[ -n $_loxx_ran ]] || printf '\e7\e[A\e[%dG\e[K\e8' $(( ${COLUMNS:-80} - 5 ))
  _loxx_ran=''
  printf '\e7\e[%dG\e[1;38;5;28m● loxx\e[0m\e8' $(( ${COLUMNS:-80} - 5 ))
}
# Clears the badge from the line just run.
_loxx_badge_ran() {
  _loxx_ran=1
  [[ ${LOXX_QUIET:-0} == 1 || ! -t 1 || $1 == *$'\n'* ]] && return 0
  printf '\e7\e[A\e[%dG\e[K\e8' $(( ${COLUMNS:-80} - 5 ))
}

[[ " ${preexec_functions[*]-} " == *" _loxx_preexec "* ]] || preexec_functions+=(_loxx_preexec)
[[ " ${preexec_functions[*]-} " == *" _loxx_badge_ran "* ]] || preexec_functions+=(_loxx_badge_ran)
[[ " ${precmd_functions[*]-} " == *" _loxx_precmd "* ]] || precmd_functions+=(_loxx_precmd)
[[ " ${precmd_functions[*]-} " == *" _loxx_badge "* ]] || precmd_functions+=(_loxx_badge)

# Startup line, only in an interactive terminal (never in scp or scripts).
if [[ ${LOXX_QUIET:-0} != 1 && $- == *i* && -t 1 ]]; then
  if [[ ${LOXX_SUGGEST:-1} != 0 ]]; then
    printf '\033[2mloxx is on: type what you want to do, like "undo my last commit", and press Enter.\033[0m\n'
  else
    printf '\033[2mloxx is on: type loxx to search your history by meaning.\033[0m\n'
  fi
fi
