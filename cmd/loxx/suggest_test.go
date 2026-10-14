package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// suggestions runs `loxx suggest` and splits its records.
func suggestions(t *testing.T, args ...string) (cmds, details, status []string) {
	t.Helper()
	stdout, _ := runOK(t, append([]string{"suggest"}, args...)...)
	if stdout != "" && !strings.HasSuffix(stdout, suggestRecordSep) {
		t.Fatalf("output must end with a record separator: %q", stdout)
	}
	for _, rec := range strings.Split(strings.TrimSuffix(stdout, suggestRecordSep), suggestRecordSep) {
		if rec == "" {
			continue
		}
		f := strings.Split(rec, suggestFieldSep)
		if len(f) != 3 {
			t.Fatalf("record without 3 fields: %q", rec)
		}
		cmds = append(cmds, f[0])
		details = append(details, f[1])
		status = append(status, f[2])
	}
	return cmds, details, status
}

func TestSuggest(t *testing.T) {
	t.Setenv("LOXX_DB_PATH", filepath.Join(t.TempDir(), "history.db"))
	runOK(t, "add", "--cwd", "/srv/shop", "--exit", "0", "git push origin main")
	runOK(t, "add", "--cwd", "/srv/shop", "--exit", "0", "git push")
	runOK(t, "add", "--exit", "1", "git pull --rebase")
	runOK(t, "add", "--exit", "0", "for f in *.log; do\n  gzip \"$f\"\ndone")

	// Keyword stage: every word, each also as a word beginning.
	cmds, details, status := suggestions(t, "--keyword-only", "--", "git pu")
	if len(cmds) != 3 {
		t.Fatalf("git pu: got %q, want the 3 git push/pull commands", cmds)
	}
	for i, d := range details {
		if strings.Contains(d, "exit") || !strings.Contains(d, "just now") {
			t.Errorf("details for %q should be like the panel's (folder · when · N×): %q", cmds[i], d)
		}
		want := map[string]string{"git pull --rebase": "fail"}[cmds[i]]
		if want == "" {
			want = "ok"
		}
		if status[i] != want {
			t.Errorf("status of %q = %q, want %q", cmds[i], status[i], want)
		}
	}
	if !strings.Contains(strings.Join(details, "|"), "/srv/shop · just now") {
		t.Errorf("details should start with the folder: %q", details)
	}

	// The line you already typed is never suggested back to you.
	cmds, _, _ = suggestions(t, "--keyword-only", "--", "git push")
	for _, c := range cmds {
		if c == "git push" {
			t.Errorf("suggested the typed line itself: %q", cmds)
		}
	}
	if len(cmds) != 1 || cmds[0] != "git push origin main" {
		t.Errorf("git push: got %q", cmds)
	}

	// A multi-line command comes back whole, newlines and all.
	if cmds, _, _ = suggestions(t, "--keyword-only", "--", "gzip"); len(cmds) != 1 || !strings.Contains(cmds[0], "\n  gzip") {
		t.Errorf("multi-line command: got %q", cmds)
	}

	// --limit, and nothing at all for an empty line.
	if cmds, _, _ = suggestions(t, "--keyword-only", "--limit", "1", "--", "git"); len(cmds) != 1 {
		t.Errorf("--limit 1: got %q", cmds)
	}
	if stdout, _ := runOK(t, "suggest", "--", "   "); stdout != "" {
		t.Errorf("empty line: got %q", stdout)
	}

	// The full stage also finds commands by meaning.
	runOK(t, "add", "sudo systemctl restart nginx")
	runOK(t, "embed", "--pending")
	if cmds, _, _ = suggestions(t, "--", "restart", "the", "web", "server"); len(cmds) == 0 || cmds[0] != "sudo systemctl restart nginx" {
		t.Errorf("by meaning: got %q", cmds)
	}
}
