// Package setup adds and removes the marked block that loads the loxx hook
// from a shell's rc file. Removing is the exact inverse of adding: after
// uninstall the file is byte-identical to what it was before setup.
package setup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	beginMarker = "# >>> loxx >>>"
	endMarker   = "# <<< loxx <<<"
	// addedNewline is appended to the begin marker when setup had to end the
	// file's last line before adding the block, so removal can take it back.
	addedNewline = " (loxx added the newline before this line)"
	// createdFile is appended to the begin marker when the rc file did not
	// exist, so uninstall removes the file instead of leaving it empty.
	createdFile = " (loxx created this file)"
)

// ErrHandWritten means the rc file already loads the loxx hook outside a
// loxx block. Adding a block too would load the hook twice and record every
// command twice.
var ErrHandWritten = errors.New("the file already loads the loxx hook")

// handWritten matches a line that runs `loxx init bash|zsh`, however the
// binary is spelled (loxx, ~/…/bin/loxx, '/abs/loxx').
var handWritten = regexp.MustCompile(`loxx['"]?\s+init\s+(bash|zsh)\b`)

// RCFile returns the rc file that shell reads at startup in home.
func RCFile(shell, home string) (string, error) {
	switch shell {
	case "bash":
		return filepath.Join(home, ".bashrc"), nil
	case "zsh":
		dir := os.Getenv("ZDOTDIR")
		if dir == "" {
			dir = home
		}
		return filepath.Join(dir, ".zshrc"), nil
	}
	return "", fmt.Errorf("unsupported shell %q", shell)
}

// Block returns the rc block that loads binPath's hook for shell. The [ -x ]
// guard keeps shell startup working if the binary is later removed.
func Block(shell, binPath string) string {
	q := quote(binPath)
	return beginMarker + "\n" +
		"[ -x " + q + " ] && eval \"$(" + q + " init " + shell + ")\"\n" +
		endMarker + "\n"
}

// Add returns content with block added, or with an existing loxx block
// replaced in place (so a moved binary is picked up by re-running setup). It
// returns ErrHandWritten, with the offending line, if the hook is already
// loaded outside a block.
func Add(content, block string) (string, error) {
	start, end, flag, err := findBlock(content)
	if err != nil {
		return "", err
	}
	if start >= 0 {
		block = strings.Replace(block, beginMarker, beginMarker+flag, 1)
		return content[:start] + block + content[end:], nil
	}
	if line, ok := handWrittenLine(content); ok {
		return "", fmt.Errorf("%w: %q", ErrHandWritten, line)
	}
	if content != "" && !strings.HasSuffix(content, "\n") {
		return content + "\n" + strings.Replace(block, beginMarker, beginMarker+addedNewline, 1), nil
	}
	return content + block, nil
}

// Remove returns content without its loxx block, restoring it exactly, and
// whether there was a block.
func Remove(content string) (string, bool, error) {
	start, end, flag, err := findBlock(content)
	if err != nil || start < 0 {
		return content, false, err
	}
	before := content[:start]
	if flag == addedNewline {
		before = strings.TrimSuffix(before, "\n")
	}
	return before + content[end:], true, nil
}

// Install adds the block for shell and binPath to the rc file at path,
// creating the file if needed, and reports whether it changed anything.
func Install(path, shell, binPath string) (changed bool, err error) {
	block := Block(shell, binPath)
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		block = strings.Replace(block, beginMarker, beginMarker+createdFile, 1)
	}
	return EditFile(path, func(s string) (string, error) { return Add(s, block) })
}

// Uninstall removes the loxx block from the rc file at path, restoring the
// file exactly: a file that setup created is deleted again. It reports
// whether it changed anything.
func Uninstall(path string) (changed bool, err error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if start, _, flag, err := findBlock(string(b)); err != nil {
		return false, err
	} else if start >= 0 && flag == createdFile {
		if rest, _, _ := Remove(string(b)); rest == "" {
			return true, os.Remove(path)
		}
	}
	return EditFile(path, func(s string) (string, error) {
		out, _, err := Remove(s)
		return out, err
	})
}

// HasBlock reports whether content contains a loxx block.
func HasBlock(content string) bool {
	start, _, _, err := findBlock(content)
	return err == nil && start >= 0
}

// HandWritten reports a line outside any loxx block that loads the hook.
func HandWritten(content string) (string, bool) {
	start, end, _, err := findBlock(content)
	if err == nil && start >= 0 {
		content = content[:start] + content[end:]
	}
	return handWrittenLine(content)
}

// findBlock locates the loxx block: start is the offset of its begin line,
// end the offset just past its end line (including the newline, if any), and
// flag what the begin marker records (addedNewline, createdFile or "").
// start is -1 when there is no block.
func findBlock(content string) (start, end int, flag string, err error) {
	lines := strings.SplitAfter(content, "\n")
	offset := 0
	start = -1
	for _, l := range lines {
		trimmed := strings.TrimRight(l, "\n")
		switch {
		case start < 0 && strings.HasPrefix(trimmed, beginMarker) &&
			(trimmed == beginMarker || trimmed == beginMarker+addedNewline || trimmed == beginMarker+createdFile):
			start, flag = offset, strings.TrimPrefix(trimmed, beginMarker)
		case start >= 0 && trimmed == endMarker:
			return start, offset + len(l), flag, nil
		}
		offset += len(l)
	}
	if start >= 0 {
		return 0, 0, "", errors.New("a loxx block has no end marker; fix the file by hand")
	}
	return -1, -1, "", nil
}

func handWrittenLine(content string) (string, bool) {
	for _, l := range strings.Split(content, "\n") {
		if t := strings.TrimSpace(l); !strings.HasPrefix(t, "#") && handWritten.MatchString(t) {
			return t, true
		}
	}
	return "", false
}

// EditFile applies edit to the file at path (a missing file reads as empty)
// and writes the result back if it changed. Symlinks are followed, so rc
// files managed by dotfile tools stay symlinks, and the file keeps its mode.
func EditFile(path string, edit func(string) (string, error)) (changed bool, err error) {
	target, err := filepath.EvalSymlinks(path)
	if errors.Is(err, os.ErrNotExist) {
		target = path
	} else if err != nil {
		return false, err
	}
	old, err := os.ReadFile(target)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	updated, err := edit(string(old))
	if err != nil || updated == string(old) {
		return false, err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}
	// Write a sibling temp file and rename it over the target, so a crash
	// can never leave a half-written rc file.
	tmp, err := os.CreateTemp(filepath.Dir(target), ".loxx-rc-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(updated); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	return true, os.Rename(tmp.Name(), target)
}

// quote returns s as a single-quoted shell word, safe in bash and zsh.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
