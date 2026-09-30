# loxx shell integration for zsh (zsh 5.1+). Load it from ~/.zshrc with:
#   eval "$(loxx init zsh)"
#
# After every command, `loxx record` runs in the background (&!), so the prompt
# never waits for it. Commands starting with a space are not recorded.

zmodload zsh/datetime 2>/dev/null # $EPOCHREALTIME

typeset -g _loxx_bin=__LOXX_BIN__
typeset -g _loxx_session="$$-${EPOCHREALTIME:-$EPOCHSECONDS}"
typeset -g _loxx_cmd="" _loxx_start="" _loxx_cwd=""

_loxx_preexec() {
  _loxx_cmd=$1 # the command line as typed
  _loxx_start=${EPOCHREALTIME:-$EPOCHSECONDS}
  _loxx_cwd=$PWD
}

_loxx_precmd() {
  local exit_code=$? # must be first: anything else overwrites $?
  [[ -n $_loxx_cmd ]] || return 0 # empty line, or the first prompt
  "$_loxx_bin" record --exit "$exit_code" --start "$_loxx_start" \
    --end "${EPOCHREALTIME:-$EPOCHSECONDS}" --cwd "$_loxx_cwd" \
    --session "$_loxx_session" -- "$_loxx_cmd" >/dev/null 2>&1 &!
  _loxx_cmd=""
}

# If the shell exits while a command is still running (e.g. the terminal
# window is closed during ssh or tmux), precmd never runs; record the command
# on exit instead, with its exit code and duration unknown.
_loxx_exit() {
  [[ -n $_loxx_cmd ]] || return 0
  "$_loxx_bin" record --start "$_loxx_start" --cwd "$_loxx_cwd" \
    --session "$_loxx_session" -- "$_loxx_cmd" </dev/null >/dev/null 2>&1
  _loxx_cmd=""
}

# Typing `loxx` with no arguments opens the search panel; the chosen command
# is placed in the next prompt, ready to edit. It is never run for you.
# With arguments, `loxx` is the loxx binary (`loxx search …`, `loxx import`, …).
loxx() {
  if (( $# )); then
    "$_loxx_bin" "$@"
    return
  fi
  local chosen
  chosen=$("$_loxx_bin" panel --cwd "$PWD" --session "$_loxx_session") || return 0
  print -z -- "$chosen"
}

autoload -Uz add-zsh-hook
add-zsh-hook preexec _loxx_preexec
add-zsh-hook zshexit _loxx_exit
# Run first among precmd hooks so $? is still the command's exit status.
precmd_functions=(_loxx_precmd ${precmd_functions:#_loxx_precmd})
