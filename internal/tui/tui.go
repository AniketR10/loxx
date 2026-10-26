// Package tui is the inline search panel opened by `loxx`.
package tui

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AniketR10/loxx/internal/color"
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
	Query   string // already typed when the panel opens (bash: a line that wasn't a command)
}

const (
	defaultHeight = 15
	// debounce is how long typing must pause before the slower, meaning-based
	// search runs; keyword results are shown on every keystroke meanwhile.
	debounce = 150 * time.Millisecond
)

// filters, in the order Tab cycles through them.
var filters = []string{"all", "this dir", "this session", "failed"}

// Run shows the panel on tty until the user picks a command or cancels, and
// returns the chosen result (with no Text when cancelled).
func Run(ctx context.Context, s *search.Searcher, tty *os.File, opt Options) (store.Result, error) {
	if opt.Height <= 0 {
		opt.Height = defaultHeight
	}
	// The panel always draws on a terminal, so only NO_COLOR turns colors off.
	m := &model{ctx: ctx, s: s, opt: opt, query: []rune(opt.Query), c: color.Palette{On: color.Wanted()}}
	final, err := tea.NewProgram(m, tea.WithInput(tty), tea.WithOutput(tty)).Run()
	if err != nil {
		return store.Result{}, err
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
	chosen   store.Result
	done     bool
	c        color.Palette
	spin     int  // spinner frame, while a meaning-based search runs
	spinning bool // a spinner tick is scheduled
}

type resultsMsg struct {
	seq      int
	results  []store.Result
	semantic bool
	err      error
}

type debounceMsg struct{ seq int }

type spinMsg struct{}

// spinner frames, shown while matching by meaning.
var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m *model) tick() tea.Cmd {
	if m.spinning {
		return nil
	}
	m.spinning = true
	return tea.Tick(90*time.Millisecond, func(time.Time) tea.Msg { return spinMsg{} })
}

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
		m.tick(),
	)
}

// cycleFilter moves through the filters, skipping "this session" if unknown.
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

	case spinMsg:
		m.spinning = false
		if !m.pending {
			return m, nil
		}
		m.spin++
		return m, m.tick()

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
			m.chosen = m.results[m.selected]
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

func (m *model) View() tea.View {
	if m.done {
		return tea.NewView("") // leave nothing behind on the screen
	}
	width := m.width
	if width <= 0 {
		width = 80
	}
	c := m.c
	caret := " "
	if c.On {
		caret = "\x1b[7m \x1b[27m" // reverse video: a block cursor
	}
	var b strings.Builder
	b.WriteString(c.Brand("loxx ❯") + " " + string(m.query) + caret + "\n")

	rows := m.rows()
	now := time.Now()
	switch {
	case m.err != nil:
		b.WriteString(c.Red("  "+ansi.Truncate(m.err.Error(), width-2, "…")) + "\n")
		rows--
	case len(m.results) == 0:
		b.WriteString(c.Dim("  no matches") + "\n")
		rows--
	}
	for i, r := range m.results[:min(len(m.results), rows)] {
		b.WriteString(m.row(r, i == m.selected, width, now) + "\n")
	}

	footer := c.Dim("filter: ") + c.Teal(filters[m.filter]) + c.Dim(" (Tab) · ↑↓ · Enter use · Esc cancel")
	if m.pending {
		footer += c.Dim(" · ") + c.Teal(spinner[m.spin%len(spinner)]) + c.Dim(" matching by meaning")
	}
	b.WriteString(ansi.Truncate(footer, width, "…"))
	return tea.NewView(b.String())
}

// row renders one result on a single line: marker, command, then its details,
// cut to the terminal width. The selected row is a full width band.
func (m *model) row(r store.Result, selected bool, width int, now time.Time) string {
	c := m.c
	if selected {
		c = c.InRow()
	}
	text := strings.ReplaceAll(r.Text, "\n", " ↵ ")
	marker := "  "
	if selected {
		marker, text = c.Teal("▸ "), c.Bold(text)
	}
	var line string
	if r.Library {
		line = marker + text + "  " + c.Pink("suggested") + c.Dim(" · "+r.Desc)
	} else {
		var details []string
		if r.Cwd != "" {
			details = append(details, c.Blue(format.TildePath(r.Cwd, m.opt.Home)))
		}
		if r.LastRun != nil {
			details = append(details, c.Dim(format.RelativeTime(*r.LastRun, now)))
		} else {
			details = append(details, c.Dim("imported"))
		}
		if r.RunCount > 1 {
			details = append(details, c.Dim(strconv.Itoa(r.RunCount)+"×"))
		}
		status := ""
		if r.ExitCode != nil {
			if *r.ExitCode == 0 {
				status = " " + c.Green("✓")
			} else {
				status = " " + c.Red("✗")
			}
		}
		line = marker + text + "  " + strings.Join(details, c.Dim(" · ")) + status
	}
	line = ansi.Truncate(line, width, "…")
	if !selected || !c.On {
		return line // without colors, the ▸ marker shows the selection
	}
	if pad := width - ansi.StringWidth(line); pad > 0 {
		line += strings.Repeat(" ", pad) // the band reaches the right edge
	}
	return c.Row(line)
}
