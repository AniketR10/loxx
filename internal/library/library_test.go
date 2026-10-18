package library

import (
	"bytes"
	"math"
	"reflect"
	"testing"

	"github.com/AniketR10/loxx/internal/embed"
)

func TestF16RoundTrip(t *testing.T) {
	for h := 0; h < 1<<16; h++ {
		f := embed.F16ToF32(uint16(h))
		if math.IsNaN(float64(f)) {
			continue
		}
		if got := f32ToF16(f); got != uint16(h) {
			t.Fatalf("%#04x → %v → %#04x", h, f, got)
		}
	}
	for _, c := range []struct {
		f    float32
		want uint16
	}{
		{1.0 + 1.0/2048, 0x3c00}, // halfway, rounds to even
		{1.0 + 3.0/2048, 0x3c02}, // halfway, rounds to even (up)
		{65520, 0x7c00},          // overflows to Inf
		{1e-9, 0},                // underflows to zero
		{float32(math.NaN()), 0x7e00},
	} {
		if got := f32ToF16(c.f); got != c.want {
			t.Errorf("f32ToF16(%v) = %#04x, want %#04x", c.f, got, c.want)
		}
	}
}

func TestWriteRead(t *testing.T) {
	entries := []Entry{
		{Tool: "git", Command: "git stash", Cursor: 9, Desc: "Stash changes"},
		{Tool: "git", Requires: "git-extras", Command: "git undo n", Cursor: 9, Desc: "Undo commits"},
	}
	vectors := [][]float32{{0.5, -0.25, 1}, {0, 0.125, -1}}
	var buf bytes.Buffer
	if err := Write(&buf, "model@1", "tldr@abc", entries, vectors); err != nil {
		t.Fatal(err)
	}
	l, err := Read(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if l.ModelID != "model@1" || l.Source != "tldr@abc" || l.Dims() != 3 || !reflect.DeepEqual(l.Entries, entries) {
		t.Fatalf("read back %+v", l)
	}
	got := make([]float32, 3)
	for i, want := range vectors {
		l.Vector(i, got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("vector %d = %v, want %v", i, got, want)
		}
	}
	if _, err := Read(buf.Bytes()[:buf.Len()-1]); err == nil {
		t.Error("a truncated file was accepted")
	}
	if _, err := Read([]byte("not a library")); err == nil {
		t.Error("a non-library file was accepted")
	}
}

const rebasePage = "# git rebase\n\n" +
	"> Reapply commits from one branch on top of another branch.\n" +
	"> More information: <https://git-scm.com/docs/git-rebase>.\n\n" +
	"- Start an [i]nteractive rebase:\n\n" +
	"`git rebase {{[-i|--interactive]}} {{target_base_branch_or_commit_hash}}`\n\n" +
	"- Continue a rebase:\n\n" +
	"`git rebase --continue`\n\n" +
	"- Format with literal braces:\n\n" +
	"`docker ps --format \"\\{\\{.ID\\}\\}\" {{[-a|--all]}}`\n"

func TestParsePage(t *testing.T) {
	p := ParsePage(rebasePage)
	want := Page{
		Name:    "git rebase",
		Summary: "Reapply commits from one branch on top of another branch.",
		Examples: []Example{
			{"Start an interactive rebase", "git rebase -i target_base_branch_or_commit_hash", 14},
			{"Continue a rebase", "git rebase --continue", 21},
			{"Format with literal braces", `docker ps --format "{{.ID}}" -a`, 31},
		},
	}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("got  %+v\nwant %+v", p, want)
	}
	if p.Tool() != "git" {
		t.Errorf("Tool() = %q", p.Tool())
	}
}

func TestParsePageAddOnAndAlias(t *testing.T) {
	p := ParsePage("# git undo\n\n> Undo recent commits.\n> Part of `git-extras`.\n\n- Undo the last commit:\n\n`git undo`\n")
	if p.Requires != "git-extras" || len(p.Examples) != 1 {
		t.Errorf("add-on page: %+v", p)
	}
	alias := ParsePage("# gcc\n\n> This command is an alias of `cc`.\n\n- View documentation for the original command:\n\n`tldr cc`\n")
	if len(alias.Examples) != 0 {
		t.Errorf("alias page kept %+v", alias.Examples)
	}
}

func TestCleanCommandCursor(t *testing.T) {
	for _, c := range []struct {
		in, out string
		cursor  int
	}{
		{"git push {{remote_name}} {{[-d|--delete]}} {{branch}}", "git push remote_name -d branch", 9},
		{"ls {{[-la|--all]}}", "ls -la", 6},
		{"tail {{[-n|--lines]}} {{10}} {{path/to/file}}", "tail -n 10 path/to/file", 8},
		{"echo {{unterminated", "echo {{unterminated", 19},
	} {
		got, cursor := cleanCommand(c.in)
		if got != c.out || cursor != c.cursor {
			t.Errorf("cleanCommand(%q) = %q, %d; want %q, %d", c.in, got, cursor, c.out, c.cursor)
		}
	}
}
