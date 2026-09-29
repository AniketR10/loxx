# loc shell integration for zsh (zsh 5.1+). Load it from ~/.zshrc with:
#   eval "$(loc init zsh)"
#
# After every command, `loc record` runs in the background (&!), so the prompt
# never waits for it. Commands starting with a space are not recorded.

zmodload zsh/datetime 2>/dev/null # $EPOCHREALTIME

typeset -g _loc_bin=__LOC_BIN__
typeset -g _loc_session="$$-${EPOCHREALTIME:-$EPOCHSECONDS}"
typeset -g _loc_cmd="" _loc_start="" _loc_cwd=""

_loc_preexec() {
  _loc_cmd=$1 # the command line as typed
  _loc_start=${EPOCHREALTIME:-$EPOCHSECONDS}
  _loc_cwd=$PWD
}

_loc_precmd() {
  local exit_code=$? # must be first: anything else overwrites $?
  [[ -n $_loc_cmd ]] || return 0 # empty line, or the first prompt
  "$_loc_bin" record --exit "$exit_code" --start "$_loc_start" \
    --end "${EPOCHREALTIME:-$EPOCHSECONDS}" --cwd "$_loc_cwd" \
    --session "$_loc_session" -- "$_loc_cmd" >/dev/null 2>&1 &!
  _loc_cmd=""
}

# If the shell exits while a command is still running (e.g. the terminal
# window is closed during ssh or tmux), precmd never runs; record the command
# on exit instead, with its exit code and duration unknown.
_loc_exit() {
  [[ -n $_loc_cmd ]] || return 0
  "$_loc_bin" record --start "$_loc_start" --cwd "$_loc_cwd" \
    --session "$_loc_session" -- "$_loc_cmd" </dev/null >/dev/null 2>&1
  _loc_cmd=""
}

# Typing `loc` with no arguments opens the search panel; the chosen command
# is placed in the next prompt, ready to edit. It is never run for you.
# With arguments, `loc` is the loc binary (`loc search …`, `loc import`, …).
loc() {
  if (( $# )); then
    "$_loc_bin" "$@"
    return
  fi
  local chosen
  chosen=$("$_loc_bin" panel --cwd "$PWD" --session "$_loc_session") || return 0
  print -z -- "$chosen"
}

autoload -Uz add-zsh-hook
add-zsh-hook preexec _loc_preexec
add-zsh-hook zshexit _loc_exit
# Run first among precmd hooks so $? is still the command's exit status.
precmd_functions=(_loc_precmd ${precmd_functions:#_loc_precmd})
