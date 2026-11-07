package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// cursorMark marks the cursor in test strings. It is not "|", which jq uses.
const cursorMark = "‸"

// cursorAt splits s at cursorMark into runes and the cursor position.
func cursorAt(s string) ([]rune, int) {
	i := strings.Index(s, cursorMark)
	return []rune(s[:i] + s[i+len(cursorMark):]), len([]rune(s[:i]))
}

func withCursor(s []rune, pos int) string {
	return string(s[:pos]) + cursorMark + string(s[pos:])
}

func TestWordStart(t *testing.T) {
	tests := []struct{ before, after string }{
		{".items[0].name‸", ".items[0].‸name"},
		{".items[0].‸", ".items[0‸]."},
		{".items[0‸", ".items[‸0"},
		{".items[‸", ".items‸["},
		{".items‸", ".‸items"},
		{".‸items", "‸.items"},
		{".a | .b‸", ".a | .‸b"},
		{".a | .‸", ".a | ‸."},
		{".a | ‸", ".a ‸| "},
		{".a ‸| ", ".‸a | "},
		{`."名前"‸`, `."名前‸"`},
		{`."名前‸"`, `."‸名前"`},
		{`."‸名前"`, `‸."名前"`},
		{".snake_case1‸", ".‸snake_case1"},
		{".na‸me", ".‸name"},
		{"   ‸", "‸   "},
		{"‸", "‸"},
		{"‸.a", "‸.a"},
	}
	for _, tt := range tests {
		s, pos := cursorAt(tt.before)
		if got := withCursor(s, wordStart(s, pos)); got != tt.after {
			t.Errorf("wordStart(%q) = %q, want %q", tt.before, got, tt.after)
		}
	}
}

func TestWordEnd(t *testing.T) {
	tests := []struct{ before, after string }{
		{"‸.items[0].name", ".‸items[0].name"},
		{".‸items[0].name", ".items‸[0].name"},
		{".items‸[0].name", ".items[‸0].name"},
		{".items[0‸].name", ".items[0].‸name"},
		{".items[0].‸name", ".items[0].name‸"},
		{".a‸ | .b", ".a |‸ .b"},
		{".a |‸ .b", ".a | .‸b"},
		{`.‸"名前"`, `."‸名前"`},
		{`."‸名前"`, `."名前‸"`},
		{".n‸ame", ".name‸"},
		{"‸   ", "   ‸"},
		{"‸", "‸"},
		{".a‸", ".a‸"},
	}
	for _, tt := range tests {
		s, pos := cursorAt(tt.before)
		if got := withCursor(s, wordEnd(s, pos)); got != tt.after {
			t.Errorf("wordEnd(%q) = %q, want %q", tt.before, got, tt.after)
		}
	}
}

func TestWordKeys(t *testing.T) {
	alt := func(k tea.KeyMsg) tea.KeyMsg { k.Alt = true; return k }
	tests := []struct {
		name          string
		key           tea.KeyMsg
		before, after string
	}{
		{"ctrl+w", tea.KeyMsg{Type: tea.KeyCtrlW}, ".items[0].name‸", ".items[0].‸"},
		{"ctrl+w keeps the text after the cursor", tea.KeyMsg{Type: tea.KeyCtrlW}, ".a.bc‸ + 1", ".a.‸ + 1"},
		{"alt+backspace", alt(tea.KeyMsg{Type: tea.KeyBackspace}), ".a | .b‸", ".a | .‸"},
		{"alt+d", alt(runes("d")), ".a.‸name + 1", ".a.‸ + 1"},
		{"alt+delete", alt(tea.KeyMsg{Type: tea.KeyDelete}), "‸.items", "‸items"},
		{"ctrl+left", tea.KeyMsg{Type: tea.KeyCtrlLeft}, ".items[0].name‸", ".items[0].‸name"},
		{"alt+left", alt(tea.KeyMsg{Type: tea.KeyLeft}), ".items[0].‸name", ".items[0‸].name"},
		{"alt+b", alt(runes("b")), ".items‸", ".‸items"},
		{"ctrl+right", tea.KeyMsg{Type: tea.KeyCtrlRight}, "‸.items[0]", ".‸items[0]"},
		{"alt+right", alt(tea.KeyMsg{Type: tea.KeyRight}), ".‸items[0]", ".items‸[0]"},
		{"alt+f", alt(runes("f")), ".items‸[0]", ".items[‸0]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, keysDoc())
			h.start()
			s, pos := cursorAt(tt.before)
			h.settle(h.m.edit(string(s), pos))
			h.settle(h.send(tt.key))
			want, wantPos := cursorAt(tt.after)
			h.wantInput(string(want), wantPos)
		})
	}
}

func TestCtrlWShowsCandidatesForTheKey(t *testing.T) {
	h := newHarness(t, keysDoc())
	h.start()
	h.typeText(".alpha")
	h.wantCandidates()
	h.press(tea.KeyCtrlW)
	h.wantInput(".", 1)
	h.wantCandidates("alpha", "beta", "gamma")
	// The preview is back to the whole document. The list covers its first rows.
	h.wantPane(`"gamma": 3`)
}
