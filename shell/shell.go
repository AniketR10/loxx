// Package shell holds the shell integration scripts that `loxx init` prints.
package shell

import (
	_ "embed"
	"fmt"
	"strings"
)

var (
	//go:embed loxx.zsh
	zshScript string

	//go:embed loxx.bash
	bashScript string

	// bash-preexec 0.7.0 (MIT), vendored unmodified from
	// https://github.com/rcaloras/bash-preexec at tag 0.7.0.
	//go:embed bash-preexec.sh
	bashPreexec string

	//go:embed bash-preexec.LICENSE
	bashPreexecLicense string
)

// bashPreexecEnd ends the here-document that loxx.bash sources bash-preexec
// from; it must never occur in bash-preexec itself.
const bashPreexecEnd = "__LOXX_BASH_PREEXEC__"

// Script returns the integration script for shell ("bash" or "zsh"), with
// binPath, the absolute path of the loxx binary, filled in so the hooks don't
// depend on $PATH.
func Script(shell, binPath string) (string, error) {
	var s string
	switch shell {
	case "zsh":
		s = zshScript
	case "bash":
		if strings.Contains(bashPreexec, bashPreexecEnd) {
			return "", fmt.Errorf("vendored bash-preexec contains %s", bashPreexecEnd)
		}
		s = strings.Replace(bashScript, "__BASH_PREEXEC_LICENSE__", comment(bashPreexecLicense), 1)
		s = strings.Replace(s, "__BASH_PREEXEC__", strings.TrimRight(bashPreexec, "\n"), 1)
	default:
		return "", fmt.Errorf("unsupported shell %q (supported: bash, zsh)", shell)
	}
	return strings.Replace(s, "__LOXX_BIN__", quote(binPath), 1), nil
}

// quote returns s as a single-quoted shell word, safe in bash and zsh.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// comment turns text into shell comment lines.
func comment(text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight("# "+l, " ")
	}
	return strings.Join(lines, "\n")
}
