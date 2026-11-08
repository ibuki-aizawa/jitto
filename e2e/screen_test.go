//go:build e2e && windows

package e2e

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// cell is one terminal cell. r == 0 marks the second half of a wide character.
type cell struct {
	r       rune
	reverse bool
	color   bool
}

// screen is a small VT emulator, just enough for what ConPTY emits. ConPTY sends
// only what changed, so the raw stream does not show what is on screen.
type screen struct {
	mu       sync.Mutex
	w, h     int
	grid     [][]cell
	row, col int
	reverse  bool
	color    bool
	pending  []byte
	// head is the start of the raw output, for failure messages.
	head []byte
	// replies holds answers to queries from the console, such as the cursor
	// position, which a real terminal would send back.
	replies []byte
}

func newScreen(w, h int) *screen {
	s := &screen{w: w, h: h}
	s.clear()
	return s
}

func blankRow(w int) []cell {
	row := make([]cell, w)
	for i := range row {
		row[i].r = ' '
	}
	return row
}

func (s *screen) clear() {
	s.grid = make([][]cell, s.h)
	for i := range s.grid {
		s.grid[i] = blankRow(s.w)
	}
}

// resize starts over with an empty screen. ConPTY redraws after a resize.
func (s *screen) resize(w, h int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.w, s.h = w, h
	s.row, s.col = 0, 0
	s.clear()
}

// write takes output from the console and returns the replies to send back to it.
func (s *screen) write(b []byte) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n := headSize - len(s.head); n > 0 {
		s.head = append(s.head, b[:min(n, len(b))]...)
	}
	s.pending = append(s.pending, b...)
	for len(s.pending) > 0 {
		n := s.step(s.pending)
		if n == 0 {
			break
		}
		s.pending = s.pending[n:]
	}
	replies := s.replies
	s.replies = nil
	return replies
}

// headSize is how much of the raw output rawHead keeps.
const headSize = 512

// rawHead returns the start of the raw output, quoted.
func (s *screen) rawHead() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strconv.Quote(string(s.head))
}

// step consumes one token and returns its length, or 0 if it is incomplete.
func (s *screen) step(b []byte) int {
	switch b[0] {
	case 0x1b:
		return s.escape(b)
	case '\r':
		s.col = 0
		return 1
	case '\n':
		s.lineFeed()
		return 1
	case '\b':
		s.col = max(s.col-1, 0)
		return 1
	case 0x07:
		return 1
	}
	if !utf8.FullRune(b) {
		return 0
	}
	r, n := utf8.DecodeRune(b)
	s.print(r)
	return n
}

func (s *screen) lineFeed() {
	if s.row < s.h-1 {
		s.row++
		return
	}
	copy(s.grid, s.grid[1:])
	s.grid[s.h-1] = blankRow(s.w)
}

func (s *screen) print(r rune) {
	width := runewidth.RuneWidth(r)
	if width == 0 {
		return
	}
	if s.col+width > s.w {
		s.col = 0
		s.lineFeed()
	}
	s.grid[s.row][s.col] = cell{r, s.reverse, s.color}
	if width == 2 {
		s.grid[s.row][s.col+1] = cell{0, s.reverse, s.color}
	}
	s.col += width
}

func (s *screen) escape(b []byte) int {
	if len(b) < 2 {
		return 0
	}
	switch b[1] {
	case '[':
		for i := 2; i < len(b); i++ {
			if b[i] >= 0x40 && b[i] <= 0x7e {
				s.csi(string(b[2:i]), b[i])
				return i + 1
			}
		}
		return 0
	case ']':
		for i := 2; i < len(b); i++ {
			if b[i] == 0x07 {
				return i + 1
			}
			if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '\\' {
				return i + 2
			}
		}
		return 0
	}
	return 2
}

func (s *screen) csi(params string, final byte) {
	if strings.HasPrefix(params, "?") {
		return // Private modes. ConPTY redraws after leaving the alternate screen.
	}
	var nums []int
	for _, p := range strings.Split(params, ";") {
		n, _ := strconv.Atoi(p)
		nums = append(nums, n)
	}
	arg := func(i, def int) int {
		if i < len(nums) && nums[i] != 0 {
			return nums[i]
		}
		return def
	}
	switch final {
	case 'n': // Device status report.
		if params == "6" {
			s.replies = append(s.replies, fmt.Sprintf("\x1b[%d;%dR", s.row+1, s.col+1)...)
		}
	case 'c': // Primary device attributes: answer as a VT100.
		if params == "" || params == "0" {
			s.replies = append(s.replies, "\x1b[?1;0c"...)
		}
	case 'H', 'f':
		s.row, s.col = arg(0, 1)-1, arg(1, 1)-1
	case 'A':
		s.row -= arg(0, 1)
	case 'B':
		s.row += arg(0, 1)
	case 'C':
		s.col += arg(0, 1)
	case 'D':
		s.col -= arg(0, 1)
	case 'G':
		s.col = arg(0, 1) - 1
	case 'd':
		s.row = arg(0, 1) - 1
	case 'X':
		s.blank(s.row, s.col, s.col+arg(0, 1))
	case 'K':
		switch arg(0, 0) {
		case 1:
			s.blank(s.row, 0, s.col+1)
		case 2:
			s.blank(s.row, 0, s.w)
		default:
			s.blank(s.row, s.col, s.w)
		}
	case 'J':
		if mode := arg(0, 0); mode == 2 || mode == 3 {
			s.clear()
			return
		}
		s.blank(s.row, s.col, s.w)
		for i := s.row + 1; i < s.h; i++ {
			s.grid[i] = blankRow(s.w)
		}
	case 'm':
		s.sgr(nums)
	}
	s.row = max(0, min(s.row, s.h-1))
	s.col = max(0, min(s.col, s.w-1))
}

func (s *screen) blank(row, from, to int) {
	for i := from; i < to && i < s.w; i++ {
		s.grid[row][i] = cell{r: ' '}
	}
}

func (s *screen) sgr(nums []int) {
	for i := 0; i < len(nums); i++ {
		switch n := nums[i]; {
		case n == 0:
			s.reverse, s.color = false, false
		case n == 7:
			s.reverse = true
		case n == 27:
			s.reverse = false
		case n == 39:
			s.color = false
		case n == 38, n == 48:
			if i+1 < len(nums) && nums[i+1] == 5 {
				i += 2
			} else if i+1 < len(nums) && nums[i+1] == 2 {
				i += 4
			}
			if n == 38 {
				s.color = true
			}
		case n >= 30 && n <= 37, n >= 90 && n <= 97:
			s.color = true
		}
	}
}

// rows returns the text of each row, without trailing blanks.
func (s *screen) rows() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, s.h)
	for i, row := range s.grid {
		var b strings.Builder
		for _, c := range row {
			if c.r != 0 {
				b.WriteRune(c.r)
			}
		}
		out[i] = strings.TrimRight(b.String(), " ")
	}
	return out
}

func (s *screen) text() string { return strings.Join(s.rows(), "\n") }

// styled reports whether row i has any reverse video cell and any colored cell.
func (s *screen) styled(i int) (reverse, color bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.grid[i] {
		reverse = reverse || c.reverse
		color = color || c.color
	}
	return reverse, color
}

// dump is a picture of the screen for failure messages.
func (s *screen) dump() string {
	rows := s.rows()
	last := len(rows)
	for last > 0 && rows[last-1] == "" {
		last--
	}
	var b strings.Builder
	for _, r := range rows[:last] {
		b.WriteString("    |" + r + "\n")
	}
	return b.String()
}
