// Package format renders search results for people: the metadata line under
// a command, relative times and ~-shortened paths.
package format

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/AniketR10/loc/internal/store"
)

// Details formats a result's metadata line, e.g.
// "~/work/api · 3 weeks ago · exit 0 · 4 runs".
func Details(r store.Result, home string, now time.Time) string {
	var parts []string
	if r.Cwd != "" {
		parts = append(parts, TildePath(r.Cwd, home))
	}
	if r.LastRun != nil {
		parts = append(parts, RelativeTime(*r.LastRun, now))
	} else {
		parts = append(parts, "imported") // no run time is known (user decision, ROADMAP §3)
	}
	if r.ExitCode != nil {
		parts = append(parts, fmt.Sprintf("exit %d", *r.ExitCode))
	}
	if r.RunCount == 1 {
		parts = append(parts, "1 run")
	} else {
		parts = append(parts, fmt.Sprintf("%d runs", r.RunCount))
	}
	return strings.Join(parts, " · ")
}

// RelativeTime describes t relative to now in the largest whole unit, e.g.
// "just now", "5 minutes ago", "3 weeks ago".
func RelativeTime(t, now time.Time) string {
	d := now.Sub(t)
	if d < time.Minute {
		return "just now"
	}
	for _, u := range []struct {
		size time.Duration
		name string
	}{
		{365 * 24 * time.Hour, "year"},
		{30 * 24 * time.Hour, "month"},
		{7 * 24 * time.Hour, "week"},
		{24 * time.Hour, "day"},
		{time.Hour, "hour"},
		{time.Minute, "minute"},
	} {
		if n := int(d / u.size); n >= 1 {
			if n == 1 {
				return "1 " + u.name + " ago"
			}
			return fmt.Sprintf("%d %ss ago", n, u.name)
		}
	}
	return "just now"
}

// TildePath shortens path by writing the home directory as ~.
func TildePath(path, home string) string {
	if home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rel, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~/" + rel
	}
	return path
}
