package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{name: "help", args: []string{"help"}, wantCode: 0, wantStdout: "Usage: loxx"},
		{name: "--help", args: []string{"--help"}, wantCode: 0, wantStdout: "Usage: loxx"},
		{name: "version", args: []string{"version"}, wantCode: 0, wantStdout: "loxx dev"},
		{name: "version extra arg", args: []string{"version", "x"}, wantCode: 2, wantStderr: "takes no arguments"},
		{name: "unknown", args: []string{"nope"}, wantCode: 2, wantStderr: `unknown command "nope"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantStdout)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

func runOK(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := run(args, &out, &errOut); code != 0 {
		t.Fatalf("loxx %s: exit %d\nstderr: %s", strings.Join(args, " "), code, errOut.String())
	}
	return out.String(), errOut.String()
}

func TestAddAndSearch(t *testing.T) {
	t.Setenv("LOXX_DB_PATH", filepath.Join(t.TempDir(), "history.db"))

	runOK(t, "add", "--cwd", "/srv/api", "--exit", "0", "docker run -v pgdata:/var/lib/postgresql/data postgres:16")
	_, stderr := runOK(t, "add", "--exit", "1", "export DB_PASSWORD=hunter2")
	if !strings.Contains(stderr, "redacted 1 secret") {
		t.Errorf("expected a redaction notice, got %q", stderr)
	}
	_, stderr = runOK(t, "add", " echo private")
	if !strings.Contains(stderr, "not recorded") {
		t.Errorf("expected a not-recorded notice, got %q", stderr)
	}

	stdout, _ := runOK(t, "search", "docker", "postg")
	if !strings.Contains(stdout, "docker run -v pgdata") || !strings.Contains(stdout, "/srv/api · just now · exit 0 · 1 run") {
		t.Errorf("unexpected search output:\n%s", stdout)
	}

	stdout, _ = runOK(t, "search", "DB_PASSWORD")
	if strings.Contains(stdout, "hunter2") || !strings.Contains(stdout, "<REDACTED:secret-env>") {
		t.Errorf("secret visible in search output:\n%s", stdout)
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"search", "private"}, &out, &errOut); code != 1 {
		t.Errorf("searching an ignored command: exit %d, want 1 (no matches)", code)
	}

	// After embedding, search also finds commands by meaning.
	runOK(t, "add", "sudo systemctl restart nginx")
	runOK(t, "embed", "--pending")
	if stdout, _ = runOK(t, "search", "--limit", "1", "restart", "the", "web", "server"); !strings.HasPrefix(stdout, "sudo systemctl restart nginx\n") {
		t.Errorf("semantic search: got\n%s", stdout)
	}
}

func TestAddUsageErrors(t *testing.T) {
	t.Setenv("LOXX_DB_PATH", filepath.Join(t.TempDir(), "history.db"))
	for _, args := range [][]string{
		{"add"},
		{"add", "one", "two"},
		{"add", "--exit", "x", "ls"},
		{"add", ""},
		{"search"},
		{"search", "--limit", "0", "ls"},
	} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 2 {
			t.Errorf("loxx %q: exit %d, want 2", args, code)
		}
	}
}

func TestEmbedPending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	t.Setenv("LOXX_DB_PATH", path)
	runOK(t, "add", "git status")
	runOK(t, "add", "docker compose up -d")

	_, stderr := runOK(t, "embed", "--pending", "--workers", "2")
	if !strings.Contains(stderr, "embedded 2 command(s)") {
		t.Errorf("first run: stderr = %q", stderr)
	}
	// Nothing left to do: a second run is silent.
	if _, stderr = runOK(t, "embed", "--pending"); stderr != "" {
		t.Errorf("second run should be silent, got %q", stderr)
	}

	// While another embed holds the lock, a new one exits cleanly.
	runOK(t, "add", "ls -la")
	unlock, err := tryLock(path + ".embed.lock")
	if err != nil {
		t.Fatal(err)
	}
	_, stderr = runOK(t, "embed", "--pending")
	unlock()
	if !strings.Contains(stderr, "already running") {
		t.Errorf("locked run: stderr = %q", stderr)
	}
	if _, stderr = runOK(t, "embed", "--pending"); !strings.Contains(stderr, "embedded 1 command(s)") {
		t.Errorf("after unlock: stderr = %q", stderr)
	}
}

func TestEmbedUsage(t *testing.T) {
	t.Setenv("LOXX_DB_PATH", filepath.Join(t.TempDir(), "history.db"))
	for _, args := range [][]string{{"embed"}, {"embed", "--pending", "x"}, {"embed", "--pending", "--workers", "0"}} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 2 {
			t.Errorf("loxx %q: exit %d, want 2", args, code)
		}
	}
}
