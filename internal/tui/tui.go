// Package tui implements the interactive screen: a jq expression input, a status
// line, and a scrollable pane showing the result.
//
// Styles come from lipgloss's default renderer, and that includes the styles
// inside the bubbles components. Because the screen is drawn on stderr, the caller
// must point the default renderer at stderr before calling New. Otherwise colors
// and the cursor disappear when stdout is redirected.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/ibuki-aizawa/jitto/internal/eval"
)

const (
	defaultDebounce  = 100 * time.Millisecond
	defaultSlowAfter = 200 * time.Millisecond
	previewTimeout   = 5 * time.Second
	previewMaxBytes  = 1 << 20
	completeTimeout  = time.Second

	// maxCandidateRows is the number of candidates the list shows at once.
	maxCandidateRows = 5
	// ellipsis ends a candidate that does not fit. It is ASCII because "…" is
	// two columns wide in some East Asian terminals.
	ellipsis = "..."

	prompt = "> "
	// chromeHeight is the number of rows above the pane: the input and the status.
	chromeHeight = 2
	// panStep is the number of columns Shift+Left and Shift+Right move the pane.
	panStep = 8

	helpText      = "Enter: confirm  Ctrl+C: cancel  ↑↓ PgUp PgDn: scroll  Shift+←→: pan"
	slowText      = "evaluating..."
	finishingText = "evaluating all output... edit the expression to stop, Ctrl+C to quit"
	truncatedText = "preview limited to 1 MiB; Enter outputs everything"
)

// state describes the current expression, as opposed to what is on screen: the
// pane keeps the last good result while a newer expression is pending or failing.
type state int

const (
	// stateEvaluating means the preview is waiting out the debounce or running.
	stateEvaluating state = iota
	// stateDone means the output is the complete result of the expression.
	stateDone
	// stateTruncated means the preview succeeded but hit previewMaxBytes.
	stateTruncated
	// stateTimedOut means the preview did not finish within previewTimeout.
	stateTimedOut
	// stateFailed means the expression is invalid or failed while running.
	stateFailed
	// stateFinishing means Enter was pressed and the complete result is being
	// computed, with no limits.
	stateFinishing
)

type (
	// debounceMsg ends the wait after an edit.
	debounceMsg struct{ seq int }
	// slowMsg fires shortly after a preview starts, to mention a slow evaluation.
	slowMsg struct{ seq int }
	// resultMsg carries the outcome of an evaluation.
	resultMsg struct {
		seq int
		res eval.Result
		err error
	}
	// completeDebounceMsg ends the wait after the text before the cursor changed.
	completeDebounceMsg struct{ seq int }
	// completionMsg carries the candidates for the text before the cursor.
	completionMsg struct {
		seq  int
		comp eval.Completion
	}
)

// evaluator is the signature of eval.Eval.
type evaluator func(ctx context.Context, expr string, docs []any, maxBytes int) (eval.Result, error)

// completer is the signature of eval.Complete.
type completer func(ctx context.Context, text string, docs []any) eval.Completion

// Model is the Bubble Tea model of the screen.
type Model struct {
	docs     []any
	run      evaluator
	complete completer

	debounce  time.Duration
	slowAfter time.Duration

	input textinput.Model
	pane  viewport.Model

	errStyle      lipgloss.Style
	noteStyle     lipgloss.Style
	helpStyle     lipgloss.Style
	menuStyle     lipgloss.Style
	selectedStyle lipgloss.Style

	width int

	// seq identifies the latest evaluation request. Messages from any other
	// request are stale and are dropped.
	seq int
	// cancel stops the running evaluation, if any.
	cancel context.CancelFunc

	state  state
	slow   bool
	errMsg string
	// output is the last good result, the one on screen.
	output string

	// comp holds the latest candidates. They may be for an earlier text before the
	// cursor; Narrow decides which of them still apply.
	comp eval.Completion
	// compSeq identifies the latest completion request, like seq.
	compSeq int
	// compCancel stops the running completion, if any.
	compCancel context.CancelFunc
	// compClosed hides the candidates until the text before the cursor changes.
	compClosed bool
	// compSel is the index of the selected candidate, and compTop the index of the
	// first one on screen.
	compSel, compTop int

	confirmed bool
}

// New returns a model that evaluates expressions against docs.
func New(docs []any) *Model {
	input := textinput.New()
	input.Prompt = prompt
	input.Placeholder = "."
	input.Cursor.SetMode(cursor.CursorStatic)
	input.Cursor.Style = cursorStyle()
	input.Focus()

	pane := viewport.New(0, 0)
	pane.SetHorizontalStep(panStep)
	pane.KeyMap = viewport.KeyMap{
		Up:       key.NewBinding(key.WithKeys("up")),
		Down:     key.NewBinding(key.WithKeys("down")),
		PageUp:   key.NewBinding(key.WithKeys("pgup")),
		PageDown: key.NewBinding(key.WithKeys("pgdown")),
		Left:     key.NewBinding(key.WithKeys("shift+left")),
		Right:    key.NewBinding(key.WithKeys("shift+right")),
	}

	return &Model{
		docs:          docs,
		run:           eval.Eval,
		complete:      eval.Complete,
		debounce:      defaultDebounce,
		slowAfter:     defaultSlowAfter,
		input:         input,
		pane:          pane,
		errStyle:      lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
		noteStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
		helpStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		menuStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("238")),
		selectedStyle: cursorStyle().Reverse(true),
	}
}

// cursorStyle returns a style that can always render the cursor, which is drawn in
// reverse video. With NO_COLOR set, the default renderer drops every attribute, not
// only colors, and the cursor would disappear.
func cursorStyle() lipgloss.Style {
	// This renderer only decides which escape sequences to produce. It writes nothing.
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.ANSI)
	return lipgloss.NewStyle().Renderer(r)
}

// Outcome returns the complete output and the expression if the user confirmed
// a result. ok is false if the user canceled.
func (m *Model) Outcome() (output, expr string, ok bool) {
	if !m.confirmed {
		return "", "", false
	}
	expr = m.input.Value()
	if strings.TrimSpace(expr) == "" {
		expr = "."
	}
	return m.output, expr, true
}

// Init evaluates the empty expression, so the input is shown right away.
func (m *Model) Init() tea.Cmd {
	return m.startPreview()
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	case debounceMsg:
		if msg.seq != m.seq {
			return m, nil
		}
		return m, m.startPreview()
	case slowMsg:
		if msg.seq == m.seq && m.state == stateEvaluating {
			m.slow = true
		}
		return m, nil
	case resultMsg:
		return m, m.handleResult(msg)
	case completeDebounceMsg:
		if msg.seq != m.compSeq {
			return m, nil
		}
		return m, m.startCompletion()
	case completionMsg:
		if msg.seq == m.compSeq {
			m.stopCompletion()
			m.comp = msg.comp
		}
		return m, nil
	}
	return m, m.updateInput(msg)
}

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "enter":
		return m.handleEnter()
	case "up", "down", "pgup", "pgdown", "shift+left", "shift+right":
		var cmd tea.Cmd
		m.pane, cmd = m.pane.Update(msg)
		return cmd
	// textinput binds these keys to its own suggestions, which are not used.
	case "tab", "ctrl+y":
		return m.acceptCandidate()
	case "ctrl+n":
		m.moveSelection(1)
		return nil
	case "ctrl+p":
		m.moveSelection(-1)
		return nil
	case "esc":
		m.compClosed = true
		return nil
	}
	return m.updateInput(msg)
}

// quit stops all work in progress and ends the program.
func (m *Model) quit() tea.Cmd {
	m.stopEvaluation()
	m.stopCompletion()
	return tea.Quit
}

// handleEnter confirms the current expression if its complete result is known.
// If it might still succeed, it computes the complete result first. If it has
// failed, Enter does nothing, so the user can fix the expression.
func (m *Model) handleEnter() tea.Cmd {
	switch m.state {
	case stateDone:
		m.confirmed = true
		return m.quit()
	case stateEvaluating, stateTruncated, stateTimedOut:
		return m.startFinishing()
	}
	return nil
}

// updateInput forwards msg to the input and reacts to the edit.
func (m *Model) updateInput(msg tea.Msg) tea.Cmd {
	value, head := m.input.Value(), m.head()
	var inputCmd tea.Cmd
	m.input, inputCmd = m.input.Update(msg)
	return tea.Batch(inputCmd, m.afterEdit(value, head))
}

// afterEdit schedules a preview if the expression changed from value, and new
// candidates if the text before the cursor changed from head. Editing also
// abandons any evaluation in progress, including the one started by Enter.
func (m *Model) afterEdit(value, head string) tea.Cmd {
	var cmds []tea.Cmd
	if m.input.Value() != value {
		seq := m.nextRequest(stateEvaluating)
		cmds = append(cmds, tea.Tick(m.debounce, func(time.Time) tea.Msg { return debounceMsg{seq: seq} }))
	}
	if m.head() != head {
		cmds = append(cmds, m.requestCompletion())
	}
	return tea.Batch(cmds...)
}

// head returns the expression up to the cursor.
func (m *Model) head() string {
	value := []rune(m.input.Value())
	return string(value[:min(m.input.Position(), len(value))])
}

// requestCompletion schedules new candidates for the text before the cursor. The
// current ones stay, narrowed by Narrow, until the new ones arrive.
func (m *Model) requestCompletion() tea.Cmd {
	m.stopCompletion()
	m.compSeq++
	m.compClosed = false
	m.compSel, m.compTop = 0, 0
	seq := m.compSeq
	return tea.Tick(m.debounce, func(time.Time) tea.Msg { return completeDebounceMsg{seq: seq} })
}

func (m *Model) startCompletion() tea.Cmd {
	ctx, cancel := context.WithTimeout(context.Background(), completeTimeout)
	m.compCancel = cancel
	complete, text, docs, seq := m.complete, m.head(), m.docs, m.compSeq
	return func() tea.Msg {
		return completionMsg{seq: seq, comp: complete(ctx, text, docs)}
	}
}

func (m *Model) stopCompletion() {
	if m.compCancel != nil {
		m.compCancel()
		m.compCancel = nil
	}
}

// candidates returns the candidates on screen.
func (m *Model) candidates() []eval.Candidate {
	if m.compClosed {
		return nil
	}
	return m.comp.Narrow(m.head())
}

// selected returns the index of the selected candidate among n. The list can
// shrink after the selection was made.
func (m *Model) selected(n int) int {
	return min(m.compSel, n-1)
}

// moveSelection selects the candidate delta rows away, wrapping around.
func (m *Model) moveSelection(delta int) {
	n := len(m.candidates())
	if n == 0 {
		return
	}
	m.compSel = (m.selected(n) + delta + n) % n
	if m.compSel < m.compTop {
		m.compTop = m.compSel
	}
	if m.compSel >= m.compTop+maxCandidateRows {
		m.compTop = m.compSel - maxCandidateRows + 1
	}
}

// acceptCandidate replaces the partial key with the selected candidate and moves
// the cursor after it. The text after the cursor stays.
func (m *Model) acceptCandidate() tea.Cmd {
	cands := m.candidates()
	if len(cands) == 0 {
		return nil
	}
	value, head := m.input.Value(), m.head()
	newHead := head[:m.comp.Start] + cands[m.selected(len(cands))].Text
	m.input.SetValue(newHead + value[len(head):])
	m.input.SetCursor(utf8.RuneCountInString(newHead))
	return m.afterEdit(value, head)
}

func (m *Model) startPreview() tea.Cmd {
	seq := m.nextRequest(stateEvaluating)
	ctx, cancel := context.WithTimeout(context.Background(), previewTimeout)
	m.cancel = cancel
	slow := tea.Tick(m.slowAfter, func(time.Time) tea.Msg { return slowMsg{seq: seq} })
	return tea.Batch(m.evaluate(ctx, seq, previewMaxBytes), slow)
}

func (m *Model) startFinishing() tea.Cmd {
	seq := m.nextRequest(stateFinishing)
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	return m.evaluate(ctx, seq, 0)
}

// nextRequest stops the running evaluation and makes all earlier messages stale.
func (m *Model) nextRequest(s state) int {
	m.stopEvaluation()
	m.seq++
	m.state = s
	m.slow = false
	return m.seq
}

func (m *Model) stopEvaluation() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

func (m *Model) evaluate(ctx context.Context, seq, maxBytes int) tea.Cmd {
	run, expr, docs := m.run, m.input.Value(), m.docs
	return func() tea.Msg {
		res, err := run(ctx, expr, docs, maxBytes)
		return resultMsg{seq: seq, res: res, err: err}
	}
}

func (m *Model) handleResult(msg resultMsg) tea.Cmd {
	if msg.seq != m.seq {
		return nil
	}
	finishing := m.state == stateFinishing
	m.stopEvaluation()
	m.slow = false

	switch {
	case msg.err == nil && finishing:
		m.output = msg.res.Output
		m.confirmed = true
		return m.quit()
	case msg.err == nil:
		m.showOutput(msg.res.Output)
		m.errMsg = ""
		m.state = stateDone
		if msg.res.Truncated {
			m.state = stateTruncated
		}
	case !finishing && errors.Is(msg.err, context.DeadlineExceeded):
		m.errMsg = fmt.Sprintf("evaluation timed out after %v; Enter keeps evaluating without a limit", previewTimeout)
		m.state = stateTimedOut
	default:
		m.errMsg = msg.err.Error()
		m.state = stateFailed
	}
	return nil
}

func (m *Model) showOutput(output string) {
	if output == m.output {
		return
	}
	m.output = output
	m.pane.SetContent(strings.TrimRight(output, "\n"))
	m.pane.GotoTop()
	m.pane.SetXOffset(0)
}

func (m *Model) resize(width, height int) {
	m.width = width
	// One extra cell keeps the cursor visible at the end of the text.
	m.input.Width = max(width-lipgloss.Width(prompt)-1, 1)
	m.pane.Width = width
	m.pane.Height = max(height-chromeHeight, 0)
	// Reapply the offset limits for the new height.
	m.pane.SetYOffset(m.pane.YOffset)
	// A line that now fits could otherwise stay panned, with no key to undo it.
	m.pane.SetXOffset(0)
}

// View implements tea.Model.
func (m *Model) View() string {
	return m.input.View() + "\n" + m.statusLine() + "\n" + m.paneView()
}

// paneView draws the candidate list over the top rows of the pane, lined up with
// the start of the expression.
func (m *Model) paneView() string {
	view := m.pane.View()
	box, boxWidth := m.candidateBox()
	if len(box) == 0 {
		return view
	}
	col := lipgloss.Width(prompt)
	lines := strings.Split(view, "\n")
	for i, row := range box {
		if i >= len(lines) {
			break
		}
		left := columns(lines[i], 0, col)
		left += strings.Repeat(" ", col-lipgloss.Width(left))
		lines[i] = left + row + columns(lines[i], col+boxWidth, m.width)
	}
	return strings.Join(lines, "\n")
}

// candidateBox renders the rows of the candidate list and returns their width.
// If there are more candidates than rows, a last row shows the position in the
// list.
func (m *Model) candidateBox() ([]string, int) {
	cands := m.candidates()
	// One column of padding on each side of the text.
	textWidth := m.width - lipgloss.Width(prompt) - 2
	rows := min(len(cands), maxCandidateRows, m.pane.Height)
	if rows == 0 || textWidth < len(ellipsis)+1 {
		return nil, 0
	}
	sel := m.selected(len(cands))
	top := min(max(m.compTop, sel-rows+1), sel, len(cands)-rows)
	shown := cands[top : top+rows]

	counter := ""
	if len(cands) > rows && rows < m.pane.Height {
		counter = fmt.Sprintf("%d/%d", sel+1, len(cands))
	}
	width := lipgloss.Width(counter)
	for _, c := range shown {
		width = max(width, lipgloss.Width(c.Text))
	}
	width = min(width, textWidth)

	var box []string
	for i, c := range shown {
		style := m.menuStyle
		if top+i == sel {
			style = m.selectedStyle
		}
		box = append(box, style.Render(" "+fit(c.Text, width)+" "))
	}
	if counter != "" {
		box = append(box, m.menuStyle.Render(" "+fit(counter, width)+" "))
	}
	return box, width + 2
}

// fit pads or cuts s to exactly width columns.
func fit(s string, width int) string {
	if w := lipgloss.Width(s); w > width {
		s = columns(s, 0, width-len(ellipsis)) + ellipsis
	}
	return s + strings.Repeat(" ", width-lipgloss.Width(s))
}

// columns returns the part of s between the columns from and to. A wide character
// cut by either edge becomes spaces, so the result is to-from columns wide unless
// s ends first. s must not contain escape sequences, which holds for the pane: it
// shows formatted JSON, in which control characters are escaped.
func columns(s string, from, to int) string {
	var b strings.Builder
	col := 0
	for _, r := range s {
		if col >= to {
			break
		}
		w := lipgloss.Width(string(r))
		switch {
		case col >= from && col+w <= to:
			b.WriteRune(r)
		case col+w > from:
			b.WriteString(strings.Repeat(" ", min(col+w, to)-max(col, from)))
		}
		col += w
	}
	return b.String()
}

// statusLine always takes exactly one row, so the layout does not jump.
func (m *Model) statusLine() string {
	style, text := m.helpStyle, helpText
	switch {
	case m.state == stateFinishing:
		style, text = m.noteStyle, finishingText
	case m.slow:
		style, text = m.noteStyle, slowText
	case m.errMsg != "":
		// A failing expression keeps its message while the next edit is evaluated.
		style, text = m.errStyle, sanitize(m.errMsg)
	case m.state == stateTruncated:
		style, text = m.noteStyle, truncatedText
	}
	return style.Inline(true).MaxWidth(m.width).Render(text)
}

// sanitize replaces control characters. Error messages can quote values from the
// input, and escape sequences in them must not reach the terminal.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
