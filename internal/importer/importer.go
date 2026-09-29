// Package importer reads existing bash and zsh history files.
package importer

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"
)

// Entry is one command from a history file.
type Entry struct {
	Command   string
	StartedAt time.Time      // zero when the file records no time
	Duration  *time.Duration // only zsh's extended history records it
}

// ParseBash reads a bash history file.
//
// With HISTTIMEFORMAT set, bash writes "#<epoch seconds>" before each entry,
// and the lines up to the next timestamp form one command (bash writes
// multi-line commands that way). Without timestamps there is no way to tell a
// multi-line command from separate ones, so every line is its own command.
func ParseBash(r io.Reader) ([]Entry, error) {
	var (
		entries []Entry
		pending *Entry // command being assembled after a timestamp
	)
	flush := func() {
		if pending != nil && strings.TrimSpace(pending.Command) != "" {
			entries = append(entries, *pending)
		}
		pending = nil
	}
	err := eachLine(r, func(line []byte) {
		text := string(line)
		if sec, ok := bashTimestamp(text); ok {
			flush()
			pending = &Entry{StartedAt: time.Unix(sec, 0)}
			return
		}
		if pending != nil {
			if pending.Command != "" {
				pending.Command += "\n"
			}
			pending.Command += text
			return
		}
		if strings.TrimSpace(text) != "" {
			entries = append(entries, Entry{Command: text})
		}
	})
	flush()
	return entries, err
}

// bashTimestamp reports whether line is a bash history timestamp ("#" and
// digits only, as bash itself checks) and returns the seconds.
func bashTimestamp(line string) (int64, bool) {
	if len(line) < 2 || line[0] != '#' {
		return 0, false
	}
	for _, c := range line[1:] {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	sec, err := strconv.ParseInt(line[1:], 10, 64)
	return sec, err == nil
}

// ParseZsh reads a zsh history file, plain or EXTENDED_HISTORY
// (": <start>:<elapsed>;<command>").
//
// zsh stores a newline inside a command as a backslash at the end of the
// line, and stores the bytes 0x00 and 0x83-0xA2 (common inside UTF-8
// characters) "metafied": the byte 0x83 followed by the original byte XOR
// 0x20. Both are undone here.
func ParseZsh(r io.Reader) ([]Entry, error) {
	var (
		entries []Entry
		cmd     []byte
		inCmd   bool
	)
	err := eachLine(r, func(line []byte) {
		line = unmetafy(line)
		if !inCmd {
			cmd = cmd[:0]
			inCmd = true
		}
		if bytes.HasSuffix(line, []byte{'\\'}) {
			cmd = append(cmd, line[:len(line)-1]...)
			cmd = append(cmd, '\n')
			return
		}
		cmd = append(cmd, line...)
		inCmd = false
		if e, ok := zshEntry(string(cmd)); ok {
			entries = append(entries, e)
		}
	})
	if inCmd {
		if e, ok := zshEntry(string(cmd)); ok {
			entries = append(entries, e)
		}
	}
	return entries, err
}

// zshEntry parses one complete history entry, extended or plain.
func zshEntry(s string) (Entry, bool) {
	var e Entry
	if rest, ok := strings.CutPrefix(s, ": "); ok {
		// ": <start>:<elapsed>;<command>"
		meta, command, ok := strings.Cut(rest, ";")
		startStr, elapsedStr, ok2 := strings.Cut(meta, ":")
		start, err1 := strconv.ParseInt(startStr, 10, 64)
		elapsed, err2 := strconv.ParseInt(elapsedStr, 10, 64)
		if ok && ok2 && err1 == nil && err2 == nil {
			e.StartedAt = time.Unix(start, 0)
			d := time.Duration(elapsed) * time.Second
			e.Duration = &d
			s = command
		}
	}
	e.Command = s
	return e, strings.TrimSpace(s) != ""
}

// unmetafy undoes zsh's metafied encoding in history files.
func unmetafy(b []byte) []byte {
	const meta = 0x83
	if bytes.IndexByte(b, meta) < 0 {
		return b
	}
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] == meta && i+1 < len(b) {
			i++
			out = append(out, b[i]^0x20)
			continue
		}
		out = append(out, b[i])
	}
	return out
}

// eachLine calls fn for every line of r without its line ending. Lines may be
// any length, unlike bufio.Scanner's default limit.
func eachLine(r io.Reader, fn func(line []byte)) error {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			line = bytes.TrimSuffix(line, []byte{'\n'})
			line = bytes.TrimSuffix(line, []byte{'\r'})
			fn(line)
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
