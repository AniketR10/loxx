// Package tui is the inline search panel that `loxx` opens: a query line, the
// best matching commands, and a footer. Keyword results appear on every
// keystroke; results ranked by meaning replace them shortly after typing
// pauses.
package tui

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AniketR10/loxx/internal/format"
	"github.com/AniketR10/loxx/internal/search"
	"github.com/AniketR10/loxx/internal/store"
)

// Options describe where the panel was opened from.
type Options struct {
	Dir     string // the shell's current directory
	GitRoot string // the git repository containing Dir, if any
	Session string // the shell session id, for the "this session" filter
	Home    string // shortens paths to ~
	Height  int    // total lines the panel may use; 0 means defaultHeight
}

const (
	defaultHeight = 15
	// debounce is how long typing must pause before the slower, meaning-based
	// search runs; keyword results are shown on every keystroke meanwhile.
	debounce = 150 * time.Millisecond
)

// filters, in the order Tab cycles through them (user decision, ROADMAP §3).
var filters = []string{"all", "this dir", "this session", "failed"}

// Run shows the panel on tty until the user picks a command or cancels, and
// returns the chosen command ("" when cancelled).
func Run(ctx context.Context, s *search.Searcher, tty *os.File, opt Options) (string, error) {
	if opt.Height <= 0 {
		opt.Height = defaultHeight
	}
	m := &model{ctx: ctx, s: s, opt: opt}
	final, err := tea.NewProgram(m, tea.WithInput(tty), tea.WithOutput(tty)).Run()
	if err != nil {
		return "", err
	}
	return final.(*model).chosen, nil
}

type model struct {
	ctx context.Context
	s   *search.Searcher
	opt Options

	query    []rune
	filter   int
	results  []store.Result
	selected int
	// seq identifies the current query and filter; results computed for an
	// older one arrive late and are dropped.
	seq      int
	semantic bool // results include meaning-based ranking
	pending  bool // a meaning-based search is scheduled or running
	err      error
	width    int
	height   int
	chosen   string
	done     bool
}

type resultsMsg struct {
	seq      int
	results  []store.Result
	semantic bool
	err      error
}

type debounceMsg struct{ seq int }

func (m *model) Init() tea.Cmd { return m.refresh() }

// refresh starts searching for the current query and filter.
func (m *model) refresh() tea.Cmd {
	m.seq++
	m.selected = 0
	seq, q, f, limit := m.seq, strings.TrimSpace(string(m.query)), m.storeFilter(), m.rows()
	if q == "" {
		m.pending = false
		return func() tea.Msg {
			rs, err := m.s.Recent(m.ctx, limit, f)
			return resultsMsg{seq: seq, results: rs, err: err}
		}
	}
	m.pending = true
	return tea.Batch(
		func() tea.Msg {
			rs, err := m.s.Keyword(m.ctx, q, limit, f)
			return resultsMsg{seq: seq, results: rs, err: err}
		},
		tea.Tick(debounce, func(time.Time) tea.Msg { return debounceMsg{seq} }),
	)
}

// cycleFilter moves to the next (step 1) or previous (step -1) filter,
// skipping "this session" when the session is unknown: with no session id it
// would silently match everything.
func (m *model) cycleFilter(step int) {
	for {
		m.filter = (m.filter + step + len(filters)) % len(filters)
		if filters[m.filter] != "this session" || m.opt.Session != "" {
			break
		}
	}
	m.semantic = false
}

func (m *model) storeFilter() store.Filter {
	switch filters[m.filter] {
	case "this dir":
		return store.Filter{Dir: m.opt.Dir, GitRoot: m.opt.GitRoot}
	case "this session":
		return store.Filter{Session: m.opt.Session}
	case "failed":
		return store.Filter{Failed: true}
	}
	return store.Filter{}
}

// rows is how many results fit: the panel minus the query and footer lines.
func (m *model) rows() int {
	h := m.opt.Height
	if m.height > 0 {
		h = min(h, m.height-1) // leave the prompt line visible
	}
	return max(1, h-2)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case resultsMsg:
		if msg.seq != m.seq || (m.semantic && !msg.semantic && msg.err == nil) {
			return m, nil // stale, or keyword results arriving after semantic ones
		}
		m.results, m.err, m.semantic = msg.results, msg.err, msg.semantic
		if msg.semantic {
			m.pending = false
		}
		m.selected = min(m.selected, max(0, len(m.results)-1))
		return m, nil

	case debounceMsg:
		if msg.seq != m.seq {
			return m, nil
		}
		seq, q, f, limit := m.seq, strings.TrimSpace(string(m.query)), m.storeFilter(), m.rows()
		return m, func() tea.Msg {
			rs, err := m.s.Search(m.ctx, q, limit, search.DefaultParams, f)
			return resultsMsg{seq: seq, results: rs, semantic: true, err: err}
		}

	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m *model) key(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc", "ctrl+c", "ctrl+g":
		m.done = true
		return m, tea.Quit
	case "enter":
		if m.selected < len(m.results) {
			m.chosen = m.results[m.selected].Text
		}
		m.done = true
		return m, tea.Quit
	case "up", "ctrl+p":
		m.selected = max(0, m.selected-1)
	case "down", "ctrl+n":
		m.selected = min(max(0, len(m.results)-1), m.selected+1)
	case "tab":
		m.cycleFilter(1)
		return m, m.refresh()
	case "shift+tab":
		m.cycleFilter(-1)
		return m, m.refresh()
	case "backspace":
		if len(m.query) > 0 {
			m.query = m.query[:len(m.query)-1]
			m.semantic = false
			return m, m.refresh()
		}
	case "ctrl+u":
		m.query = nil
		m.semantic = false
		return m, m.refresh()
	case "ctrl+w":
		q := strings.TrimRight(string(m.query), " ")
		m.query = []rune(q[:strings.LastIndex(q, " ")+1])
		m.semantic = false
		return m, m.refresh()
	default:
		if k.Text != "" && k.Mod&^tea.ModShift == 0 {
			m.query = append(m.query, []rune(k.Text)...)
			m.semantic = false
			return m, m.refresh()
		}
	}
	return m, nil
}

// Plain ANSI styles; the panel needs no styling library.
const (
	bold    = "\x1b[1m"
	dim     = "\x1b[2m"
	reverse = "\x1b[7m"
	green   = "\x1b[32m"
	red     = "\x1b[31m"
	reset   = "\x1b[0m"
)

func (m *model) View() tea.View {
	if m.done {
		return tea.NewView("") // leave nothing behind on the screen
	}
	width := m.width
	if width <= 0 {
		width = 80
	}
	var b strings.Builder
	b.WriteString(bold + "loxx ❯ " + reset + string(m.query) + reverse + " " + reset + "\n")

	rows := m.rows()
	now := time.Now()
	switch {
	case m.err != nil:
		b.WriteString(red + "  " + ansi.Truncate(m.err.Error(), width-2, "…") + reset + "\n")
		rows--
	case len(m.results) == 0:
		b.WriteString(dim + "  no matches" + reset + "\n")
		rows--
	}
	for i, r := range m.results[:min(len(m.results), rows)] {
		b.WriteString(m.row(r, i == m.selected, width, now) + "\n")
	}

	footer := "filter: " + filters[m.filter] + " (Tab) · ↑↓ · Enter use · Esc cancel"
	if m.pending {
		footer += " · matching by meaning…"
	}
	b.WriteString(dim + ansi.Truncate(footer, width, "…") + reset)
	return tea.NewView(b.String())
}

// row renders one result on a single line: marker, command, then dimmed
// details, cut to the terminal width.
func (m *model) row(r store.Result, selected bool, width int, now time.Time) string {
	text := strings.ReplaceAll(r.Text, "\n", " ↵ ")
	var details []string
	if r.Cwd != "" {
		details = append(details, format.TildePath(r.Cwd, m.opt.Home))
	}
	if r.LastRun != nil {
		details = append(details, format.RelativeTime(*r.LastRun, now))
	} else {
		details = append(details, "imported")
	}
	if r.RunCount > 1 {
		details = append(details, strconv.Itoa(r.RunCount)+"×")
	}
	status := ""
	if r.ExitCode != nil {
		if *r.ExitCode == 0 {
			status = " " + green + "✓" + reset
		} else {
			status = " " + red + "✗" + reset
		}
	}
	marker, style := "  ", ""
	if selected {
		marker, style = "▸ ", bold
	}
	line := marker + style + text + reset + "  " + dim + strings.Join(details, " · ") + reset + status
	return ansi.Truncate(line, width, "…")
}
