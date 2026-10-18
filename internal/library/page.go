package library

import (
	"regexp"
	"strings"
)

// Page is one parsed tldr page.
type Page struct {
	Name     string // "git rebase"
	Summary  string // first line of the description
	Requires string // add-on named by "Part of `X`", or ""
	Examples []Example
}

// Example is one command on a page, cleaned for the command line.
type Example struct {
	Desc    string
	Command string
	Cursor  int // byte offset of the first placeholder, or len(Command)
}

var (
	partOf   = regexp.MustCompile("Part of `([^`]+)`")
	mnemonic = regexp.MustCompile(`\[([A-Za-z0-9]+)\]`) // e[x]tract → extract
)

// ParsePage reads a tldr page in Markdown. Alias pages (`tldr other`) give no examples.
func ParsePage(md string) Page {
	var p Page
	desc := ""
	for _, line := range strings.Split(md, "\n") {
		switch {
		case strings.HasPrefix(line, "# ") && p.Name == "":
			p.Name = strings.TrimSpace(line[2:])
		case strings.HasPrefix(line, "> "):
			text := strings.TrimSpace(line[2:])
			if p.Summary == "" {
				p.Summary = mnemonic.ReplaceAllString(text, "$1")
			}
			if m := partOf.FindStringSubmatch(text); m != nil {
				p.Requires = strings.Fields(m[1])[0]
			}
		case strings.HasPrefix(line, "- "):
			desc = mnemonic.ReplaceAllString(strings.TrimSuffix(strings.TrimSpace(line[2:]), ":"), "$1")
		case strings.HasPrefix(line, "`") && strings.HasSuffix(line, "`") && len(line) > 1 && desc != "":
			cmd, cursor := cleanCommand(line[1 : len(line)-1])
			if !strings.HasPrefix(cmd, "tldr ") {
				p.Examples = append(p.Examples, Example{desc, cmd, cursor})
			}
			desc = ""
		}
	}
	return p
}

// Tool is the program a page's commands run.
func (p Page) Tool() string {
	if f := strings.Fields(p.Name); len(f) > 0 {
		return f[0]
	}
	return ""
}

// cleanCommand turns {{[-n|--namespace]}} into -n and {{path/to/file}} into
// path/to/file, and returns where the first placeholder starts.
func cleanCommand(s string) (string, int) {
	var b strings.Builder
	cursor := -1
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], `\{\{`):
			b.WriteString("{{")
			i += 4
		case strings.HasPrefix(s[i:], `\}\}`):
			b.WriteString("}}")
			i += 4
		case strings.HasPrefix(s[i:], "{{"):
			end := closing(s, i+2)
			if end < 0 {
				b.WriteString(s[i:])
				i = len(s)
				continue
			}
			inner := strings.NewReplacer(`\{`, "{", `\}`, "}").Replace(s[i+2 : end])
			if strings.HasPrefix(inner, "[") && strings.HasSuffix(inner, "]") && strings.Contains(inner, "|") {
				b.WriteString(strings.SplitN(inner[1:len(inner)-1], "|", 2)[0]) // an option, not a placeholder
			} else {
				if cursor < 0 {
					cursor = b.Len()
				}
				b.WriteString(inner)
			}
			i = end + 2
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	if cursor < 0 {
		cursor = b.Len()
	}
	return b.String(), cursor
}

// closing finds the "}}" ending a placeholder that starts at i, skipping escaped braces.
func closing(s string, i int) int {
	for j := i; j+1 < len(s); j++ {
		if s[j] == '\\' {
			j++
			continue
		}
		if s[j] == '}' && s[j+1] == '}' {
			for j+2 < len(s) && s[j+2] == '}' { // "}}}": the placeholder ends with "}"
				j++
			}
			return j
		}
	}
	return -1
}
