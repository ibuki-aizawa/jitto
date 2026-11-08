package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ibuki-aizawa/jitto/internal/eval"
)

// call records one evaluation.
type call struct {
	ctx      context.Context
	expr     string
	maxBytes int
}

// harness drives a Model without a terminal: it plays the part of the Bubble Tea
// runtime by running commands and feeding their messages back into Update.
type harness struct {
	t     *testing.T
	m     *Model
	calls []call
	quit  bool
}

func newHarness(t *testing.T, docs ...any) *harness {
	t.Helper()
	h := &harness{t: t, m: New(docs)}
	// Whether the result has colors depends on the terminal running the tests.
	// Tests of the colors turn them on.
	h.m.color = false
	h.m.debounce = time.Millisecond
	h.m.slowAfter = time.Millisecond
	h.useEvaluator(eval.Eval)
	h.m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return h
}

// useEvaluator installs fn, recording every call to it.
func (h *harness) useEvaluator(fn evaluator) {
	h.m.run = func(ctx context.Context, expr string, docs []any, opts eval.Options) (eval.Result, error) {
		h.calls = append(h.calls, call{ctx, expr, opts.MaxBytes})
		return fn(ctx, expr, docs, opts)
	}
}

// messages runs cmd and returns the messages it produces.
func messages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var msgs []tea.Msg
		for _, c := range batch {
			msgs = append(msgs, messages(c)...)
		}
		return msgs
	}
	return []tea.Msg{msg}
}

// settle runs cmd and everything that follows from it to completion.
func (h *harness) settle(cmd tea.Cmd) {
	h.t.Helper()
	for _, msg := range messages(cmd) {
		if _, ok := msg.(tea.QuitMsg); ok {
			h.quit = true
			continue
		}
		_, next := h.m.Update(msg)
		h.settle(next)
	}
}

// send delivers msg and returns the resulting command without running it.
func (h *harness) send(msg tea.Msg) tea.Cmd {
	_, cmd := h.m.Update(msg)
	return cmd
}

func (h *harness) start() {
	h.t.Helper()
	h.settle(h.m.Init())
}

func (h *harness) typeText(s string) {
	h.t.Helper()
	h.settle(h.send(runes(s)))
}

func (h *harness) press(k tea.KeyType) {
	h.t.Helper()
	h.settle(h.send(tea.KeyMsg{Type: k}))
}

func runes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func (h *harness) wantPane(want string) {
	h.t.Helper()
	if got := h.m.View(); !strings.Contains(got, want) {
		h.t.Errorf("view does not contain %q:\n%s", want, got)
	}
}

func (h *harness) wantNoOutcome() {
	h.t.Helper()
	if h.quit {
		h.t.Error("program quit unexpectedly")
	}
	if out, expr, ok := h.m.Outcome(); ok {
		h.t.Errorf("unexpected outcome: output %q, expr %q", out, expr)
	}
}

func (h *harness) wantOutcome(output, expr string) {
	h.t.Helper()
	if !h.quit {
		h.t.Error("program did not quit")
	}
	gotOut, gotExpr, ok := h.m.Outcome()
	if !ok {
		h.t.Fatal("no outcome")
	}
	if gotOut != output || gotExpr != expr {
		h.t.Errorf("got output %q, expr %q; want output %q, expr %q", gotOut, gotExpr, output, expr)
	}
}

func doc() map[string]any {
	return map[string]any{"a": "one", "b": "two"}
}

func TestStartShowsIdentity(t *testing.T) {
	h := newHarness(t, doc())
	h.start()
	h.wantPane(`"a": "one"`)
	h.wantPane(helpText)
}

func TestTypingUpdatesResult(t *testing.T) {
	h := newHarness(t, doc())
	h.start()
	h.typeText(".a")
	h.wantPane(`"one"`)
	if strings.Contains(h.m.View(), `"two"`) {
		t.Errorf("old result still shown:\n%s", h.m.View())
	}
}

func TestRapidTypingEvaluatesOnlyTheLatest(t *testing.T) {
	h := newHarness(t, doc())
	h.start()
	h.calls = nil

	first := h.send(runes("."))
	second := h.send(runes("a"))
	h.settle(first)
	if len(h.calls) != 0 {
		t.Errorf("stale debounce started an evaluation: %+v", h.calls)
	}
	h.settle(second)
	if len(h.calls) != 1 || h.calls[0].expr != ".a" {
		t.Errorf("got calls %+v, want one for .a", h.calls)
	}
	if h.calls[0].maxBytes != previewMaxBytes {
		t.Errorf("preview used maxBytes %d, want %d", h.calls[0].maxBytes, previewMaxBytes)
	}
}

func TestStaleResultIsDropped(t *testing.T) {
	h := newHarness(t, doc())
	h.start()
	h.typeText(".a")
	h.send(resultMsg{seq: h.m.seq - 1, res: eval.Result{Output: "\"stale\"\n"}})
	if strings.Contains(h.m.View(), "stale") {
		t.Errorf("stale result shown:\n%s", h.m.View())
	}
}

func TestEditCancelsRunningEvaluation(t *testing.T) {
	h := newHarness(t, doc())
	h.start()
	h.settle(h.send(runes(".")))
	h.calls = nil

	// Debounce ends and the evaluation is under way; its result has not arrived.
	cmd := h.send(debounceMsg{seq: h.m.seq})
	messages(cmd)
	if len(h.calls) != 1 {
		t.Fatalf("got calls %+v, want one", h.calls)
	}
	running := h.calls[0].ctx
	if running.Err() != nil {
		t.Fatalf("context ended early: %v", running.Err())
	}

	h.send(runes("a"))
	if !errors.Is(running.Err(), context.Canceled) {
		t.Errorf("running evaluation was not canceled: %v", running.Err())
	}
}

func TestErrorKeepsLastResult(t *testing.T) {
	h := newHarness(t, doc())
	h.start()
	h.typeText(".a")
	h.typeText(" | error(\"boom\")")
	h.wantPane(`"one"`)
	h.wantPane("boom")
	h.wantNoOutcome()
}

func TestSyntaxErrorKeepsLastResult(t *testing.T) {
	h := newHarness(t, doc())
	h.start()
	h.typeText(".[")
	h.wantPane(`"a": "one"`)
	h.wantPane("unexpected")
}

func TestFixingTheExpressionClearsTheError(t *testing.T) {
	h := newHarness(t, doc())
	h.start()
	h.typeText(".[")
	h.press(tea.KeyBackspace)
	h.wantPane(helpText)
	if strings.Contains(h.m.View(), "unexpected") {
		t.Errorf("error still shown:\n%s", h.m.View())
	}
}

func TestEnterConfirmsWithoutReevaluating(t *testing.T) {
	h := newHarness(t, doc())
	h.start()
	h.typeText(".a")
	h.calls = nil
	h.press(tea.KeyEnter)
	h.wantOutcome("\"one\"\n", ".a")
	if len(h.calls) != 0 {
		t.Errorf("complete result was evaluated again: %+v", h.calls)
	}
}

func TestEnterWithEmptyExpressionReportsIdentity(t *testing.T) {
	h := newHarness(t, "x")
	h.start()
	h.press(tea.KeyEnter)
	h.wantOutcome("\"x\"\n", ".")
}

func TestEnterOnErrorStaysOpen(t *testing.T) {
	h := newHarness(t, doc())
	h.start()
	h.typeText(".[")
	h.calls = nil
	h.press(tea.KeyEnter)
	h.wantNoOutcome()
	h.wantPane("unexpected")
	if len(h.calls) != 0 {
		t.Errorf("failed expression was evaluated again: %+v", h.calls)
	}

	// The user can then fix the expression and confirm.
	h.typeText("\"a\"]")
	h.press(tea.KeyEnter)
	h.wantOutcome("\"one\"\n", `.["a"]`)
}

func TestEnterDuringDebounceUsesLatestExpression(t *testing.T) {
	h := newHarness(t, doc())
	h.start()
	h.calls = nil

	debounce := h.send(runes(".a"))
	h.settle(h.send(tea.KeyMsg{Type: tea.KeyEnter}))
	h.wantOutcome("\"one\"\n", ".a")

	if len(h.calls) != 1 || h.calls[0].expr != ".a" || h.calls[0].maxBytes != 0 {
		t.Errorf("got calls %+v, want one unlimited call for .a", h.calls)
	}
	h.calls = nil
	h.settle(debounce)
	if len(h.calls) != 0 {
		t.Errorf("stale debounce started an evaluation: %+v", h.calls)
	}
}

func TestEnterDuringDebounceWithBadExpressionStaysOpen(t *testing.T) {
	h := newHarness(t, doc())
	h.start()
	h.send(runes(".["))
	h.press(tea.KeyEnter)
	h.wantNoOutcome()
	h.wantPane("unexpected")
	h.wantPane(`"a": "one"`)
}

func TestEnterOnTruncatedPreviewOutputsEverything(t *testing.T) {
	h := newHarness(t, nil)
	h.start()
	// More than previewMaxBytes of output.
	h.typeText("range(400000)")
	h.wantPane(truncatedText)
	if len(h.m.result.Output) > previewMaxBytes {
		t.Errorf("preview is %d bytes, over the limit", len(h.m.result.Output))
	}

	h.press(tea.KeyEnter)
	if !h.quit {
		t.Fatal("program did not quit")
	}
	out, _, ok := h.m.Outcome()
	if !ok {
		t.Fatal("no outcome")
	}
	if !strings.HasSuffix(out, "\n399999\n") {
		t.Errorf("output is incomplete: %d bytes ending in %q", len(out), out[len(out)-20:])
	}
}

func TestEnterAfterTimeoutEvaluatesWithoutLimit(t *testing.T) {
	h := newHarness(t, doc())
	h.start()
	h.useEvaluator(func(ctx context.Context, expr string, docs []any, opts eval.Options) (eval.Result, error) {
		if opts.MaxBytes != 0 {
			return eval.Result{}, context.DeadlineExceeded
		}
		return eval.Result{Output: "\"slow\"\n"}, nil
	})
	h.typeText(".slow")
	h.wantPane("timed out")
	h.wantNoOutcome()

	h.press(tea.KeyEnter)
	h.wantOutcome("\"slow\"\n", ".slow")
}

func TestFinishingFailureStaysOpen(t *testing.T) {
	h := newHarness(t, doc())
	h.start()
	h.typeText(".a")
	h.useEvaluator(func(context.Context, string, []any, eval.Options) (eval.Result, error) {
		return eval.Result{}, errors.New("late failure")
	})
	h.send(runes("x"))
	h.press(tea.KeyEnter)
	h.wantNoOutcome()
	h.wantPane("late failure")
	h.wantPane(`"one"`)
}

func TestSecondEnterWhileFinishingIsIgnored(t *testing.T) {
	h := newHarness(t, doc())
	h.start()
	h.calls = nil
	h.send(runes(".a"))

	cmd := h.send(tea.KeyMsg{Type: tea.KeyEnter})
	h.wantPane(finishingText)
	if again := h.send(tea.KeyMsg{Type: tea.KeyEnter}); again != nil {
		t.Error("second Enter started more work")
	}
	h.settle(cmd)
	if len(h.calls) != 1 {
		t.Errorf("got calls %+v, want one", h.calls)
	}
	h.wantOutcome("\"one\"\n", ".a")
}

func TestEditWhileFinishingStopsIt(t *testing.T) {
	h := newHarness(t, doc())
	h.start()
	h.send(runes(".a"))
	h.calls = nil

	finishing := h.send(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := messages(finishing)
	ctx := h.calls[0].ctx

	debounce := h.send(runes("x"))
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Errorf("finishing evaluation was not canceled: %v", ctx.Err())
	}
	for _, msg := range msgs {
		h.send(msg) // The result of the abandoned evaluation arrives late.
	}
	h.wantNoOutcome()

	// The edited expression goes back to being previewed.
	h.settle(debounce)
	h.wantNoOutcome()
	h.wantPane("null")
	h.wantPane(helpText)
}

func TestCtrlCCancelsWithoutOutcome(t *testing.T) {
	h := newHarness(t, doc())
	h.start()
	h.typeText(".a")
	h.send(runes("x"))
	cmd := h.send(debounceMsg{seq: h.m.seq})
	messages(cmd)
	running := h.calls[len(h.calls)-1].ctx

	h.settle(h.send(tea.KeyMsg{Type: tea.KeyCtrlC}))
	if !h.quit {
		t.Error("program did not quit")
	}
	if _, _, ok := h.m.Outcome(); ok {
		t.Error("canceled program has an outcome")
	}
	if !errors.Is(running.Err(), context.Canceled) {
		t.Errorf("running evaluation was not canceled: %v", running.Err())
	}
}

func TestSlowEvaluationIsMentioned(t *testing.T) {
	h := newHarness(t, doc())
	h.start()
	h.send(runes("."))
	seq := h.m.seq

	h.send(slowMsg{seq: seq - 1})
	if strings.Contains(h.m.View(), slowText) {
		t.Error("stale slow message took effect")
	}
	h.send(slowMsg{seq: seq})
	h.wantPane(slowText)

	h.send(resultMsg{seq: seq, res: eval.Result{Output: "1\n"}})
	if strings.Contains(h.m.View(), slowText) {
		t.Error("slow text outlived the evaluation")
	}
}

func TestScrollKeysMoveThePaneNotTheInput(t *testing.T) {
	h := newHarness(t, manyLines(100)...)
	h.start()

	h.press(tea.KeyPgDown)
	if h.m.pane.YOffset == 0 {
		t.Error("PgDn did not scroll")
	}
	offset := h.m.pane.YOffset
	h.press(tea.KeyDown)
	if h.m.pane.YOffset != offset+1 {
		t.Errorf("Down: offset %d, want %d", h.m.pane.YOffset, offset+1)
	}
	h.press(tea.KeyUp)
	h.press(tea.KeyPgUp)
	if h.m.pane.YOffset != 0 {
		t.Errorf("offset %d, want 0", h.m.pane.YOffset)
	}
	if got := h.m.input.Value(); got != "" {
		t.Errorf("scroll keys changed the input to %q", got)
	}
}

func TestPanKeysMoveThePaneNotTheCursor(t *testing.T) {
	long := strings.Repeat("x", 200) + "END"
	h := newHarness(t, long)
	h.start()
	if strings.Contains(h.m.View(), "END") {
		t.Fatal("line should not fit on screen yet")
	}
	for range 40 {
		h.press(tea.KeyShiftRight)
	}
	h.wantPane("END")
	for range 40 {
		h.press(tea.KeyShiftLeft)
	}
	if strings.Contains(h.m.View(), "END") {
		t.Error("Shift+Left did not pan back")
	}
}

func TestPanningContentThatFitsChangesNothing(t *testing.T) {
	// 76 columns including the quotes: close enough to the edge of the 80-column
	// window that a wrong offset would cut off the end.
	h := newHarness(t, strings.Repeat("x", 74))
	h.start()
	before := h.m.View()
	for _, k := range []tea.KeyType{tea.KeyShiftLeft, tea.KeyShiftLeft, tea.KeyShiftRight} {
		h.press(k)
		if got := h.m.View(); got != before {
			t.Errorf("view changed:\n%s", got)
		}
	}
}

func TestResizeResetsPan(t *testing.T) {
	line := strings.Repeat("x", 100) + "END"
	h := newHarness(t, line)
	h.start()
	for range 40 {
		h.press(tea.KeyShiftRight)
	}
	// The line now fits, so no pan key could bring its start back into view.
	h.m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	h.wantPane(`"xxx`)
	h.wantPane("END")
}

func TestNewResultResetsPan(t *testing.T) {
	long := strings.Repeat("x", 200) + "END"
	h := newHarness(t, long)
	h.start()
	for range 40 {
		h.press(tea.KeyShiftRight)
	}
	h.typeText(`[.]`)
	h.wantPane(`"xxx`)
}

func TestCharactersThatViewportBindsGoToTheInput(t *testing.T) {
	h := newHarness(t, manyLines(100)...)
	h.start()
	// viewport's default key map binds all of these, and jq expressions need them.
	for _, s := range []string{"j", "k", "f", "b", "d", "u", " "} {
		h.send(runes(s))
	}
	if got := h.m.input.Value(); got != "jkfbdu " {
		t.Errorf("input is %q", got)
	}
	if h.m.pane.YOffset != 0 {
		t.Errorf("typing scrolled the pane to %d", h.m.pane.YOffset)
	}
}

func TestLeftRightMoveTheCursor(t *testing.T) {
	h := newHarness(t, doc())
	h.start()
	h.typeText(".b")
	h.press(tea.KeyLeft)
	h.typeText("a")
	if got := h.m.input.Value(); got != ".ab" {
		t.Errorf("input is %q, want .ab", got)
	}
}

func TestNewResultScrollsToTop(t *testing.T) {
	h := newHarness(t, manyLines(100)...)
	h.start()
	h.press(tea.KeyPgDown)
	h.typeText(`"x"`)
	h.wantPane(`"x"`)
	if h.m.pane.YOffset != 0 {
		t.Errorf("offset %d, want 0", h.m.pane.YOffset)
	}
}

func TestSameResultKeepsScrollPosition(t *testing.T) {
	h := newHarness(t, manyLines(100)...)
	h.start()
	h.press(tea.KeyPgDown)
	offset := h.m.pane.YOffset
	h.typeText(" ")
	if h.m.pane.YOffset != offset {
		t.Errorf("offset %d, want %d", h.m.pane.YOffset, offset)
	}
}

func TestResize(t *testing.T) {
	h := newHarness(t, manyLines(100)...)
	h.start()
	h.press(tea.KeyPgDown)
	h.m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	if got := lipgloss.Height(h.m.View()); got != 10 {
		t.Errorf("view is %d rows, want 10", got)
	}
	if got := lipgloss.Width(h.m.View()); got > 40 {
		t.Errorf("view is %d columns, want at most 40", got)
	}
}

func TestTinyWindowsDoNotPanic(t *testing.T) {
	h := newHarness(t, manyLines(100)...)
	h.start()
	for _, size := range [][2]int{{0, 0}, {1, 1}, {2, 2}, {3, 1}, {1, 3}} {
		h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		_ = h.m.View()
	}
}

func TestViewBeforeWindowSizeDoesNotPanic(t *testing.T) {
	m := New([]any{"x"})
	_ = m.View()
}

func TestErrorIsOneSanitizedLine(t *testing.T) {
	h := newHarness(t, doc())
	h.start()
	h.typeText(`error("line1\nline2\u001b[2J` + strings.Repeat("x", 200) + `")`)
	status := h.m.statusLine()

	if strings.Contains(status, "\x1b[2J") {
		t.Errorf("escape sequence from the error reached the screen: %q", status)
	}
	if !strings.Contains(status, "line1 line2") {
		t.Errorf("status is %q", status)
	}
	if got := lipgloss.Height(status); got != 1 {
		t.Errorf("status is %d rows, want 1", got)
	}
	if got := lipgloss.Width(status); got > 80 {
		t.Errorf("status is %d columns, want at most 80", got)
	}
}

func TestNoticeTakesThePlaceOfTheHelp(t *testing.T) {
	h := newHarness(t, doc())
	h.m.SetNotice("input is not valid UTF-8")
	h.start()
	h.wantPane("input is not valid UTF-8")
	if strings.Contains(h.m.View(), helpText) {
		t.Errorf("help shown with the notice:\n%s", h.m.View())
	}

	// A message about the expression comes first, and the notice comes back after.
	h.typeText(".[")
	h.wantPane("unexpected")
	h.press(tea.KeyBackspace)
	h.wantPane("input is not valid UTF-8")
}

func TestNoticeIsSanitized(t *testing.T) {
	h := newHarness(t, doc())
	h.m.SetNotice("bad\x1b[2Jinput")
	h.start()
	if status := h.m.statusLine(); strings.Contains(status, "\x1b[2J") || !strings.Contains(status, "bad [2Jinput") {
		t.Errorf("status is %q", status)
	}
}

func TestCursorIsVisibleWithoutColorSupport(t *testing.T) {
	// Tests have no terminal, so the default renderer drops all attributes, the same
	// as it does with NO_COLOR set.
	h := newHarness(t, doc())
	h.start()
	if got := h.m.input.View(); !strings.Contains(got, "\x1b[7m") {
		t.Errorf("input has no reverse video cursor: %q", got)
	}
}

func TestOutcomeBeforeConfirmation(t *testing.T) {
	h := newHarness(t, doc())
	h.start()
	h.wantNoOutcome()
}

func keysDoc() map[string]any {
	return map[string]any{"alpha": json.Number("1"), "beta": json.Number("2"), "gamma": json.Number("3")}
}

// paneLines returns the rows of the screen below the input and the status.
func (h *harness) paneLines() []string {
	return strings.Split(h.m.View(), "\n")[chromeHeight:]
}

func (h *harness) wantInput(value string, cursor int) {
	h.t.Helper()
	if got := h.m.input.Value(); got != value {
		h.t.Errorf("input is %q, want %q", got, value)
	}
	if got := h.m.input.Position(); got != cursor {
		h.t.Errorf("cursor at %d, want %d", got, cursor)
	}
}

func (h *harness) wantCandidates(want ...string) {
	h.t.Helper()
	var got []string
	for _, c := range h.m.candidates() {
		got = append(got, c.Text)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		h.t.Errorf("candidates %q, want %q", got, want)
	}
}

func TestCandidatesAppearUnderTheExpression(t *testing.T) {
	h := newHarness(t, keysDoc())
	h.start()
	h.typeText(".")
	h.wantCandidates("alpha", "beta", "gamma")
	lines := h.paneLines()
	// The list starts below the expression, after the prompt, and covers the pane.
	for i, row := range []string{"{  alpha ", "   beta  ", "   gamma "} {
		if !strings.HasPrefix(stripANSI(lines[i]), row) {
			t.Errorf("row %d is %q, want it to start with %q", i, lines[i], row)
		}
	}
	// Nothing is selected until Ctrl+N or Ctrl+P.
	for i := range 3 {
		if strings.Contains(lines[i], "\x1b[7m") {
			t.Errorf("row %d is selected: %q", i, lines[i])
		}
	}
}

func TestListIsBelowThePartialKey(t *testing.T) {
	h := newHarness(t, map[string]any{"items": map[string]any{"alpha": nil}})
	h.start()
	h.typeText(".items.al")
	// The text of the list starts in the column of "al".
	keyCol := lipgloss.Width(prompt + ".items.")
	if got := strings.Index(stripANSI(h.paneLines()[0]), " alpha "); got != keyCol-1 {
		t.Errorf("list at column %d, want %d:\n%s", got, keyCol-1, h.m.View())
	}
}

func TestListMovesLeftToFit(t *testing.T) {
	h := newHarness(t, map[string]any{"alpha": nil})
	h.start()
	// Below the key, the list would end past column 80.
	h.typeText(strings.Repeat(" ", 75) + ".")
	if got := strings.Index(stripANSI(h.paneLines()[0]), " alpha "); got != 80-len(" alpha ") {
		t.Errorf("list at column %d, want it at the right edge:\n%s", got, h.m.View())
	}
}

func TestTypingNarrowsCandidatesAtOnce(t *testing.T) {
	h := newHarness(t, keysDoc())
	h.start()
	h.typeText(".")
	// The new candidates have not been computed yet.
	h.send(runes("A"))
	h.wantCandidates("alpha")
}

func TestCandidatesDisappearAfterTheKey(t *testing.T) {
	h := newHarness(t, keysDoc())
	h.start()
	h.typeText(".")
	h.send(runes(" "))
	h.wantCandidates()
}

func TestTabInsertsTheCandidate(t *testing.T) {
	for _, k := range []tea.KeyMsg{{Type: tea.KeyTab}, {Type: tea.KeyCtrlY}} {
		t.Run(k.String(), func(t *testing.T) {
			h := newHarness(t, keysDoc())
			h.start()
			h.typeText(".b")
			h.settle(h.send(k))
			h.wantInput(".beta", 5)
			h.wantCandidates()
			h.wantPane("2")
		})
	}
}

func TestTabKeepsTheTextAfterTheCursor(t *testing.T) {
	h := newHarness(t, keysDoc())
	h.start()
	h.typeText(".g + 1")
	for range 4 {
		h.press(tea.KeyLeft)
	}
	h.wantCandidates("gamma")
	h.press(tea.KeyTab)
	h.wantInput(".gamma + 1", 6)
}

func TestTabQuotesKeysThatNeedIt(t *testing.T) {
	h := newHarness(t, map[string]any{"名前": "太郎"})
	h.start()
	h.typeText(".")
	h.press(tea.KeyTab)
	h.wantInput(`."名前"`, 5)
	h.wantPane(`"太郎"`)
}

func TestTabWithoutCandidatesDoesNothing(t *testing.T) {
	h := newHarness(t, keysDoc())
	h.start()
	h.typeText(".x")
	h.press(tea.KeyTab)
	h.wantInput(".x", 2)
}

// wantSelectedRow checks that row, and no other row of the list, is selected.
// -1 means none.
func (h *harness) wantSelectedRow(row int) {
	h.t.Helper()
	for i, line := range h.paneLines()[:maxCandidateRows] {
		if got := strings.Contains(line, "\x1b[7m"); got != (i == row) {
			h.t.Errorf("row %d selected: %v, want %v: %q", i, got, i == row, line)
		}
	}
}

func TestCtrlNAndCtrlPCycleThroughCandidatesAndTheTypedText(t *testing.T) {
	h := newHarness(t, keysDoc())
	h.start()
	h.typeText(".")
	steps := []struct {
		key   tea.KeyType
		input string
		row   int
	}{
		{tea.KeyCtrlN, ".alpha", 0},
		{tea.KeyCtrlN, ".beta", 1},
		{tea.KeyCtrlN, ".gamma", 2},
		{tea.KeyCtrlN, ".", -1}, // Back to the typed text.
		{tea.KeyCtrlN, ".alpha", 0},
		{tea.KeyCtrlP, ".", -1},
		{tea.KeyCtrlP, ".gamma", 2},
	}
	for _, s := range steps {
		h.press(s.key)
		h.wantInput(s.input, len(s.input))
		h.wantSelectedRow(s.row)
		// The list stays whole although the input changed.
		h.wantCandidates("alpha", "beta", "gamma")
	}
}

func TestCyclingPreviewsTheCandidate(t *testing.T) {
	h := newHarness(t, keysDoc())
	h.start()
	h.typeText(".")
	h.press(tea.KeyCtrlN)
	h.press(tea.KeyCtrlN)
	if got := h.m.result.Output; got != "2\n" {
		t.Errorf("preview is %q, want the result of .beta", got)
	}
}

func TestCyclingKeepsTheTextAfterTheCursor(t *testing.T) {
	h := newHarness(t, keysDoc())
	h.start()
	h.typeText(".g + 1")
	for range 4 {
		h.press(tea.KeyLeft)
	}
	h.press(tea.KeyCtrlN)
	h.wantInput(".gamma + 1", 6)
	h.wantPane("4")
	h.press(tea.KeyCtrlN)
	h.wantInput(".g + 1", 2)
}

func TestEnterWhileCyclingConfirmsTheCandidate(t *testing.T) {
	h := newHarness(t, keysDoc())
	h.start()
	h.typeText(".")
	// Enter right away, before the preview of the candidate arrives.
	h.send(tea.KeyMsg{Type: tea.KeyCtrlN})
	h.press(tea.KeyEnter)
	h.wantOutcome("1\n", ".alpha")
}

func TestTabEndsTheCycleWithTheCandidate(t *testing.T) {
	h := newHarness(t, keysDoc())
	h.start()
	h.typeText(".")
	h.press(tea.KeyCtrlN)
	h.press(tea.KeyCtrlN)
	h.press(tea.KeyTab)
	h.wantInput(".beta", 5)
	h.wantCandidates()
	h.wantSelectedRow(-1)
}

func TestTabBackAtTheTypedTextInsertsTheFirst(t *testing.T) {
	h := newHarness(t, keysDoc())
	h.start()
	h.typeText(".")
	h.press(tea.KeyCtrlN)
	h.press(tea.KeyCtrlP)
	h.press(tea.KeyTab)
	h.wantInput(".alpha", 6)
}

func TestEscWhileCyclingPutsBackTheTypedText(t *testing.T) {
	h := newHarness(t, keysDoc())
	h.start()
	h.typeText(".b + 1")
	for range 4 {
		h.press(tea.KeyLeft)
	}
	h.press(tea.KeyCtrlN)
	h.wantInput(".beta + 1", 5)
	if got := h.m.result.Output; got != "3\n" {
		t.Errorf("preview is %q, want the result of .beta + 1", got)
	}
	h.press(tea.KeyEsc)
	h.wantInput(".b + 1", 2)
	h.wantCandidates()
	if got := h.m.result.Output; got != "1\n" {
		t.Errorf("preview is %q, want the result of .b + 1", got)
	}
}

func TestEditingEndsTheCycle(t *testing.T) {
	h := newHarness(t, keysDoc())
	h.start()
	h.typeText(".")
	h.press(tea.KeyCtrlN)
	h.press(tea.KeyBackspace)
	h.wantInput(".alph", 5)
	// The candidates are for the edited text again.
	h.wantCandidates("alpha")
	h.wantSelectedRow(-1)
	h.press(tea.KeyCtrlN)
	h.wantInput(".alpha", 6)
}

// Windows reports pressing Ctrl on its own as a key, before the N of each Ctrl+N.
func TestKeysThatChangeNothingKeepTheCycle(t *testing.T) {
	ctrlAlone := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0}}
	h := newHarness(t, keysDoc())
	h.start()
	h.typeText(".")
	h.press(tea.KeyCtrlN)
	for _, k := range []tea.KeyMsg{ctrlAlone, {Type: tea.KeyF5}, {Type: tea.KeyEnd}} {
		h.settle(h.send(k))
	}
	h.wantInput(".alpha", 6)
	h.settle(h.send(ctrlAlone))
	h.press(tea.KeyCtrlN)
	h.wantInput(".beta", 5)
	h.wantSelectedRow(1)
}

func TestCtrlNWithoutCandidatesDoesNothing(t *testing.T) {
	h := newHarness(t, keysDoc())
	h.start()
	h.typeText(".x")
	h.press(tea.KeyCtrlN)
	h.wantInput(".x", 2)
}

func TestSelectionScrollsTheList(t *testing.T) {
	doc := map[string]any{}
	for i := range 12 {
		doc[fmt.Sprintf("k%02d", i)] = nil
	}
	h := newHarness(t, doc)
	h.start()
	h.typeText(".")
	lines := h.paneLines()
	if got := stripANSI(lines[maxCandidateRows]); !strings.HasPrefix(got, "   -/12") {
		t.Errorf("counter row is %q", got)
	}
	for range 8 {
		h.press(tea.KeyCtrlN)
	}
	lines = h.paneLines()
	if got := stripANSI(lines[maxCandidateRows-1]); !strings.HasPrefix(got, "   k07") {
		t.Errorf("last row is %q, want the selected k07", got)
	}
	if got := stripANSI(lines[maxCandidateRows]); !strings.HasPrefix(got, "   8/12") {
		t.Errorf("counter row is %q", got)
	}
	h.press(tea.KeyTab)
	h.wantInput(".k07", 4)
}

func TestEscClosesCandidatesUntilTheNextEdit(t *testing.T) {
	h := newHarness(t, keysDoc())
	h.start()
	h.typeText(".")
	h.press(tea.KeyEsc)
	h.wantCandidates()
	h.press(tea.KeyTab)
	h.wantInput(".", 1)

	h.typeText("a")
	h.wantCandidates("alpha")
}

func TestArrowsScrollWhileCandidatesAreShown(t *testing.T) {
	doc := keysDoc()
	doc["list"] = manyLines(100)
	h := newHarness(t, doc)
	h.start()
	h.typeText(".")
	h.press(tea.KeyDown)
	if h.m.pane.YOffset != 1 {
		t.Errorf("offset %d, want 1", h.m.pane.YOffset)
	}
	h.press(tea.KeyTab)
	h.wantInput(".alpha", 6)
}

func TestEnterConfirmsWhileCandidatesAreShown(t *testing.T) {
	h := newHarness(t, keysDoc())
	h.start()
	h.typeText(".al")
	h.wantCandidates("alpha")
	h.press(tea.KeyEnter)
	h.wantOutcome("null\n", ".al")
}

func TestCursorMoveRequestsCandidates(t *testing.T) {
	h := newHarness(t, keysDoc())
	h.start()
	h.typeText(".g | .")
	h.wantCandidates()
	for range 4 {
		h.press(tea.KeyLeft)
	}
	h.wantCandidates("gamma")
}

func TestStaleCompletionIsDropped(t *testing.T) {
	h := newHarness(t, keysDoc())
	h.start()
	h.typeText(".")
	stale := eval.Complete(context.Background(), ".", []any{map[string]any{"stale": nil}})
	h.send(completionMsg{seq: h.m.compSeq - 1, comp: stale})
	h.wantCandidates("alpha", "beta", "gamma")
}

func TestEditCancelsRunningCompletion(t *testing.T) {
	h := newHarness(t, keysDoc())
	var running context.Context
	h.m.complete = func(ctx context.Context, text string, docs []any) eval.Completion {
		running = ctx
		return eval.Completion{}
	}
	h.start()
	h.send(runes("."))
	messages(h.send(completeDebounceMsg{seq: h.m.compSeq}))
	if running == nil || running.Err() != nil {
		t.Fatalf("completion did not run: %v", running)
	}
	h.send(runes("a"))
	if !errors.Is(running.Err(), context.Canceled) {
		t.Errorf("running completion was not canceled: %v", running.Err())
	}
}

func TestCandidatesFitTinyWindows(t *testing.T) {
	h := newHarness(t, map[string]any{strings.Repeat("k", 100): nil, "名前": nil})
	h.start()
	h.typeText(".")
	for _, size := range [][2]int{{80, 24}, {12, 5}, {7, 4}, {3, 3}, {1, 1}, {0, 0}} {
		h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		pane := h.m.paneView()
		if size[0] == 0 {
			continue
		}
		if got := lipgloss.Width(pane); got > size[0] {
			t.Errorf("%v: pane is %d columns:\n%s", size, got, pane)
		}
		if got := lipgloss.Height(pane); got > max(size[1]-chromeHeight, 1) {
			t.Errorf("%v: pane is %d rows:\n%s", size, got, pane)
		}
	}
}

func TestCandidatesOverWideCharacters(t *testing.T) {
	h := newHarness(t, map[string]any{"a": "あいうえおかきくけこ"})
	h.start()
	h.typeText(".")
	for _, line := range h.paneLines() {
		if got := lipgloss.Width(line); got > 80 {
			t.Errorf("row is %d columns: %q", got, line)
		}
	}
}

func TestResultColors(t *testing.T) {
	h := newHarness(t, map[string]any{"a": "x", "n": nil})
	h.m.color = true
	h.start()
	view := h.m.View()
	for _, want := range []string{"\x1b[1m{", "\x1b[34;1m\"a\"", "\x1b[32m\"x\"", "\x1b[90mnull"} {
		if !strings.Contains(view, want) {
			t.Errorf("screen lacks %q:\n%q", want, view)
		}
	}
}

func TestResultColorsUnderCandidates(t *testing.T) {
	long := strings.Repeat("x", 40)
	h := newHarness(t, map[string]any{"alpha": long, "beta": "あいうえおかきくけこ"})
	h.m.color = true
	h.start()
	h.typeText(".")
	h.wantCandidates("alpha", "beta")
	lines := h.paneLines()
	// The rows look as they do without colors.
	for i, want := range []string{`{  alpha `, `   beta  : "` + long + `",`} {
		if got := stripANSI(lines[i]); !strings.HasPrefix(got, want) {
			t.Errorf("row %d is %q, want it to start with %q", i, got, want)
		}
	}
	// The result right of the list keeps its colors.
	if !strings.Contains(lines[1], "\x1b[32m") {
		t.Errorf("row 1 lost the string color: %q", lines[1])
	}
	for _, line := range lines {
		if got := lipgloss.Width(line); got > 80 {
			t.Errorf("row is %d columns: %q", got, line)
		}
	}
}

// useRaw makes the evaluator write strings as they are, like jq -r.
func (h *harness) useRaw() {
	h.useEvaluator(func(ctx context.Context, expr string, docs []any, opts eval.Options) (eval.Result, error) {
		opts.Raw = true
		return eval.Eval(ctx, expr, docs, opts)
	})
}

func TestRawStringsInPane(t *testing.T) {
	h := newHarness(t, map[string]any{
		"tab":   "x\ty",
		"lines": "one\ntwo\n\n",
		"json":  `"k": null`,
	})
	h.m.color = true
	h.useRaw()
	h.start()
	h.typeText(`.tab, .lines, .json, {k: 1}`)
	lines := h.paneLines()
	want := []string{"x       y", "one", "two", "", "", `"k": null`, "{", `  "k": 1`, "}"}
	for i, w := range want {
		if got := stripANSI(lines[i]); strings.TrimRight(got, " ") != w {
			t.Errorf("row %d is %q, want %q", i, got, w)
		}
	}
	// Raw strings have no colors, even when they look like JSON.
	for i := range 6 {
		if strings.Contains(lines[i], "\x1b[") {
			t.Errorf("raw row %d has colors: %q", i, lines[i])
		}
	}
	if !strings.Contains(lines[7], "\x1b[34;1m\"k\"") {
		t.Errorf("JSON row has no colors: %q", lines[7])
	}
}

func TestRawControlCharactersAreVisible(t *testing.T) {
	h := newHarness(t, "\x1b[31mred\x07\x7f\u0085\r")
	h.useRaw()
	h.start()
	if got, want := strings.TrimRight(h.paneLines()[0], " "), `^[[31mred^G^?\u0085^M`; got != want {
		t.Errorf("row is %q, want %q", got, want)
	}
}

func TestVisible(t *testing.T) {
	tests := []struct {
		line  string
		color bool
		want  string
	}{
		{"plain", false, "plain"},
		{"\tx", false, "        x"},
		{"ab\tx", false, "ab      x"},
		{"あ\tx", false, "あ      x"},
		{"12345678\tx", false, "12345678        x"},
		{"^[\x1b", false, "^[^["},
		{"a\x00", false, "a^@"},
		{"\x1b", true, "\x1b[2m^[\x1b[0m"},
		{"\x1bx\tx", false, "^[x     x"},
	}
	for _, tt := range tests {
		if got := visible(tt.line, tt.color); got != tt.want {
			t.Errorf("visible(%q, %v) = %q, want %q", tt.line, tt.color, got, tt.want)
		}
	}
}

func TestColumns(t *testing.T) {
	tests := []struct {
		s        string
		from, to int
		want     string
	}{
		{"abcdef", 0, 3, "abc"},
		{"abcdef", 2, 4, "cd"},
		{"abc", 1, 10, "bc"},
		{"あいう", 0, 4, "あい"},
		{"あいう", 1, 4, " い"},
		{"あいう", 0, 3, "あ "},
		{"あいう", 1, 2, " "},
		{"", 0, 3, ""},
		// Colors take no columns, are kept before the cut, and are reset at the end.
		{"\x1b[32mabc\x1b[0mdef", 1, 4, "\x1b[32mbc\x1b[0md\x1b[0m"},
		{"\x1b[32mabc\x1b[0mdef", 0, 2, "\x1b[32mab\x1b[0m"},
		{"ab\x1b[1m{\x1b[0m", 0, 3, "ab\x1b[1m{\x1b[0m\x1b[0m"},
		{"\x1b[32m\"あい\"\x1b[0m", 2, 5, "\x1b[32m い\x1b[0m"},
	}
	for _, tt := range tests {
		if got := columns(tt.s, tt.from, tt.to); got != tt.want {
			t.Errorf("columns(%q, %d, %d) = %q, want %q", tt.s, tt.from, tt.to, got, tt.want)
		}
	}
}

// stripANSI removes SGR sequences, which the tests only produce for the
// selected candidate.
func stripANSI(s string) string {
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			return s
		}
		j := strings.IndexByte(s[i:], 'm')
		if j < 0 {
			return s
		}
		s = s[:i] + s[i+j+1:]
	}
}

func manyLines(n int) []any {
	docs := make([]any, n)
	for i := range docs {
		docs[i] = json.Number("1")
	}
	return docs
}
