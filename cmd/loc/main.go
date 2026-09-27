// Command loc is a local-first shell history tool with hybrid keyword and
// semantic search.
package main

import (
	"fmt"
	"io"
	"os"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

type command struct {
	name    string
	summary string
	run     func(args []string, stdout, stderr io.Writer) int
}

func commands() []command {
	return []command{
		{name: "add", summary: "record a command manually", run: runAdd},
		{name: "embed", summary: "embed commands for semantic search", run: runEmbed},
		{name: "search", summary: "search recorded commands by keyword", run: runSearch},
		{name: "version", summary: "print the loc version", run: runVersion},
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches to a subcommand and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "help", "-h", "-help", "--help":
		usage(stdout)
		return 0
	}
	for _, c := range commands() {
		if c.name == args[0] {
			return c.run(args[1:], stdout, stderr)
		}
	}
	fmt.Fprintf(stderr, "loc: unknown command %q\n\n", args[0])
	usage(stderr)
	return 2
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "Usage: loc <command> [arguments]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	for _, c := range commands() {
		fmt.Fprintf(w, "  %-10s %s\n", c.name, c.summary)
	}
}

func runVersion(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "loc version: takes no arguments")
		return 2
	}
	fmt.Fprintf(stdout, "loc %s\n", version)
	return 0
}
