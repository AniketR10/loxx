// Package color is loxx's terminal palette: teal and pink, in 256 colors, so
// it looks the same on light and dark terminals. Colors are used only on a
// terminal, and never when NO_COLOR is set (https://no-color.org).
package color

import (
	"io"
	"os"
	"strconv"
)

// Palette colors text, or returns it unchanged when colors are off.
type Palette struct {
	On  bool
	row bool // inside Row: colors end in white on the row's background
}

// For is the palette for output written to w: colored only if w is a
// terminal, NO_COLOR is unset and TERM isn't "dumb".
func For(w io.Writer) Palette {
	f, ok := w.(*os.File)
	if !ok || !Wanted() {
		return Palette{}
	}
	fi, err := f.Stat()
	return Palette{On: err == nil && fi.Mode()&os.ModeCharDevice != 0}
}

// Wanted reports whether the user allows colors at all.
func Wanted() bool {
	_, off := os.LookupEnv("NO_COLOR")
	return !off && os.Getenv("TERM") != "dumb"
}

// 256-color codes (xterm).
const (
	teal  = 73  // #5fafaf: loxx itself, prompts, keys
	pink  = 168 // #d75f87: suggestions, the fun lines
	green = 78  // #5fd787: success
	red   = 167 // #d75f5f: failure
	blue  = 110 // #87afd7: folders
	white = 231
	shade = 236 // background of the selected row
)

// SGR codes that end one attribute without resetting the others, so a row's
// background survives the colors inside it.
const (
	Reset   = "\x1b[0m"
	FgOff   = "\x1b[39m"
	BoldOff = "\x1b[22m"
)

func (p Palette) wrap(code, s, end string) string {
	if !p.On || s == "" {
		return s
	}
	return code + s + end
}

func fg(n int) string { return "\x1b[38;5;" + strconv.Itoa(n) + "m" }
func bg(n int) string { return "\x1b[48;5;" + strconv.Itoa(n) + "m" }

// fgEnd ends a foreground color: back to the terminal's own text color, or
// to white inside a selected row.
func (p Palette) fgEnd() string {
	if p.row {
		return fg(white)
	}
	return FgOff
}

func (p Palette) Teal(s string) string  { return p.wrap(fg(teal), s, p.fgEnd()) }
func (p Palette) Pink(s string) string  { return p.wrap(fg(pink), s, p.fgEnd()) }
func (p Palette) Green(s string) string { return p.wrap(fg(green), s, p.fgEnd()) }
func (p Palette) Red(s string) string   { return p.wrap(fg(red), s, p.fgEnd()) }
func (p Palette) Blue(s string) string  { return p.wrap(fg(blue), s, p.fgEnd()) }
func (p Palette) Bold(s string) string  { return p.wrap("\x1b[1m", s, BoldOff) }
func (p Palette) Dim(s string) string   { return p.wrap("\x1b[2m", s, BoldOff) }

// Brand is "loxx" in bold teal.
func (p Palette) Brand(s string) string { return p.Bold(p.Teal(s)) }

// InRow is the palette for text inside a selected row (see Row).
func (p Palette) InRow() Palette { return Palette{On: p.On, row: true} }

// Row gives a whole line the selected-row background, with white text. Color
// its parts with InRow so they don't end the background early.
func (p Palette) Row(s string) string {
	return p.wrap(bg(shade)+fg(white), s, Reset)
}

// Prefix is "loxx:" in teal, for messages loxx prints to the user.
func (p Palette) Prefix() string { return p.Brand("loxx:") }
