package importer

import (
	"strings"
	"testing"
	"time"
)

func TestParseBashPlain(t *testing.T) {
	entries, err := ParseBash(strings.NewReader("git status\n\n  \ndocker ps\r\nls -la"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"git status", "docker ps", "ls -la"}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries %+v, want %v", len(entries), entries, want)
	}
	for i, e := range entries {
		if e.Command != want[i] || !e.StartedAt.IsZero() || e.Duration != nil {
			t.Errorf("entry %d = %+v, want %q with no time", i, e, want[i])
		}
	}
}

func TestParseBashTimestamps(t *testing.T) {
	in := "#1700000000\ngit status\n#1700000060\nfor i in 1 2; do\n  echo $i\ndone\n#1700000120\n#1700000180\nls\n"
	entries, err := ParseBash(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{Command: "git status", StartedAt: time.Unix(1700000000, 0)},
		{Command: "for i in 1 2; do\n  echo $i\ndone", StartedAt: time.Unix(1700000060, 0)},
		// #1700000120 had no command after it and is dropped.
		{Command: "ls", StartedAt: time.Unix(1700000180, 0)},
	}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries %+v", len(entries), entries)
	}
	for i := range want {
		if entries[i].Command != want[i].Command || !entries[i].StartedAt.Equal(want[i].StartedAt) {
			t.Errorf("entry %d = %+v, want %+v", i, entries[i], want[i])
		}
	}
}

func TestParseBashCommentIsNotTimestamp(t *testing.T) {
	entries, _ := ParseBash(strings.NewReader("#not a timestamp\n# 123\n"))
	if len(entries) != 2 || entries[0].Command != "#not a timestamp" || entries[1].Command != "# 123" {
		t.Errorf("got %+v", entries)
	}
}

func TestParseZsh(t *testing.T) {
	in := ": 1700000000:5;make test\n" + // extended, 5 s
		"git status\n" + // plain line mixed in
		": 1700000100:0;for f in *; do\\\n  echo $f\\\ndone\n" + // multi-line
		":not extended;weird\n" // looks odd but is a plain command
	entries, err := ParseZsh(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("got %d entries %+v", len(entries), entries)
	}
	if e := entries[0]; e.Command != "make test" || !e.StartedAt.Equal(time.Unix(1700000000, 0)) || e.Duration == nil || *e.Duration != 5*time.Second {
		t.Errorf("extended entry = %+v", e)
	}
	if e := entries[1]; e.Command != "git status" || !e.StartedAt.IsZero() || e.Duration != nil {
		t.Errorf("plain entry = %+v", e)
	}
	if e := entries[2]; e.Command != "for f in *; do\n  echo $f\ndone" {
		t.Errorf("multi-line entry = %q", e.Command)
	}
	if e := entries[3]; e.Command != ":not extended;weird" || !e.StartedAt.IsZero() {
		t.Errorf("odd plain entry = %+v", e)
	}
}

func TestParseZshUnmetafies(t *testing.T) {
	// zsh metafies only the bytes 0x00 and 0x83-0xA2. "日" is E6 97 A5, so its
	// 0x97 is written as 0x83 0xB7; "é" (C3 A9) needs no escaping.
	in := ": 1700000000:0;echo \xe6\x83\xb7\xa5 café\n"
	entries, err := ParseZsh(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Command != "echo 日 café" {
		t.Errorf("got %q, want %q", entries[0].Command, "echo 日 café")
	}
}

func TestParseZshTrailingContinuationAtEOF(t *testing.T) {
	entries, _ := ParseZsh(strings.NewReader("echo one\\"))
	if len(entries) != 1 || entries[0].Command != "echo one\n" {
		t.Errorf("got %+v", entries)
	}
}
