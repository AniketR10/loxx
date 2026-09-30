package main

import (
	"fmt"
	"io"
	"os"

	"github.com/AniketR10/loxx/shell"
)

// runInit prints the shell integration script. Users load it with
// eval "$(loxx init zsh)" in their shell's rc file.
func runInit(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(stderr, "Usage: loxx init <shell>")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Prints the hook that records every command. Add one line to your rc file:")
		fmt.Fprintln(stderr, `  ~/.zshrc:  eval "$(loxx init zsh)"`)
		fmt.Fprintln(stderr, `  ~/.bashrc: eval "$(loxx init bash)"   (at the end: it wraps PROMPT_COMMAND)`)
		if len(args) == 1 {
			return 0
		}
		return 2
	}
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(stderr, "loxx init:", err)
		return 1
	}
	script, err := shell.Script(args[0], self)
	if err != nil {
		fmt.Fprintln(stderr, "loxx init:", err)
		return 2
	}
	fmt.Fprint(stdout, script)
	return 0
}
