package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AniketR10/loc/internal/format"
	"github.com/AniketR10/loc/internal/importer"
	"github.com/AniketR10/loc/internal/scrub"
	"github.com/AniketR10/loc/internal/store"
)

func runImport(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: loc import [--bash FILE] [--zsh FILE] [--force]")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Imports existing shell history, scrubbed like live commands. With no files")
		fmt.Fprintln(stderr, "given, imports ~/.bash_history and ~/.zsh_history if they exist. A file is")
		fmt.Fprintln(stderr, "only imported once unless --force is given.")
		fmt.Fprintln(stderr)
		fs.PrintDefaults()
	}
	bashFile := fs.String("bash", "", "bash history file to import")
	zshFile := fs.String("zsh", "", "zsh history file to import")
	force := fs.Bool("force", false, "import again even if the file was imported before")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 2
	}

	type source struct {
		path  string
		parse func(io.Reader) ([]importer.Entry, error)
		given bool // named on the command line, so a missing file is an error
	}
	var sources []source
	if *bashFile != "" || *zshFile != "" {
		if *bashFile != "" {
			sources = append(sources, source{*bashFile, importer.ParseBash, true})
		}
		if *zshFile != "" {
			sources = append(sources, source{*zshFile, importer.ParseZsh, true})
		}
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(stderr, "loc import:", err)
			return 1
		}
		sources = []source{
			{filepath.Join(home, ".bash_history"), importer.ParseBash, false},
			{filepath.Join(home, ".zsh_history"), importer.ParseZsh, false},
		}
	}

	path, err := store.DefaultPath()
	if err != nil {
		fmt.Fprintln(stderr, "loc import:", err)
		return 1
	}
	ctx := context.Background()
	db, err := store.Open(ctx, path)
	if err != nil {
		fmt.Fprintln(stderr, "loc import:", err)
		return 1
	}
	defer db.Close()

	hostname, _ := os.Hostname()
	home, _ := os.UserHomeDir()
	imported := 0
	for _, src := range sources {
		abs, err := filepath.Abs(src.path)
		if err != nil {
			fmt.Fprintln(stderr, "loc import:", err)
			return 1
		}
		marker := "imported:" + abs
		if _, done, err := db.Meta(ctx, marker); err != nil {
			fmt.Fprintln(stderr, "loc import:", err)
			return 1
		} else if done && !*force {
			fmt.Fprintf(stderr, "loc: %s was already imported (use --force to import it again)\n", format.TildePath(abs, home))
			continue
		}
		f, err := os.Open(abs)
		if errors.Is(err, os.ErrNotExist) && !src.given {
			continue
		}
		if err != nil {
			fmt.Fprintln(stderr, "loc import:", err)
			return 1
		}
		entries, err := src.parse(f)
		f.Close()
		if err != nil {
			fmt.Fprintf(stderr, "loc import: reading %s: %v\n", abs, err)
			return 1
		}

		var execs []store.Execution
		redactions, skipped := 0, 0
		for _, en := range entries {
			if scrub.Ignored(en.Command) {
				skipped++
				continue
			}
			cmd := scrub.Scrub(strings.ToValidUTF8(en.Command, "�"))
			if cmd.Text() == "" {
				skipped++
				continue
			}
			redactions += cmd.Redactions()
			execs = append(execs, store.Execution{
				Command:   cmd,
				StartedAt: en.StartedAt,
				Duration:  en.Duration,
				Hostname:  hostname,
				Source:    store.SourceImport,
			})
		}
		if err := db.AddBatch(ctx, execs); err != nil {
			fmt.Fprintf(stderr, "loc import: storing %s: %v\n", abs, err)
			return 1
		}
		if err := db.SetMeta(ctx, marker, "1"); err != nil {
			fmt.Fprintln(stderr, "loc import:", err)
			return 1
		}
		imported += len(execs)
		fmt.Fprintf(stderr, "loc: imported %d command(s) from %s (%d secret(s) redacted, %d skipped)\n",
			len(execs), format.TildePath(abs, home), redactions, skipped)
	}
	if imported > 0 {
		if err := startBackgroundEmbed(path); err != nil {
			fmt.Fprintln(stderr, "loc import: starting background embed:", err)
			return 1
		}
	}
	return 0
}
