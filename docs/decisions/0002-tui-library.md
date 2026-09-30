# 0002: TUI library: Bubbletea v2 alone

- **Date:** 2026-09-29
- **Status:** Accepted
- **Decided by:** the user, on Claude's recommendation after a measured spike

## Context

Phase 5 adds the Ctrl-R panel: an inline (not fullscreen) search box and
result list drawn on `/dev/tty`, with keyword results immediately and
semantic results merged in later, and the chosen command printed to stdout.
The user asked whether Bubbletea is "the best, not bloated, and a perfect
fit", so we measured instead of assuming.

## Options

| Option | Fit |
|---|---|
| Bubbletea (+ Bubbles, Lipgloss) | Inline by default; message model suits async results; handles key parsing and resize. Bubbles (24 modules) and Lipgloss (18) are more than one input line and a list need. |
| Hand-rolled on `golang.org/x/term` | Smallest, but we'd own escape-sequence parsing, pasted text, wide characters, resize and inline cursor maths: the bugs that break TUIs across terminals and tmux. |
| tcell / tview | Fullscreen-oriented; weak inline rendering. Poor fit. |

## Spike (throwaway module, 2026-09-29, AC power)

A bare inline panel (input + 15-row list over 1,000 items, output on
`/dev/tty`, choice on stdout) built two ways, 20 launches each in an 80×24
pseudo-terminal:

| | Hand-rolled (`x/term`) | Bubbletea v2.0.9 alone |
|---|---|---|
| Binary | 1.5 MB | 3.6 MB (**+2.1 MB**, ~4% of loxx's 52 MB) |
| First paint | 1.4 ms | **24.8 ms** (max 25.5) |
| Modules | 1 | 16, all MIT or BSD |

Bar agreed in advance: < 5 MB and < 50 ms. Both met.

## Decision

**Bubbletea v2.0.9, without Bubbles or Lipgloss.**

Caveats accepted:
- Pinned one release behind: v2.0.10 requires Go 1.26 (the fourth dependency
  to hit this; see ROADMAP O6).
- It depends on `github.com/charmbracelet/ultraviolet` at an unreleased
  pseudo-version.
- It sends terminal-capability queries at startup. Real terminals answer
  them; Phase 5's "works in two terminals and tmux" check must confirm it.

## Learnings

1. **Measure "is it bloated?" instead of arguing it.** One hour gave hard
   numbers: +2.1 MB and +23 ms, both well under the bar.
2. **Drop companion libraries you don't need.** Bubbletea alone is 16
   modules; with Bubbles and Lipgloss it would be several dozen more.
3. **Pseudo-terminals in tests need a size.** With the default 0×0 window,
   Bubbletea never painted (it had no columns to draw in). A real terminal
   always has a size; set one (TIOCSWINSZ) in any pty harness.
4. **Check a new dependency's Go version first.** Four dependencies have now
   needed a version pinned below latest to stay on Go 1.25.
