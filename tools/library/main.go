// Command library builds loxx's command library files from tldr-pages (dev only).
//
//	git clone --filter=blob:none --sparse https://github.com/tldr-pages/tldr.git
//	git -C tldr sparse-checkout set pages && git -C tldr checkout <tldrCommit>
//	go run ./tools/library --tldr tldr --out internal/library/data
//
// It writes one file per system (linux.lib, darwin.lib). Embedding takes minutes.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AniketR10/loxx/internal/embed"
	"github.com/AniketR10/loxx/internal/library"
)

// tldrCommit pins the pages; a commit hash can't change content without changing.
const tldrCommit = "273668766e52afe5307483ab06b65198794a5932"

// systems maps each output file to its tldr page folders; the first folder wins on duplicates.
var systems = []struct{ file, dirs string }{
	{"linux.lib", "linux common"},
	{"darwin.lib", "osx common"},
}

func main() {
	tldr := flag.String("tldr", "", "tldr-pages checkout at the pinned commit")
	out := flag.String("out", "internal/library/data", "folder for the library files")
	flag.Parse()
	if *tldr == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*tldr, *out); err != nil {
		fmt.Fprintln(os.Stderr, "library:", err)
		os.Exit(1)
	}
}

func run(tldr, out string) error {
	head, err := exec.Command("git", "-C", tldr, "rev-parse", "HEAD").Output()
	if err != nil {
		return fmt.Errorf("reading the tldr commit: %w", err)
	}
	if got := strings.TrimSpace(string(head)); got != tldrCommit {
		return fmt.Errorf("tldr checkout is at %s, want %s", got, tldrCommit)
	}
	model, err := embed.Default()
	if err != nil {
		return err
	}
	cache := map[string][]float32{} // texts shared by both systems are embedded once
	for _, sys := range systems {
		entries, texts, err := collect(tldr, strings.Fields(sys.dirs))
		if err != nil {
			return err
		}
		start := time.Now()
		vectors := embedAll(model, texts, cache)
		var buf bytes.Buffer
		if err := library.Write(&buf, embed.ModelID, "tldr-pages@"+tldrCommit[:12], entries, vectors); err != nil {
			return err
		}
		path := filepath.Join(out, sys.file)
		if err := os.MkdirAll(out, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			return err
		}
		fmt.Printf("%s: %d examples, %.1f MB, embedded in %s\n", path, len(entries), float64(buf.Len())/1e6, time.Since(start).Round(time.Second))
	}
	return nil
}

// collect reads the pages of dirs; a page name seen in an earlier dir is skipped.
func collect(tldr string, dirs []string) ([]library.Entry, []string, error) {
	var entries []library.Entry
	var texts []string
	seen := map[string]bool{}
	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(tldr, "pages", dir, "*.md"))
		if err != nil || len(files) == 0 {
			return nil, nil, fmt.Errorf("no pages in %s/pages/%s", tldr, dir)
		}
		sort.Strings(files)
		for _, f := range files {
			name := filepath.Base(f)
			if seen[name] {
				continue
			}
			seen[name] = true
			b, err := os.ReadFile(f)
			if err != nil {
				return nil, nil, err
			}
			p := library.ParsePage(string(b))
			for _, ex := range p.Examples {
				entries = append(entries, library.Entry{
					Tool: p.Tool(), Requires: p.Requires, Command: ex.Command, Cursor: ex.Cursor, Desc: ex.Desc,
				})
				// The bake-off's best text: tool + description + the page's summary.
				texts = append(texts, p.Name+": "+ex.Desc+". "+p.Summary)
			}
		}
	}
	return entries, texts, nil
}

func embedAll(m *embed.Model, texts []string, cache map[string][]float32) [][]float32 {
	var todo []string
	for _, t := range texts {
		if _, ok := cache[t]; !ok {
			cache[t] = nil
			todo = append(todo, t)
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	next := make(chan string)
	for range runtime.NumCPU() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range next {
				v := m.Embed(t)
				mu.Lock()
				cache[t] = v
				mu.Unlock()
			}
		}()
	}
	for _, t := range todo {
		next <- t
	}
	close(next)
	wg.Wait()
	vectors := make([][]float32, len(texts))
	for i, t := range texts {
		vectors[i] = cache[t]
	}
	return vectors
}
