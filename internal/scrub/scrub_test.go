package scrub

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/AniketR10/loc/internal/secretcorpus"
)

func TestPositiveCorpus(t *testing.T) {
	positives, err := secretcorpus.Positives()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range positives {
		got := Scrub(p.Cmd)
		for _, secret := range p.Secrets {
			if strings.Contains(got.Text(), secret) {
				t.Errorf("secret %q survived scrubbing\n  in:  %q\n  out: %q", secret, p.Cmd, got.Text())
			}
		}
		if got.Redactions() == 0 {
			t.Errorf("no redactions reported for %q", p.Cmd)
		}
	}
}

func TestNegativeCorpus(t *testing.T) {
	negatives, err := secretcorpus.Negatives()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range negatives {
		got := Scrub(n.Cmd)
		if got.Text() != n.Cmd || got.Redactions() != 0 {
			t.Errorf("non-secret command was changed (%d redactions)\n  in:  %q\n  out: %q", got.Redactions(), n.Cmd, got.Text())
		}
	}
}

func TestIdempotent(t *testing.T) {
	positives, err := secretcorpus.Positives()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range positives {
		once := Scrub(p.Cmd)
		twice := Scrub(once.Text())
		if twice.Text() != once.Text() || twice.Redactions() != 0 {
			t.Errorf("scrubbing is not idempotent (%d new redactions)\n  once:  %q\n  twice: %q", twice.Redactions(), once.Text(), twice.Text())
		}
	}
}

func TestReplacementFormat(t *testing.T) {
	got := Scrub("export DB_PASSWORD=hunter2 && ./migrate")
	want := "export DB_PASSWORD=<REDACTED:secret-env> && ./migrate"
	if got.Text() != want {
		t.Errorf("got %q, want %q", got.Text(), want)
	}
	if got.Redactions() != 1 {
		t.Errorf("redactions = %d, want 1", got.Redactions())
	}
}

func TestIgnored(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
	}{
		{" export TOKEN=abc", true},
		{"  ls", true},
		{"ls", false},
		{"\tls", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := Ignored(tt.raw); got != tt.want {
			t.Errorf("Ignored(%q) = %v, want %v", tt.raw, got, tt.want)
		}
	}
}

func TestTrimsWhitespace(t *testing.T) {
	if got := Scrub("  git status \n").Text(); got != "git status" {
		t.Errorf("got %q, want %q", got, "git status")
	}
}

func FuzzScrub(f *testing.F) {
	positives, err := secretcorpus.Positives()
	if err != nil {
		f.Fatal(err)
	}
	negatives, err := secretcorpus.Negatives()
	if err != nil {
		f.Fatal(err)
	}
	for _, p := range positives {
		f.Add(p.Cmd)
	}
	for _, n := range negatives {
		f.Add(n.Cmd)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		once := Scrub(raw)
		if utf8.ValidString(raw) && !utf8.ValidString(once.Text()) {
			t.Fatalf("scrubbing produced invalid UTF-8 from %q", raw)
		}
		twice := Scrub(once.Text())
		if twice.Text() != once.Text() {
			t.Fatalf("not idempotent\n  in:    %q\n  once:  %q\n  twice: %q", raw, once.Text(), twice.Text())
		}
	})
}

func BenchmarkScrub(b *testing.B) {
	cmds := []string{
		"git status",
		"docker run -e POSTGRES_PASSWORD=mysecretpassword -v pgdata:/var/lib/postgresql/data -d postgres:16",
		"curl -H \"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U\" https://api.example.com/v1/items?page=2",
	}
	for _, c := range cmds {
		b.Run(c[:min(len(c), 20)], func(b *testing.B) {
			for b.Loop() {
				Scrub(c)
			}
		})
	}
}
