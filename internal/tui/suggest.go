package tui

import (
	"context"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ibuki-aizawa/jitto/internal/eval"
)

const (
	suggestTimeout = time.Second
	// maxExampleRows is the number of examples the list shows at once.
	maxExampleRows = 10

	examplesHelpText = "Ctrl+N Ctrl+P: try an example  Tab: keep it  Esc: back"
	findingText      = "finding examples..."
	noExamplesText   = "no examples for this result"
)

// suggester is the signature of eval.Suggest.
type suggester func(ctx context.Context, expr string, docs []any) []eval.Suggestion

type (
	// examplesMsg carries the examples for an expression.
	examplesMsg struct {
		seq   int
		items []eval.Suggestion
	}
)

// examples is the list of example expressions that Ctrl+T opens. Like the
// candidates, Ctrl+N and Ctrl+P write each one into the input, so the pane
// previews it. The stops are the examples and the expression as typed.
type examples struct {
	// value and cursor are the input as typed, before any example.
	value  string
	cursor int
	// items is nil until the examples arrive.
	items   []eval.Suggestion
	loading bool
	// sel is the index of the example in the input, or -1 for the typed text.
	sel int
	// top is the index of the first example on screen.
	top int
}

// toggleExamples opens the list of examples for the expression, or closes it as
// Esc does.
func (m *Model) toggleExamples() tea.Cmd {
	if m.examples != nil {
		return m.closeExamples()
	}
	// The candidate list and the example list share the space below the input.
	m.cycle = nil
	m.compClosed = true
	m.stopCompletion()
	m.compSeq++

	m.exSeq++
	m.examples = &examples{value: m.input.Value(), cursor: m.input.Position(), loading: true, sel: -1}
	ctx, cancel := context.WithTimeout(context.Background(), suggestTimeout)
	m.exCancel = cancel
	suggest, expr, docs, seq := m.suggest, m.input.Value(), m.docs, m.exSeq
	return func() tea.Msg {
		return examplesMsg{seq: seq, items: suggest(ctx, expr, docs)}
	}
}

func (m *Model) handleExamples(msg examplesMsg) {
	if msg.seq != m.exSeq || m.examples == nil {
		return
	}
	m.stopExamples()
	if len(msg.items) == 0 {
		m.examples = nil
		m.exNote = noExamplesText
		return
	}
	m.examples.items = msg.items
	m.examples.loading = false
}

// dropExamples closes the list and abandons the request for it.
func (m *Model) dropExamples() {
	m.stopExamples()
	m.exSeq++
	m.examples = nil
}

func (m *Model) stopExamples() {
	if m.exCancel != nil {
		m.exCancel()
		m.exCancel = nil
	}
}

// cycleExamples writes the example delta stops away into the input, with the
// cursor at its end.
func (m *Model) cycleExamples(delta int) tea.Cmd {
	ex := m.examples
	if ex.loading {
		return nil
	}
	stops := len(ex.items) + 1
	ex.sel = (ex.sel+1+delta+stops)%stops - 1
	if ex.sel >= 0 {
		ex.top = min(ex.top, ex.sel)
		ex.top = max(ex.top, ex.sel-maxExampleRows+1)
	}

	value, cursor := ex.value, ex.cursor
	if ex.sel >= 0 {
		value = ex.items[ex.sel].Text
		cursor = utf8.RuneCountInString(value)
	}
	// Only the preview follows: the list stays.
	before, _ := m.setInput(value, cursor)
	return m.schedulePreview(before)
}

// acceptExample closes the list with the example in the input. Back at the typed
// text, it writes the first example.
func (m *Model) acceptExample() tea.Cmd {
	ex := m.examples
	if ex.loading {
		return nil
	}
	m.dropExamples()
	if ex.sel >= 0 {
		return m.requestCompletion()
	}
	text := ex.items[0].Text
	return m.edit(text, utf8.RuneCountInString(text))
}

// closeExamples closes the list and puts back the expression as typed.
func (m *Model) closeExamples() tea.Cmd {
	ex := m.examples
	m.dropExamples()
	var cmd tea.Cmd
	if m.input.Value() != ex.value {
		cmd = m.edit(ex.value, ex.cursor)
	}
	// Like Esc on the candidates, keep them closed until the next edit.
	m.compClosed = true
	return cmd
}

// exampleBox renders the rows of the example list and returns their width: each
// example, and what it gives. If there are more examples than rows, a last row
// shows the position in the list.
func (m *Model) exampleBox() ([]string, int) {
	ex := m.examples
	if ex == nil || ex.loading {
		return nil, 0
	}
	textWidth := m.width - 2
	rows := min(len(ex.items), maxExampleRows, m.pane.Height)
	if rows == 0 || textWidth < len(ellipsis)+1 {
		return nil, 0
	}
	top := min(max(ex.top, ex.sel-rows+1, 0), len(ex.items)-rows)
	shown := ex.items[top : top+rows]

	counter := ""
	if len(ex.items) > rows && rows < m.pane.Height {
		pos := "-"
		if ex.sel >= 0 {
			pos = strconv.Itoa(ex.sel + 1)
		}
		counter = pos + "/" + strconv.Itoa(len(ex.items))
	}
	exampleWidth, width := 0, lipgloss.Width(counter)
	for _, s := range shown {
		exampleWidth = max(exampleWidth, lipgloss.Width(s.Example))
	}
	for _, s := range shown {
		width = max(width, exampleWidth+2+lipgloss.Width(s.Description))
	}
	width = min(width, textWidth)

	var box []string
	for i, s := range shown {
		style := m.menuStyle
		if top+i == ex.sel {
			style = m.selectedStyle
		}
		text := s.Example + strings.Repeat(" ", exampleWidth-lipgloss.Width(s.Example)) + "  " + s.Description
		box = append(box, style.Render(" "+fit(text, width)+" "))
	}
	if counter != "" {
		box = append(box, m.menuStyle.Render(" "+fit(counter, width)+" "))
	}
	return box, width + 2
}

// exampleColumn returns the column of the example list boxWidth wide: its text
// starts below where the examples go in the input, and the list moves left as
// far as it must to fit.
func (m *Model) exampleColumn(boxWidth int) int {
	first := m.examples.items[0]
	before := strings.TrimSuffix(first.Text, first.Example)
	col := lipgloss.Width(m.input.Prompt) + lipgloss.Width(before) - 1
	return max(min(col, m.width-boxWidth), 0)
}
