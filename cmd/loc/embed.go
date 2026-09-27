package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/AniketR10/loc/internal/embed"
	"github.com/AniketR10/loc/internal/store"
)

// embedBatch is how many commands are embedded and saved per transaction.
// Interrupting loses at most one batch of work.
const embedBatch = 64

func runEmbed(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("embed", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: loc embed --pending [--workers N]")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Embeds every command that has no vector from the built-in model yet.")
		fmt.Fprintln(stderr, "Only one embed runs at a time; a second one exits immediately.")
		fmt.Fprintln(stderr)
		fs.PrintDefaults()
	}
	pending := fs.Bool("pending", false, "embed all commands that have no vector yet (required)")
	workers := fs.Int("workers", max(1, runtime.NumCPU()/2), "commands embedded in parallel")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if !*pending || fs.NArg() != 0 || *workers < 1 {
		fs.Usage()
		return 2
	}

	path, err := store.DefaultPath()
	if err != nil {
		fmt.Fprintln(stderr, "loc embed:", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := store.Open(ctx, path)
	if err != nil {
		fmt.Fprintln(stderr, "loc embed:", err)
		return 1
	}
	defer db.Close()

	unlock, err := tryLock(path + ".embed.lock")
	if errors.Is(err, errLocked) {
		fmt.Fprintln(stderr, "loc embed: another embed is already running")
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, "loc embed:", err)
		return 1
	}
	defer unlock()

	total, err := db.CountPendingEmbeddings(ctx, embed.ModelID)
	if err != nil {
		fmt.Fprintln(stderr, "loc embed:", err)
		return 1
	}
	if total == 0 {
		return 0
	}
	model, err := embed.Default()
	if err != nil {
		fmt.Fprintln(stderr, "loc embed:", err)
		return 1
	}

	start := time.Now()
	progress := isTerminal(stderr)
	done := 0
	for ctx.Err() == nil {
		batch, err := db.PendingEmbeddings(ctx, embed.ModelID, embedBatch)
		if err == nil && len(batch) == 0 {
			break
		}
		if err == nil {
			err = db.SaveEmbeddings(ctx, embed.ModelID, embedAll(model, batch, *workers))
		}
		if err != nil {
			if ctx.Err() != nil {
				break // interrupted; reported below
			}
			fmt.Fprintln(stderr, "\nloc embed:", err)
			return 1
		}
		done += len(batch)
		if progress {
			fmt.Fprintf(stderr, "\rloc: embedded %d/%d", done, total)
		}
	}
	if progress {
		fmt.Fprintln(stderr)
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintf(stderr, "loc embed: interrupted after %d command(s); run it again to continue\n", done)
		return 1
	}
	fmt.Fprintf(stderr, "loc: embedded %d command(s) in %.1fs\n", done, time.Since(start).Seconds())
	return 0
}

// embedAll embeds batch using up to workers goroutines. Parallelism is across
// commands: on the dev machine independent work scaled ~6x on 12 threads,
// while splitting a single embedding did not help (see ROADMAP Phase 2).
func embedAll(model *embed.Model, batch []store.PendingCommand, workers int) []store.Embedding {
	out := make([]store.Embedding, len(batch))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(workers, len(batch)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				out[i] = store.Embedding{CommandID: batch[i].ID, Vector: model.Embed(batch[i].Text)}
			}
		}()
	}
	for i := range batch {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return out
}

var errLocked = errors.New("locked")

// tryLock takes an exclusive, non-blocking lock on path, creating it
// owner-only if needed. The kernel releases the lock if the process dies.
func tryLock(path string) (unlock func(), err error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errLocked
		}
		return nil, err
	}
	return func() { f.Close() }, nil
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
