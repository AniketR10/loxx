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

	// bash-preexec 0.7.0 (MIT), vendored unmodified.
	//go:embed bash-preexec.sh
	bashPreexec string

	//go:embed bash-preexec.LICENSE
	bashPreexecLicense string
)

// bashPreexecEnd ends the here-document that loxx.bash sources bash-preexec
// from; it must never occur in bash-preexec itself.
const bashPreexecEnd = "__LOXX_BASH_PREEXEC__"

// Script returns the hook for shell with binPath filled in.
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
	return guarded(strings.Replace(s, "__LOXX_BIN__", quote(binPath), 1), shell)
}

// hookEnd ends the here-document in guarded; no hook may contain it.
const hookEnd = "__LOXX_HOOK_END__"

// guarded makes a hook run only in its own shell; elsewhere it prints which
// init to use. The wrong shell never parses it (it's a quoted here-document).
func guarded(script, shell string) (string, error) {
	if strings.Contains(script, hookEnd) {
		return "", fmt.Errorf("the %s hook contains %s", shell, hookEnd)
	}
	version, other := "ZSH_VERSION", "bash"
	if shell == "bash" {
		version, other = "BASH_VERSION", "zsh"
	}
	return fmt.Sprintf(`if [ -n "${%[1]s-}" ]; then
IFS= read -r -d '' _loxx_hook <<'%[2]s'
%[3]s
%[2]s
eval "$_loxx_hook"
unset _loxx_hook
else
  printf '%%s\n' 'loxx: this is the %[4]s hook, but this shell is not %[4]s. Use: eval "$(loxx init %[5]s)"' >&2
fi
`, version, hookEnd, strings.TrimRight(script, "\n"), shell, other), nil
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
