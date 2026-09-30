package format

import (
	"testing"
	"time"
)

func TestRelativeTime(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	for d, want := range map[time.Duration]string{
		10 * time.Second:     "just now",
		time.Minute:          "1 minute ago",
		90 * time.Minute:     "1 hour ago",
		3 * 24 * time.Hour:   "3 days ago",
		15 * 24 * time.Hour:  "2 weeks ago",
		70 * 24 * time.Hour:  "2 months ago",
		800 * 24 * time.Hour: "2 years ago",
		-5 * time.Minute:     "just now", // clock skew: never "-5 minutes ago"
	} {
		if got := RelativeTime(now.Add(-d), now); got != want {
			t.Errorf("RelativeTime(-%v) = %q, want %q", d, got, want)
		}
	}
}

func TestTildePath(t *testing.T) {
	for in, want := range map[string]string{
		"/home/a":       "~",
		"/home/a/src/x": "~/src/x",
		"/home/ab/src":  "/home/ab/src", // not inside /home/a
		"/srv/api":      "/srv/api",
	} {
		if got := TildePath(in, "/home/a"); got != want {
			t.Errorf("TildePath(%q) = %q, want %q", in, got, want)
		}
	}
}
