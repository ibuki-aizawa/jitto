package tui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ibuki-aizawa/jitto/internal/eval"
)

func itemsDoc() map[string]any {
	return map[string]any{"items": []any{
		map[string]any{"name": "b", "n": json.Number("2")},
		map[string]any{"name": "a", "n": json.Number("1")},
	}}
}

// fakeExamples are the examples the tests get for any expression.
var fakeExamples = []eval.Suggestion{
	{Text: ".items | map(.name)", Example: "map(.name)", Description: "the name of each item"},
	{Text: ".items | length", Example: "length", Description: "the number of items"},
	{Text: ".items | first", Example: "first", Description: "the first item"},
}

// newExamplesHarness types expr and opens the examples, which are fakeExamples.
func newExamplesHarness(t *testing.T, expr string) *harness {
	t.Helper()
	h := newHarness(t, itemsDoc())
	h.m.suggest = func(ctx context.Context, expr string, docs []any) []eval.Suggestion {
		return fakeExamples
	}
	h.start()
	h.typeText(expr)
	h.press(tea.KeyCtrlT)
	return h
}

func (h *harness) wantExampleRow(row int) {
	h.t.Helper()
	for i, line := range h.paneLines()[:len(fakeExamples)] {
		if got := strings.Contains(line, "\x1b[7m"); got != (i == row) {
			h.t.Errorf("row %d selected: %v, want %v: %q", i, got, i == row, line)
		}
	}
}

func (h *harness) wantStatus(want string) {
	h.t.Helper()
	if row := stripANSI(strings.Split(h.m.View(), "\n")[1]); row != want {
		h.t.Errorf("status line is %q, want %q", row, want)
	}
}

func TestCtrlTShowsExamplesUnderTheExpression(t *testing.T) {
	h := newExamplesHarness(t, ".items")
	h.wantStatus(examplesHelpText)
	lines := h.paneLines()
	// The examples start where they go in the input, after ".items | ".
	col := len("> .items | ") - 1
	for i, want := range []string{
		" map(.name)  the name of each item ",
		" length      the number of items   ",
		" first       the first item        ",
	} {
		if got := stripANSI(lines[i]); !strings.HasPrefix(got[min(col, len(got)):], want) {
			t.Errorf("row %d is %q, want %q from column %d", i, got, want, col)
		}
	}
	h.wantExampleRow(-1)
	h.wantInput(".items", 6)
}

func TestCtrlTUsesSuggest(t *testing.T) {
	h := newHarness(t, itemsDoc())
	h.start()
	h.typeText(".items")
	h.press(tea.KeyCtrlT)
	if h.m.examples == nil || len(h.m.examples.items) == 0 {
		t.Fatal("no examples")
	}
	if got := h.m.examples.items[0].Text; got != ".items | map(.name)" {
		t.Errorf("first example is %q", got)
	}
}

func TestCtrlNAndCtrlPCycleThroughExamplesAndTheTypedText(t *testing.T) {
	h := newExamplesHarness(t, ".items")
	steps := []struct {
		key   tea.KeyType
		input string
		row   int
	}{
		{tea.KeyCtrlN, ".items | map(.name)", 0},
		{tea.KeyCtrlN, ".items | length", 1},
		{tea.KeyCtrlN, ".items | first", 2},
		{tea.KeyCtrlN, ".items", -1}, // Back to the typed text.
		{tea.KeyCtrlP, ".items | first", 2},
	}
	for _, s := range steps {
		h.press(s.key)
		h.wantInput(s.input, len(s.input))
		h.wantExampleRow(s.row)
		h.wantStatus(examplesHelpText)
	}
}

func TestCyclingPreviewsTheExample(t *testing.T) {
	h := newExamplesHarness(t, ".items")
	h.press(tea.KeyCtrlN)
	h.press(tea.KeyCtrlN)
	if got := h.m.result.Output; got != "2\n" {
		t.Errorf("preview is %q, want the result of .items | length", got)
	}
}

func TestTabKeepsTheExample(t *testing.T) {
	h := newExamplesHarness(t, ".items")
	h.press(tea.KeyCtrlN)
	h.press(tea.KeyCtrlN)
	h.press(tea.KeyTab)
	h.wantInput(".items | length", 15)
	if h.m.examples != nil {
		t.Error("the list is still open")
	}
	h.wantHelp()
}

func TestTabWithoutSelectionWritesTheFirstExample(t *testing.T) {
	h := newExamplesHarness(t, ".items")
	h.press(tea.KeyCtrlY)
	h.wantInput(".items | map(.name)", 19)
	if got := h.m.result.Output; got != "[\n  \"b\",\n  \"a\"\n]\n" {
		t.Errorf("result is %q", got)
	}
}

func TestEscPutsBackTheTypedExpression(t *testing.T) {
	for _, k := range []tea.KeyType{tea.KeyEsc, tea.KeyCtrlT} {
		h := newExamplesHarness(t, ".items")
		h.press(tea.KeyCtrlN)
		h.press(k)
		h.wantInput(".items", 6)
		if h.m.examples != nil {
			t.Errorf("%v: the list is still open", k)
		}
		if got := h.m.result.Output; !strings.Contains(got, `"name": "b"`) {
			t.Errorf("%v: result is %q, want that of .items", k, got)
		}
	}
}

func TestEditingKeepsTheExampleAndClosesTheList(t *testing.T) {
	h := newExamplesHarness(t, ".items")
	h.press(tea.KeyCtrlN)
	h.typeText(" | length")
	h.wantInput(".items | map(.name) | length", 28)
	if h.m.examples != nil {
		t.Error("the list is still open")
	}
}

func TestKeysThatChangeNothingKeepTheExamples(t *testing.T) {
	ctrlAlone := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0}}
	h := newExamplesHarness(t, ".items")
	h.press(tea.KeyCtrlN)
	for _, k := range []tea.KeyMsg{ctrlAlone, {Type: tea.KeyF5}, {Type: tea.KeyDown}} {
		h.settle(h.send(k))
	}
	h.press(tea.KeyCtrlN)
	h.wantInput(".items | length", 15)
	h.wantExampleRow(1)
}

func TestEnterConfirmsTheExample(t *testing.T) {
	h := newExamplesHarness(t, ".items")
	h.press(tea.KeyCtrlN)
	h.press(tea.KeyCtrlN)
	h.press(tea.KeyEnter)
	h.wantOutcome("2\n", ".items | length")
}

func TestExamplesHideTheCandidates(t *testing.T) {
	h := newExamplesHarness(t, ".items[0].")
	h.wantCandidates()
	h.press(tea.KeyEsc)
	h.wantCandidates()
	// They come back with the next edit.
	h.typeText("n")
	h.wantCandidates("name")
}

func TestNoExamples(t *testing.T) {
	h := newHarness(t, itemsDoc())
	h.m.suggest = func(context.Context, string, []any) []eval.Suggestion { return nil }
	h.start()
	h.typeText(".items[0].n")
	h.press(tea.KeyCtrlT)
	h.wantStatus(noExamplesText)
	if h.m.examples != nil {
		t.Error("the list is open")
	}
	h.typeText("a")
	h.wantHelp()
}

func TestStatusWhileFindingExamples(t *testing.T) {
	h := newHarness(t, itemsDoc())
	h.start()
	// Leave the request unanswered.
	h.send(tea.KeyMsg{Type: tea.KeyCtrlT})
	h.wantStatus(findingText)
	h.press(tea.KeyCtrlN)
	h.press(tea.KeyTab)
	h.wantInput("", 0)
}

func TestEditDropsThePendingExamples(t *testing.T) {
	h := newHarness(t, itemsDoc())
	var running context.Context
	h.m.suggest = func(ctx context.Context, _ string, _ []any) []eval.Suggestion {
		running = ctx
		return fakeExamples
	}
	h.start()
	cmd := h.send(tea.KeyMsg{Type: tea.KeyCtrlT})
	msgs := messages(cmd)
	h.typeText(".")
	if !errors.Is(running.Err(), context.Canceled) {
		t.Errorf("request was not canceled: %v", running.Err())
	}
	for _, msg := range msgs {
		h.send(msg)
	}
	if h.m.examples != nil {
		t.Error("stale examples opened the list")
	}
}

func TestExamplesFitTinyWindows(t *testing.T) {
	h := newExamplesHarness(t, ".items")
	for _, size := range [][2]int{{80, 24}, {12, 5}, {7, 4}, {3, 3}, {1, 1}, {0, 0}} {
		h.m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		pane := h.m.paneView()
		if size[0] == 0 {
			continue
		}
		for i, line := range strings.Split(pane, "\n") {
			if w := len([]rune(stripANSI(line))); w > size[0] {
				t.Errorf("%v: row %d is %d wide: %q", size, i, w, line)
			}
		}
	}
}
