package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// In tests os.Executable is the test binary: never let record/import
	// launch it as a background "loc embed".
	os.Setenv("LOC_BACKGROUND_EMBED", "0")
	os.Exit(m.Run())
}

// execution is one executions row, read straight from the database.
type execution struct {
	text                 string
	cwd, gitRoot         sql.NullString
	exitCode, durationMS sql.NullInt64
	startedAt            sql.NullInt64
	session, source      sql.NullString
}

func executions(t *testing.T, dbPath string) []execution {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT c.text, e.cwd, e.git_root, e.exit_code, e.duration_ms, e.started_at, e.session_id, e.source
		FROM executions e JOIN commands c ON c.id = e.command_id ORDER BY e.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []execution
	for rows.Next() {
		var e execution
		if err := rows.Scan(&e.text, &e.cwd, &e.gitRoot, &e.exitCode, &e.durationMS, &e.startedAt, &e.session, &e.source); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

func TestRecord(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "history.db")
	t.Setenv("LOC_DB_PATH", dbPath)
	repo := filepath.Join(dir, "repo")
	sub := filepath.Join(repo, "src", "pkg")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	runOK(t, "record", "--exit", "3", "--start", "1759000000.5", "--end", "1759000002,75",
		"--cwd", sub, "--session", "s1", "--", "make test DB_PASSWORD=hunter2")
	// Ignored and empty commands are dropped silently, with exit status 0.
	for _, cmd := range []string{" secret-thing", "   ", ""} {
		if _, stderr := runOK(t, "record", "--", cmd); stderr != "" {
			t.Errorf("record %q printed %q", cmd, stderr)
		}
	}

	got := executions(t, dbPath)
	if len(got) != 1 {
		t.Fatalf("got %d executions, want 1: %+v", len(got), got)
	}
	e := got[0]
	if e.text != "make test DB_PASSWORD=<REDACTED:secret-env>" {
		t.Errorf("text = %q (must be scrubbed)", e.text)
	}
	if e.cwd.String != sub || e.gitRoot.String != repo {
		t.Errorf("cwd = %q, git root = %q; want %q and %q", e.cwd.String, e.gitRoot.String, sub, repo)
	}
	if e.exitCode.Int64 != 3 || e.durationMS.Int64 != 2250 || e.startedAt.Int64 != 1759000000500 {
		t.Errorf("exit = %d, duration = %d ms, started = %d; want 3, 2250, 1759000000500",
			e.exitCode.Int64, e.durationMS.Int64, e.startedAt.Int64)
	}
	if e.session.String != "s1" || e.source.String != "live" {
		t.Errorf("session = %q, source = %q", e.session.String, e.source.String)
	}
}

func TestRecordUsage(t *testing.T) {
	t.Setenv("LOC_DB_PATH", filepath.Join(t.TempDir(), "history.db"))
	for _, args := range [][]string{
		{"record"},
		{"record", "--", "a", "b"},
		{"record", "--exit", "x", "--", "ls"},
		{"record", "--start", "yesterday", "--", "ls"},
		{"record", "--start", "-5", "--", "ls"},
	} {
		if code := run(args, &strings.Builder{}, &strings.Builder{}); code != 2 {
			t.Errorf("loc %q: exit %d, want 2", args, code)
		}
	}
}

func TestParseEpoch(t *testing.T) {
	for in, want := range map[string]time.Time{
		"":                  {},
		"1759000000":        time.Unix(1759000000, 0),
		"1759000000.123456": time.Unix(1759000000, 123456000),
		"1759000000,123456": time.Unix(1759000000, 123456000), // bash EPOCHREALTIME in some locales
	} {
		got, err := parseEpoch(in)
		if err != nil || !got.Equal(want) {
			t.Errorf("parseEpoch(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
}

func TestGitRoot(t *testing.T) {
	dir := t.TempDir()
	worktree := filepath.Join(dir, "wt")
	if err := os.MkdirAll(filepath.Join(worktree, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	// In a worktree or submodule, .git is a file.
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := gitRoot(filepath.Join(worktree, "a")); got != worktree {
		t.Errorf("gitRoot in worktree = %q, want %q", got, worktree)
	}
	if got := gitRoot(dir); got != "" {
		t.Errorf("gitRoot outside any repo = %q, want \"\"", got)
	}
	if got := gitRoot("relative/path"); got != "" {
		t.Errorf("gitRoot(relative) = %q, want \"\"", got)
	}
}

func TestImport(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "history.db")
	t.Setenv("LOC_DB_PATH", dbPath)
	bashHist := filepath.Join(dir, "bash_history")
	zshHist := filepath.Join(dir, "zsh_history")
	if err := os.WriteFile(bashHist, []byte("git status\nexport API_TOKEN=abc123def456ghi\ngit status\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(zshHist, []byte(": 1759000000:4;make build\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr := runOK(t, "import", "--bash", bashHist, "--zsh", zshHist)
	if !strings.Contains(stderr, "imported 3 command(s)") || !strings.Contains(stderr, "1 secret(s) redacted") ||
		!strings.Contains(stderr, "imported 1 command(s)") {
		t.Errorf("import output: %q", stderr)
	}

	got := executions(t, dbPath)
	if len(got) != 4 {
		t.Fatalf("got %d executions, want 4", len(got))
	}
	for _, e := range got[:3] { // bash: no timestamps, per the user's decision
		if e.startedAt.Valid || e.source.String != "import" {
			t.Errorf("bash import %q: started_at valid=%v, source=%q", e.text, e.startedAt.Valid, e.source.String)
		}
	}
	if z := got[3]; z.startedAt.Int64 != 1759000000000 || z.durationMS.Int64 != 4000 {
		t.Errorf("zsh import: started %d, duration %d", z.startedAt.Int64, z.durationMS.Int64)
	}
	if strings.Contains(got[1].text, "abc123def456ghi") {
		t.Error("imported secret was not scrubbed")
	}

	// Imported commands show "imported", not an invented time.
	if stdout, _ := runOK(t, "search", "git", "status"); !strings.Contains(stdout, "imported · 2 runs") {
		t.Errorf("search output:\n%s", stdout)
	}

	// A second import is skipped; --force imports again.
	if _, stderr = runOK(t, "import", "--bash", bashHist); !strings.Contains(stderr, "already imported") {
		t.Errorf("second import: %q", stderr)
	}
	runOK(t, "import", "--force", "--bash", bashHist)
	if n := len(executions(t, dbPath)); n != 7 {
		t.Errorf("after --force: %d executions, want 7", n)
	}

	// A file named explicitly must exist.
	if code := run([]string{"import", "--bash", filepath.Join(dir, "missing")}, &strings.Builder{}, &strings.Builder{}); code != 1 {
		t.Errorf("missing file: exit %d, want 1", code)
	}
}

func TestInit(t *testing.T) {
	var out, errOut strings.Builder
	if code := run([]string{"init", "zsh"}, &out, &errOut); code != 0 {
		t.Fatalf("init zsh: exit %d: %s", code, errOut.String())
	}
	self, _ := os.Executable()
	if !strings.Contains(out.String(), "_loc_bin='"+self+"'") || !strings.Contains(out.String(), "add-zsh-hook preexec _loc_preexec") {
		t.Errorf("zsh script missing the binary path or hooks:\n%s", out.String())
	}
	if strings.Contains(out.String(), "__LOC_BIN__") {
		t.Error("placeholder was not replaced")
	}
	if code := run([]string{"init", "bash"}, &strings.Builder{}, &strings.Builder{}); code != 0 {
		t.Errorf("init bash: exit %d", code)
	}
	for _, args := range [][]string{{"init"}, {"init", "fish"}} {
		if code := run(args, &strings.Builder{}, &strings.Builder{}); code != 2 {
			t.Errorf("loc %q: exit %d, want 2", args, code)
		}
	}
}
