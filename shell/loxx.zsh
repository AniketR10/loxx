# loxx hook for zsh 5.1+. Load from ~/.zshrc: eval "$(loxx init zsh)"

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

# Shell exiting mid-command (window closed): record it without exit code.
_loxx_exit() {
  [[ -n $_loxx_cmd ]] || return 0
  "$_loxx_bin" record --start "$_loxx_start" --cwd "$_loxx_cwd" \
    --session "$_loxx_session" -- "$_loxx_cmd" </dev/null >/dev/null 2>&1
  _loxx_cmd=""
}

# `loxx` alone opens the search panel; with arguments it's the binary.
loxx() {
  if (( $# )); then
    LOXX_HOOK=zsh "$_loxx_bin" "$@" # tells `loxx status` the hook is active here
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

# Ctrl-Alt-G opens the search with the line as the query; the choice fills the line, never runs.
_loxx_search() {
  local chosen
  chosen=$("$_loxx_bin" panel --cwd "$PWD" --session "$_loxx_session" --query "$BUFFER")
  if (( ! $? )) && [[ -n $chosen ]]; then
    BUFFER=$chosen
    CURSOR=${#BUFFER}
  fi
  zle reset-prompt
  return 0 # a widget ending in failure makes zsh beep
}
zle -N _loxx_search
() {
  local map was
  # Only where Ctrl-Alt-G still does zsh's default.
  for map in emacs viins vicmd; do
    was=$(bindkey -M $map '^[^G')
    [[ ${was##* } == (send-break|undefined-key) ]] && bindkey -M $map '^[^G' _loxx_search
  done
}

# Startup line (hidden by LOXX_QUIET=1).
if [[ ${LOXX_QUIET:-0} != 1 && -t 1 ]]; then
  print -r -- $'\e[2m''loxx is on: type `loxx` or press Ctrl-Alt-G to search your history, e.g. "undo my last commit".'$'\e[0m'
fi
