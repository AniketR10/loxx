package main

import (
	"fmt"
	"io"

	"github.com/AniketR10/loxx/internal/color"
)

// say prints a message from loxx to w, starting "loxx:" (in teal on a
// terminal).
func say(w io.Writer, format string, a ...any) {
	fmt.Fprintf(w, color.For(w).Prefix()+" "+format, a...)
}

// sayDone is say for something that worked: a green ✓ on a terminal, and
// the same words as say without one.
func sayDone(w io.Writer, format string, a ...any) {
	c := color.For(w)
	mark := ""
	if c.On {
		mark = c.Green("✓") + " "
	}
	fmt.Fprintf(w, c.Prefix()+" "+mark+format, a...)
}
