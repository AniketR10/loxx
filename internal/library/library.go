// Package library is loxx's built-in command library: tldr-pages examples with
// their meanings computed ahead of time, so search can suggest commands that
// were never run here.
package library

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/AniketR10/loxx/internal/embed"
)

const magic = "LOXXLIB1"

// Entry is one example command.
type Entry struct {
	Tool     string // the program it runs, which must be on PATH
	Requires string // add-on that must be on PATH instead ("git-extras"), or ""
	Command  string // placeholders as plain words
	Cursor   int    // byte offset of the first placeholder, or len(Command)
	Desc     string // what it does, from tldr
}

type meta struct {
	ModelID string
	Source  string
	Dims    int
	Entries []Entry
}

// Library is a loaded library file.
type Library struct {
	ModelID string // model that made the vectors
	Source  string // where the examples came from
	Entries []Entry
	dims    int
	vectors []byte // f16, little-endian, dims per entry
}

// Write stores entries and their vectors (one per entry) in the library format.
func Write(w io.Writer, modelID, source string, entries []Entry, vectors [][]float32) error {
	if len(entries) != len(vectors) || len(vectors) == 0 {
		return fmt.Errorf("library: %d entries, %d vectors", len(entries), len(vectors))
	}
	dims := len(vectors[0])
	var m bytes.Buffer
	if err := gob.NewEncoder(&m).Encode(meta{modelID, source, dims, entries}); err != nil {
		return err
	}
	out := bytes.NewBufferString(magic)
	binary.Write(out, binary.LittleEndian, uint32(m.Len()))
	out.Write(m.Bytes())
	var h [2]byte
	for i, v := range vectors {
		if len(v) != dims {
			return fmt.Errorf("library: vector %d has %d values, want %d", i, len(v), dims)
		}
		for _, f := range v {
			binary.LittleEndian.PutUint16(h[:], f32ToF16(f))
			out.Write(h[:])
		}
	}
	_, err := out.WriteTo(w)
	return err
}

// Read loads a library file. The vectors stay in b, which must not change.
func Read(b []byte) (*Library, error) {
	if len(b) < len(magic)+4 || string(b[:len(magic)]) != magic {
		return nil, errors.New("library: not a library file")
	}
	n := int(binary.LittleEndian.Uint32(b[len(magic):]))
	rest := b[len(magic)+4:]
	if n > len(rest) {
		return nil, errors.New("library: truncated")
	}
	var m meta
	if err := gob.NewDecoder(bytes.NewReader(rest[:n])).Decode(&m); err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	vectors := rest[n:]
	if m.Dims <= 0 || len(vectors) != len(m.Entries)*m.Dims*2 {
		return nil, fmt.Errorf("library: %d bytes of vectors for %d entries", len(vectors), len(m.Entries))
	}
	return &Library{m.ModelID, m.Source, m.Entries, m.Dims, vectors}, nil
}

// Vector decodes entry i's vector into dst, which must hold the library's dims.
func (l *Library) Vector(i int, dst []float32) {
	b := l.vectors[i*l.dims*2:]
	for k := range dst[:l.dims] {
		dst[k] = embed.F16ToF32(binary.LittleEndian.Uint16(b[2*k:]))
	}
}

// Dims is the length of each vector.
func (l *Library) Dims() int { return l.dims }

// f32ToF16 converts to IEEE 754 half precision, rounding to nearest even.
func f32ToF16(f float32) uint16 {
	b := math.Float32bits(f)
	sign := uint16(b>>16) & 0x8000
	exp := int(b>>23&0xff) - 127 + 15
	mant := b & 0x7fffff
	switch {
	case b&0x7fffffff > 0x7f800000: // NaN
		return sign | 0x7e00
	case exp >= 0x1f: // too large: ±Inf
		return sign | 0x7c00
	case exp <= 0: // subnormal or zero
		if exp < -10 {
			return sign
		}
		mant |= 0x800000
		shift := uint(14 - exp)
		h := uint16(mant >> shift)
		rem, half := mant&(1<<shift-1), uint32(1)<<(shift-1)
		if rem > half || rem == half && h&1 == 1 {
			h++
		}
		return sign | h
	}
	h := uint16(exp)<<10 | uint16(mant>>13)
	if rem := mant & 0x1fff; rem > 0x1000 || rem == 0x1000 && h&1 == 1 {
		h++ // a carry into the exponent is still correct
	}
	return sign | h
}

// Default returns the library built into this binary, or nil if there is none.
var Default = sync.OnceValues(func() (*Library, error) {
	if len(data) == 0 {
		return nil, nil
	}
	return Read(data)
})

// Installed returns the indexes of entries whose program is in one of the
// directories of path (a $PATH value).
func (l *Library) Installed(path string) []int {
	have := map[string]bool{}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue // POSIX: the current directory, which loxx never trusts
		}
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			have[f.Name()] = true
		}
	}
	var rows []int
	for i, e := range l.Entries {
		need := e.Tool
		if e.Requires != "" {
			need = e.Requires
		}
		if have[need] {
			rows = append(rows, i)
		}
	}
	return rows
}
