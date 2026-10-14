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

# Suggestions while you type (zsh 5.3+; off with LOXX_SUGGEST=0): keyword
# matches after a 0.3 s pause, matches by meaning after 0.9 s. Never run.
_loxx_suggest_setup() {
  [[ ${LOXX_SUGGEST:-1} != 0 ]] || return 1
  autoload -Uz is-at-least add-zle-hook-widget
  is-at-least 5.3 || return 1
  zmodload zsh/system zsh/zselect 2>/dev/null || return 1
  # zsh 5.8+ gives the search's pid directly; older zsh reads it from the child.
  typeset -g _loxx_pid_direct=0
  is-at-least 5.8 && _loxx_pid_direct=1

  typeset -g _loxx_fd='' _loxx_pid='' _loxx_last='' _loxx_query='' _loxx_sel=0 _loxx_top=1 _loxx_finishing=0
  typeset -ga _loxx_cmds=() _loxx_details=() _loxx_status=()
  # The list lives in POSTDISPLAY; colours need memo= tags (zsh 5.9+).
  typeset -g _loxx_post='' _loxx_color=0
  is-at-least 5.9 && _loxx_color=1
  typeset -gA _loxx_keys=() # key sequence -> the widget it ran before loxx
  typeset -g _loxx_mark='▸ ' _loxx_dot='·'
  if [[ ${(L)${LC_ALL:-${LC_CTYPE:-$LANG}}} != *utf(-|)8* ]]; then
    _loxx_mark='> ' _loxx_dot='-' # the terminal may not show these characters
  fi

  zle -N _loxx_suggest_ready
  zle -N _loxx_suggest_down
  zle -N _loxx_suggest_up
  zle -N _loxx_suggest_accept
  zle -N _loxx_suggest_interrupt
  local key widget was
  # Down, Up (both cursor modes) and Enter, as raw bytes like $KEYS.
  for key widget in $'\e[B' _loxx_suggest_down $'\eOB' _loxx_suggest_down \
    $'\e[A' _loxx_suggest_up $'\eOA' _loxx_suggest_up $'\r' _loxx_suggest_accept; do
    was=$(bindkey -- $key)
    was=${was##* } # (not on a `local` line: zsh wouldn't strip it there)
    [[ $was == _loxx_suggest_* ]] && continue # already set up in this shell
    [[ $was == undefined-key ]] || _loxx_keys[$key]=$was
    bindkey -- $key $widget
  done
  add-zle-hook-widget line-pre-redraw _loxx_suggest_changed
  add-zle-hook-widget line-init _loxx_suggest_line_init
  add-zle-hook-widget line-finish _loxx_suggest_line_finish
  # Ctrl-C skips line-finish; clear the list from TRAPINT (unless you have one).
  if (( ! ${+functions[TRAPINT]} )); then
    TRAPINT() {
      zle && zle _loxx_suggest_interrupt
      return $(( 128 + $1 )) # then Ctrl-C does what it always does
    }
  fi
}

# Runs what the key did before loxx (or $1 if it was unbound).
_loxx_suggest_orig() {
  zle ${_loxx_keys[$KEYS]:-$1}
}

_loxx_suggest_cancel() {
  if [[ -n $_loxx_fd ]]; then
    zle -F $_loxx_fd 2>/dev/null
    exec {_loxx_fd}<&-
    _loxx_fd=''
  fi
  if [[ -n $_loxx_pid ]]; then
    kill -TERM $_loxx_pid 2>/dev/null
    _loxx_pid=''
  fi
}

_loxx_suggest_clear() {
  _loxx_cmds=() _loxx_details=() _loxx_status=() _loxx_sel=0 _loxx_top=1
  _loxx_suggest_undraw
}

# Removes the list, keeping other plugins' text in POSTDISPLAY.
_loxx_suggest_undraw() {
  [[ -n $_loxx_post ]] || return 0
  [[ $POSTDISPLAY == *"$_loxx_post" ]] && POSTDISPLAY=${POSTDISPLAY%"$_loxx_post"}
  region_highlight=("${(@)region_highlight:#* memo=loxx}")
  _loxx_post=''
}

_loxx_suggest_reset() {
  _loxx_suggest_cancel
  _loxx_suggest_clear
  _loxx_last=$BUFFER
}

# Dim hint shown in an empty prompt (hidden by LOXX_QUIET=1).
typeset -g _loxx_placeholder=' type what you want to do, like "undo my last commit"'

_loxx_suggest_placeholder() {
  [[ ${LOXX_QUIET:-0} == 1 ]] && return
  (( _loxx_color )) || return # undimmed, the hint would look like typed text
  if [[ -z $BUFFER ]] && (( ! _loxx_finishing )); then
    [[ -n $POSTDISPLAY ]] && return # already showing (or another plugin's text)
    setopt localoptions extendedglob
    local shown=${(%)PROMPT} # the prompt as displayed, minus colour codes
    shown=${shown//$'\e'\[[0-9;]#m/}
    POSTDISPLAY=${_loxx_placeholder[1,COLUMNS-${#${shown##*$'\n'}}-10]} # room for the badge
    region_highlight+=("0 ${#POSTDISPLAY} fg=8 memo=loxx-hint")
  elif [[ -n $POSTDISPLAY && $_loxx_placeholder == "$POSTDISPLAY"* ]]; then
    POSTDISPLAY=''
    region_highlight=("${(@)region_highlight:#* memo=loxx-hint}")
  fi
}

# Only touch POSTDISPLAY here: zle -M in line-finish stops the line running.
_loxx_suggest_line_init() {
  _loxx_finishing=0
  _loxx_suggest_placeholder
}
_loxx_suggest_line_finish() {
  _loxx_finishing=1
  _loxx_suggest_undraw
  _loxx_suggest_placeholder
}
_loxx_suggest_interrupt() {
  _loxx_suggest_cancel
  _loxx_suggest_line_finish
  zle -R
}

# Starts a new search whenever the line changes.
_loxx_suggest_changed() {
  if [[ $BUFFER == "$_loxx_last" ]]; then
    _loxx_suggest_placeholder
    return
  fi
  _loxx_last=$BUFFER
  _loxx_suggest_cancel
  _loxx_suggest_clear
  _loxx_suggest_placeholder
  local q=$BUFFER
  # Skip short, private and multi-line lines, and zsh's completion menu.
  (( ${#q} >= 3 )) || return
  [[ $q == ' '* || $q == *$'\n'* ]] && return
  # More keys queued (typing fast, pasting): wait for the last one.
  (( PENDING || ${KEYS_QUEUED_COUNT:-0} )) && return
  [[ $LASTWIDGET == *complet* || $LASTWIDGET == *expand* || $LASTWIDGET == menu-* ]] && return
  _loxx_query=$q
  integer n=50 # fetched; up to 13 rows are shown at a time, and the list scrolls
  exec {_loxx_fd}< <(
    (( _loxx_pid_direct )) || print -r -- $sysparams[pid] # so the next key can stop it
    zselect -t 30
    "$_loxx_bin" suggest --keyword-only --limit $n -- "$q" 2>/dev/null
    print -rn -- $'\x1e'
    zselect -t 60
    exec "$_loxx_bin" suggest --limit $n -- "$q" 2>/dev/null
  )
  if (( _loxx_pid_direct )); then
    _loxx_pid=$sysparams[procsubstpid]
  else
    command true # older zsh: keeps Ctrl-C working (zsh-users/zsh-autosuggestions#364)
    read -r _loxx_pid <&$_loxx_fd
  fi
  zle -F -w $_loxx_fd _loxx_suggest_ready
}

# Reads a search's output: two stages ended by \x1e, records by NUL, fields by \x1f.
_loxx_suggest_ready() {
  local fd=$1 data rec rest
  if ! IFS= read -r -u $fd -d $'\x1e' data && [[ -z $data ]]; then
    zle -F $fd 2>/dev/null
    exec {fd}<&-
    [[ $fd == $_loxx_fd ]] && _loxx_fd='' _loxx_pid=''
    return
  fi
  # Drop results for a line that's gone.
  if [[ $BUFFER != "$_loxx_query" ]]; then
    _loxx_suggest_cancel
    return
  fi
  # Keep the list if the keyword stage found nothing; meaning may.
  [[ -z $data && ${#_loxx_cmds} -gt 0 ]] && return
  (( _loxx_sel )) && return # don't move the list under someone choosing from it
  _loxx_cmds=() _loxx_details=() _loxx_status=() _loxx_top=1
  for rec in ${(ps:\0:)data}; do
    _loxx_cmds+=("${rec%%$'\x1f'*}")
    rest=${rec#*$'\x1f'}
    _loxx_details+=("${${rest%%$'\x1f'*}//·/$_loxx_dot}")
    _loxx_status+=("${${(M)rest:#*$'\x1f'*}#*$'\x1f'}")
  done
  _loxx_suggest_draw
  zle -R # a result arriving in the background doesn't redraw by itself
}

# Draws the list under the line, coloured like the search panel.
_loxx_suggest_draw() {
  _loxx_suggest_undraw
  integer total=${#_loxx_cmds}
  (( total )) || return 0
  # Up to 13 rows (fewer in short terminals); scrolls with the chosen row.
  integer rows=$(( LINES - 3 ))
  (( rows > 13 )) && rows=13
  (( rows < 3 )) && rows=3
  integer shown=$rows
  (( total > rows )) && shown=$(( rows - 1 ))
  (( total < shown )) && shown=$total
  if (( _loxx_sel == 0 )); then
    _loxx_top=1
  elif (( _loxx_sel < _loxx_top )); then
    _loxx_top=$_loxx_sel
  elif (( _loxx_sel > _loxx_top + shown - 1 )); then
    _loxx_top=$(( _loxx_sel - shown + 1 ))
  fi
  local base=$POSTDISPLAY # e.g. zsh-autosuggestions' grey rest of the line
  local list='' line cmd mark mk
  integer i s c d full off=$(( ${#BUFFER} + ${#base} ))
  for i in {$_loxx_top..$(( _loxx_top + shown - 1 ))}; do
    cmd=${_loxx_cmds[i]//$'\n'/ ↵ }
    mark='  '
    (( i == _loxx_sel )) && mark=$_loxx_mark
    case ${_loxx_status[i]} in
      ok) mk=' ✓' ;;
      fail) mk=' ✗' ;;
      *) mk='' ;;
    esac
    line="$mark$cmd  ${_loxx_details[i]}$mk"
    full=${#line}
    line=${line[1,COLUMNS-1]}
    list+=$'\n'
    s=$(( off + ${#list} )) # where this row starts
    list+=$line
    c=$(( s + ${#mark} + ${#cmd} )) # end of the command
    d=$(( c + 2 + ${#_loxx_details[i]} )) # end of the details
    (( i == _loxx_sel )) && _loxx_suggest_hl $s $c $(( s + ${#line} )) bold
    _loxx_suggest_hl $(( c + 2 )) $d $(( s + ${#line} )) fg=8
    case $mk in
      ' ✓') _loxx_suggest_hl $(( s + full - 1 )) $(( s + full )) $(( s + ${#line} )) fg=green ;;
      ' ✗') _loxx_suggest_hl $(( s + full - 1 )) $(( s + full )) $(( s + ${#line} )) fg=red ;;
    esac
  done
  if (( total > shown )); then
    line="  $_loxx_top–$(( _loxx_top + shown - 1 )) of $total $_loxx_dot ↑↓ scroll"
    list+=$'\n'
    s=$(( off + ${#list} ))
    list+=$line
    _loxx_suggest_hl $s $(( s + ${#line} )) $(( s + ${#line} )) fg=8
  fi
  POSTDISPLAY=$base$list
  _loxx_post=$list
}

# Adds a tagged highlight, cut at the row's end.
_loxx_suggest_hl() {
  (( _loxx_color )) || return 0
  integer start=$1 end=$2 row_end=$3
  (( end > row_end )) && end=$row_end
  (( start < end )) || return 0
  region_highlight+=("$start $end $4 memo=loxx")
}

_loxx_suggest_down() {
  if (( ${#_loxx_cmds} )); then
    (( _loxx_sel = _loxx_sel % ${#_loxx_cmds} + 1 ))
    _loxx_suggest_draw
  else
    _loxx_suggest_orig down-line-or-history
  fi
}

_loxx_suggest_up() {
  if (( _loxx_sel )); then
    (( _loxx_sel-- ))
    _loxx_suggest_draw
  else
    _loxx_suggest_orig up-line-or-history
  fi
}

# Enter on a chosen suggestion fills the prompt (never runs it); else as usual.
_loxx_suggest_accept() {
  if (( _loxx_sel )); then
    BUFFER=${_loxx_cmds[_loxx_sel]}
    CURSOR=${#BUFFER}
    _loxx_suggest_reset
    if (( ${+widgets[autosuggest-clear]} )); then
      zle autosuggest-clear
    fi
    return 0 # a widget ending in failure makes zsh beep
  fi
  _loxx_suggest_reset
  _loxx_suggest_orig accept-line
}

# "● loxx" at the right of the prompt (hidden by LOXX_QUIET=1).
typeset -g _loxx_badge='%B%F{28}● loxx%f%b' # dark green (#008700)
_loxx_badge_precmd() {
  [[ ${LOXX_QUIET:-0} == 1 ]] && return
  [[ $RPROMPT == *"$_loxx_badge"* ]] || RPROMPT="${RPROMPT:+$RPROMPT }$_loxx_badge"
}
add-zsh-hook precmd _loxx_badge_precmd
# Keep it off past lines, unless you have a right prompt of your own.
[[ -z ${RPROMPT}${RPS1} && ${LOXX_QUIET:-0} != 1 ]] && setopt transient_rprompt

# Startup line (hidden by LOXX_QUIET=1).
if _loxx_suggest_setup; then
  _loxx_hello='type what you want to do, like "undo my last commit", and matching commands appear below.'
else
  _loxx_hello='type loxx to search your history by meaning.'
fi
if [[ ${LOXX_QUIET:-0} != 1 && -t 1 ]]; then
  print -r -- $'\e[2m'"loxx is on: $_loxx_hello"$'\e[0m'
fi
unset _loxx_hello
