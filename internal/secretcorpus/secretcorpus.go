// Package secretcorpus loads the scrubber test corpus from testdata/secrets.
// It is only meant to be imported by tests.
package secretcorpus

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Positive is a command containing secrets that must never survive scrubbing.
type Positive struct {
	Cmd     string   `json:"cmd"`
	Secrets []string `json:"secrets"`
}

// Negative is a command that the scrubber must leave unchanged.
type Negative struct {
	Cmd string `json:"cmd"`
}

// Positives returns every entry in testdata/secrets/positive.jsonl.
func Positives() ([]Positive, error) {
	return load[Positive]("positive.jsonl")
}

// Negatives returns every entry in testdata/secrets/negative.jsonl.
func Negatives() ([]Negative, error) {
	return load[Negative]("negative.jsonl")
}

func load[T any](name string) ([]T, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return nil, fmt.Errorf("secretcorpus: cannot locate source file")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "testdata", "secrets", name)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []T
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var e T
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", name, line, err)
		}
		entries = append(entries, e)
	}
	return entries, sc.Err()
}
