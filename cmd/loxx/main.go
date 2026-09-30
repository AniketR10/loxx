// Command loxx is a local-first shell history tool with hybrid keyword and
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
		{name: "forget", summary: "delete commands from your history for good", run: runForget},
		{name: "import", summary: "import existing bash/zsh history", run: runImport},
		{name: "init", summary: "print the shell hook that records commands", run: runInit},
		{name: "panel", summary: "open the search panel (what plain `loxx` does)", run: runPanel},
		{name: "record", summary: "record a finished command (used by the shell hook)", run: runRecord},
		{name: "search", summary: "search recorded commands by meaning and keyword", run: runSearch},
		{name: "setup", summary: "add the hook to your shells and import your history", run: runSetup},
		{name: "status", summary: "show what loxx has recorded and whether the hook is active", run: runStatus},
		{name: "uninstall", summary: "remove the hook (and optionally your history and the binary)", run: runUninstall},
		{name: "version", summary: "print the loxx version", run: runVersion},
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches to a subcommand and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return runPanel(nil, stdout, stderr)
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
	fmt.Fprintf(stderr, "loxx: unknown command %q\n\n", args[0])
	usage(stderr)
	return 2
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "Usage: loxx                       open the search panel")
	fmt.Fprintln(w, "       loxx <command> [arguments]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	for _, c := range commands() {
		fmt.Fprintf(w, "  %-10s %s\n", c.name, c.summary)
	}
}

func runVersion(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "loxx version: takes no arguments")
		return 2
	}
	fmt.Fprintf(stdout, "loxx %s\n", version)
	return 0
}
