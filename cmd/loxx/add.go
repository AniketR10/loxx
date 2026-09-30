package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/AniketR10/loxx/internal/scrub"
	"github.com/AniketR10/loxx/internal/store"
)

func runAdd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: loxx add [--cwd DIR] [--exit CODE] <command>")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Records a command manually. Quote the command so it is one argument.")
		fmt.Fprintln(stderr)
		fs.PrintDefaults()
	}
	cwd := fs.String("cwd", "", "directory the command ran in (default: current directory)")
	var exitCode *int
	fs.Func("exit", "exit code of the command (default: unknown)", func(s string) error {
		n, err := strconv.Atoi(s)
		if err != nil {
			return errors.New("must be an integer")
		}
		exitCode = &n
		return nil
	})
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}

	raw := fs.Arg(0)
	if scrub.Ignored(raw) {
		fmt.Fprintln(stderr, "loxx: not recorded (command starts with a space)")
		return 0
	}
	cmd := scrub.Scrub(raw)
	if cmd.Text() == "" {
		fmt.Fprintln(stderr, "loxx add: command is empty")
		return 2
	}

	if *cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(stderr, "loxx add:", err)
			return 1
		}
		*cwd = wd
	}
	hostname, _ := os.Hostname()

	ctx := context.Background()
	db, err := openDB(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "loxx add:", err)
		return 1
	}
	defer db.Close()

	_, err = db.Add(ctx, store.Execution{
		Command:   cmd,
		Cwd:       *cwd,
		ExitCode:  exitCode,
		StartedAt: time.Now(),
		Hostname:  hostname,
		Source:    store.SourceManual,
	})
	if err != nil {
		fmt.Fprintln(stderr, "loxx add:", err)
		return 1
	}
	if n := cmd.Redactions(); n > 0 {
		fmt.Fprintf(stderr, "loxx: redacted %d secret(s) before storing\n", n)
	}
	return 0
}

func openDB(ctx context.Context) (*store.DB, error) {
	path, err := store.DefaultPath()
	if err != nil {
		return nil, err
	}
	return store.Open(ctx, path)
}
