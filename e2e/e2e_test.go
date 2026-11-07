//go:build e2e && windows

// Package e2e drives the real jitto binary inside a pseudo console (ConPTY): it
// sends keys, reads the screen, and checks exit codes and output files.
//
//	go test -tags e2e ./e2e         # the regular scenarios
//	go test -tags e2e ./e2e -slow   # also a timeout and a 50 MB input
//
// These tests cover what unit tests cannot reach: how the program behaves with real
// consoles, shells, pipes and redirection. They cannot cover IME input or how a
// particular terminal renders the screen.
package e2e

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/UserExistsError/conpty"
)

const (
	testVersion = "1.2.3-e2e"
	screenWait  = 15 * time.Second
	exitWait    = 15 * time.Second

	keyEnter      = "\r"
	keyCtrlC      = "\x03"
	keyBackspace  = "\b"
	keyUp         = "\x1b[A"
	keyDown       = "\x1b[B"
	keyLeft       = "\x1b[D"
	keyPgUp       = "\x1b[5~"
	keyPgDn       = "\x1b[6~"
	keyShiftRight = "\x1b[1;2C"
	keyShiftLeft  = "\x1b[1;2D"
	keyTab        = "\t"
	keyEsc        = "\x1b"
	keyCtrlN      = "\x0e"
	keyCtrlP      = "\x10"
	keyCtrlY      = "\x19"
	keyCtrlW      = "\x17"
	keyCtrlLeft   = "\x1b[1;5D"

	helpShown = "Enter: confirm"
)

var (
	slow = flag.Bool("slow", false, "also run the slow scenarios")
	// jitto is the path of the binary under test.
	jitto string
)

func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(run(m))
}

func run(m *testing.M) int {
	dir, err := os.MkdirTemp("", "jitto-e2e")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(dir)

	jitto = filepath.Join(dir, "jitto.exe")
	build := exec.Command("go", "build", "-ldflags", "-X main.version="+testVersion, "-o", jitto, "..")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build jitto: %v\n%s", err, out)
		return 1
	}

	// The scenarios decide on color themselves. See TestNoColor.
	os.Unsetenv("NO_COLOR")
	// termenv treats any CI variable as "not a terminal" and turns colors off.
	// CI services set it, but jitto runs in a real console here.
	os.Unsetenv("CI")
	return m.Run()
}

// fixture is a working directory holding the sample input.
type fixture struct {
	t   *testing.T
	dir string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t, t.TempDir()}

	items := make([]int, 100)
	for i := range items {
		items[i] = i
	}
	data, err := json.Marshal(map[string]any{
		"a":     "one",
		"名前":    "太郎",
		"long":  strings.Repeat("x", 200) + "END",
		"items": items,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.write("data.json", string(data))
	f.write("ascii.json", `{"a":"one","items":[1,2]}`)
	return f
}

func (f *fixture) write(name, content string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) read(name string) string {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, name))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(b)
}

// session is one command running in its own pseudo console.
type session struct {
	t      *testing.T
	pty    *conpty.ConPty
	scr    *screen
	exited bool
}

// start runs a command line in the fixture's directory. In the command line,
// {jitto} stands for the binary under test.
func (f *fixture) start(cmdline string) *session {
	f.t.Helper()
	cmdline = strings.ReplaceAll(cmdline, "{jitto}", jitto)
	pty, err := conpty.Start(cmdline, conpty.ConPtyDimensions(80, 24), conpty.ConPtyWorkDir(f.dir))
	if err != nil {
		f.t.Fatalf("start %s: %v", cmdline, err)
	}
	s := &session{t: f.t, pty: pty, scr: newScreen(80, 24)}
	go func() {
		buf := make([]byte, 64*1024)
		for {
			n, err := pty.Read(buf)
			if n > 0 {
				s.scr.write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	f.t.Cleanup(s.close)
	return s
}

// close ends the session, so nothing holds on to the fixture's files.
func (s *session) close() {
	if !s.exited {
		s.send(keyCtrlC)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = s.pty.Wait(ctx)
	}
	s.pty.Close()
}

func (s *session) send(keys string) {
	s.t.Helper()
	if _, err := s.pty.Write([]byte(keys)); err != nil {
		s.t.Fatalf("send %q: %v", keys, err)
	}
}

// typeKeys sends one key at a time, like a person.
func (s *session) typeKeys(keys string) {
	s.t.Helper()
	for _, r := range keys {
		s.send(string(r))
		time.Sleep(20 * time.Millisecond)
	}
}

func (s *session) repeat(key string, n int) {
	s.t.Helper()
	for range n {
		s.send(key)
		time.Sleep(10 * time.Millisecond)
	}
}

func (s *session) waitFor(what string, timeout time.Duration, cond func(screenText string) bool) {
	s.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond(s.scr.text()) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.t.Fatalf("timed out waiting: %s\n%s", what, s.scr.dump())
}

func (s *session) see(text string) {
	s.t.Helper()
	s.waitFor("screen shows "+strconv.Quote(text), screenWait, func(t string) bool {
		return strings.Contains(t, text)
	})
}

func (s *session) gone(text string) {
	s.t.Helper()
	s.waitFor("screen stops showing "+strconv.Quote(text), screenWait, func(t string) bool {
		return !strings.Contains(t, text)
	})
}

func (s *session) wantExit(code int) {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), exitWait)
	defer cancel()
	got, err := s.pty.Wait(ctx)
	if err != nil {
		s.t.Fatalf("did not exit (want code %d): %v\n%s", code, err, s.scr.dump())
	}
	s.exited = true
	if int(got) != code {
		s.t.Errorf("exit code %d, want %d\n%s", got, code, s.scr.dump())
	}
	time.Sleep(200 * time.Millisecond) // Let the last output arrive.
}

func (s *session) wantRunningAfter(d time.Duration) {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	if code, err := s.pty.Wait(ctx); err == nil {
		s.exited = true
		s.t.Fatalf("exited with code %d, want it to keep running\n%s", code, s.scr.dump())
	}
}

func (s *session) wantConsoleContains(text string) {
	s.t.Helper()
	if !strings.Contains(s.scr.text(), text) {
		s.t.Errorf("console does not contain %q\n%s", text, s.scr.dump())
	}
}

func (s *session) wantConsoleLacks(text string) {
	s.t.Helper()
	if strings.Contains(s.scr.text(), text) {
		s.t.Errorf("console contains %q\n%s", text, s.scr.dump())
	}
}

// showingDoc waits for the first result of the sample input.
func (s *session) showingDoc() {
	s.t.Helper()
	s.see(`"a": "one"`)
}

// selectA narrows the sample input to `"one"`.
func (s *session) selectA() {
	s.t.Helper()
	s.showingDoc()
	s.typeKeys(".a")
	s.gone(`"a": "one"`)
	s.see(`"one"`)
}

func needCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s is not installed", name)
	}
}

func needSlow(t *testing.T) {
	t.Helper()
	if !*slow {
		t.Skip("slow scenario: run with -slow")
	}
}

func decodeUTF16LE(t *testing.T, s string) string {
	t.Helper()
	if !strings.HasPrefix(s, "\xff\xfe") {
		t.Fatalf("not UTF-16 LE with a BOM: %q", s)
	}
	b := []byte(s[2:])
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return string(utf16.Decode(u))
}

func TestFileArgument(t *testing.T) {
	f := newFixture(t)
	s := f.start(`cmd /c ""{jitto}" data.json > out.txt"`)
	s.selectA()
	s.see(helpShown)
	s.send(keyEnter)
	s.wantExit(0)

	if got := f.read("out.txt"); got != "\"one\"\n" {
		t.Errorf("stdout is %q", got)
	}
	s.wantConsoleContains(".a")
	s.wantConsoleLacks(helpShown)
}

func TestCmdPipeWithJapanese(t *testing.T) {
	f := newFixture(t)
	s := f.start(`cmd /c "type data.json | "{jitto}" > out.txt"`)
	s.showingDoc()
	s.typeKeys(`."名前"`)
	s.see(`"太郎"`)
	s.gone(`"a": "one"`)
	s.send(keyEnter)
	s.wantExit(0)

	if got := f.read("out.txt"); got != "\"太郎\"\n" {
		t.Errorf("stdout is %q", got)
	}
	s.wantConsoleContains(`."名前"`)
}

func TestUnquotedJapaneseKeyGetsAHint(t *testing.T) {
	f := newFixture(t)
	s := f.start(`"{jitto}" data.json`)
	s.showingDoc()
	s.typeKeys(".名前")
	s.see(`(try ."名前")`)
	s.see(`"a": "one"`) // The last good result stays.
}

func TestCmdPipeWithStdoutOnConsole(t *testing.T) {
	f := newFixture(t)
	s := f.start(`cmd /c "type data.json | "{jitto}""`)
	s.selectA()
	s.send(keyEnter)
	s.wantExit(0)
	s.wantConsoleContains(`"one"`)
	s.wantConsoleContains(".a")
	s.wantConsoleLacks(helpShown)
}

// Bubble Tea hangs on exit if it opens the console itself, which it does whenever
// standard input is not a console, even if the input came from a file.
func TestPipedStdinIsIgnoredWithFileArgument(t *testing.T) {
	f := newFixture(t)
	s := f.start(`cmd /c "type ascii.json | "{jitto}" data.json > out.txt"`)
	s.showingDoc()
	s.see("  18,") // Only the file has this many items.
	s.typeKeys(".a")
	s.gone(`"items"`)
	s.send(keyEnter)
	s.wantExit(0)
	if got := f.read("out.txt"); got != "\"one\"\n" {
		t.Errorf("stdout is %q", got)
	}
}

func TestPowerShell51(t *testing.T) {
	needCommand(t, "powershell")

	t.Run("pipe and redirect", func(t *testing.T) {
		f := newFixture(t)
		s := f.start(`powershell -NoProfile -Command "Get-Content ascii.json | & '{jitto}' > out.txt; exit $LASTEXITCODE"`)
		s.selectA()
		s.send(keyEnter)
		s.wantExit(0)
		if got := decodeUTF16LE(t, f.read("out.txt")); !strings.Contains(got, `"one"`) {
			t.Errorf("stdout is %q", got)
		}
	})

	// ">" writes UTF-16 LE with a byte order mark.
	t.Run("file saved with >", func(t *testing.T) {
		f := newFixture(t)
		s := f.start(`powershell -NoProfile -Command "Get-Content -Raw -Encoding UTF8 data.json > saved.json; & '{jitto}' saved.json; exit $LASTEXITCODE"`)
		s.showingDoc()
		s.typeKeys(`."名前"`)
		s.see(`"太郎"`)
		s.send(keyEnter)
		s.wantExit(0)
		if got := decodeUTF16LE(t, f.read("saved.json")); !strings.Contains(got, "太郎") {
			t.Fatalf("saved file is %q", got)
		}
	})

	// With the UTF-8 code page and this setting, 5.1 starts the pipe with two UTF-8
	// byte order marks.
	t.Run("two byte order marks", func(t *testing.T) {
		f := newFixture(t)
		s := f.start(`powershell -NoProfile -Command "chcp 65001 > $null; $OutputEncoding = [Text.Encoding]::UTF8; Get-Content -Raw -Encoding UTF8 data.json | ConvertFrom-Json | ConvertTo-Json | & '{jitto}' > out.txt; exit $LASTEXITCODE"`)
		s.showingDoc()
		s.typeKeys(`."名前"`)
		s.see(`"太郎"`)
		s.send(keyEnter)
		s.wantExit(0)
	})

	// The workaround the README gives for Japanese text.
	t.Run("README workaround keeps Japanese", func(t *testing.T) {
		f := newFixture(t)
		s := f.start(`powershell -NoProfile -Command "$OutputEncoding = [Console]::OutputEncoding = [Text.Encoding]::UTF8; Get-Content -Encoding UTF8 data.json | & '{jitto}' | Set-Content -Encoding UTF8 out.txt; exit $LASTEXITCODE"`)
		s.showingDoc()
		s.typeKeys(`."名前"`)
		s.see(`"太郎"`)
		s.send(keyEnter)
		s.wantExit(0)
		if got := f.read("out.txt"); !strings.Contains(got, "太郎") {
			t.Errorf("stdout is %q", got)
		}
	})
}

func TestPowerShell7(t *testing.T) {
	needCommand(t, "pwsh")

	t.Run("pipe keeps Japanese", func(t *testing.T) {
		f := newFixture(t)
		s := f.start(`pwsh -NoProfile -Command "Get-Content data.json | & '{jitto}' > out.txt; exit $LASTEXITCODE"`)
		s.showingDoc()
		s.typeKeys(`."名前"`)
		s.see(`"太郎"`)
		s.send(keyEnter)
		s.wantExit(0)
	})

	t.Run("pipe to the next command", func(t *testing.T) {
		f := newFixture(t)
		s := f.start(`pwsh -NoProfile -Command "& '{jitto}' data.json | Out-File -Encoding utf8 out.txt; exit $LASTEXITCODE"`)
		s.selectA()
		s.send(keyEnter)
		s.wantExit(0)
		if got := f.read("out.txt"); !strings.Contains(got, `"one"`) {
			t.Errorf("stdout is %q", got)
		}
		s.wantConsoleContains(".a")
	})

	t.Run("capture in a variable", func(t *testing.T) {
		f := newFixture(t)
		s := f.start(`pwsh -NoProfile -Command "$r = & '{jitto}' data.json; Set-Content -Path out.txt -Value $r; exit $LASTEXITCODE"`)
		s.selectA()
		s.send(keyEnter)
		s.wantExit(0)
		if got := f.read("out.txt"); !strings.Contains(got, `"one"`) {
			t.Errorf("variable holds %q", got)
		}
		s.wantConsoleContains(".a")
	})
}

func TestCtrlCPrintsNothing(t *testing.T) {
	f := newFixture(t)
	s := f.start(`cmd /c "type data.json | "{jitto}" > out.txt"`)
	s.selectA()
	s.send(keyCtrlC)
	s.wantExit(130)
	if got := f.read("out.txt"); got != "" {
		t.Errorf("stdout is %q", got)
	}
	s.wantConsoleLacks(".a")
}

func TestErrorKeepsResultAndEnterDoesNotConfirm(t *testing.T) {
	f := newFixture(t)
	s := f.start(`cmd /c ""{jitto}" data.json > out.txt"`)
	s.selectA()

	const failing = " | keys"
	s.typeKeys(failing)
	s.see("keys cannot be applied")
	s.see(`"one"`)

	s.send(keyEnter)
	s.wantRunningAfter(500 * time.Millisecond)
	s.see("keys cannot be applied")

	s.repeat(keyBackspace, len(failing))
	s.see(helpShown)
	s.send(keyEnter)
	s.wantExit(0)
	if got := f.read("out.txt"); got != "\"one\"\n" {
		t.Errorf("stdout is %q", got)
	}
}

func TestEnterRightAfterTypingUsesTheLatestExpression(t *testing.T) {
	f := newFixture(t)
	s := f.start(`cmd /c ""{jitto}" data.json > out.txt"`)
	s.showingDoc()
	s.send(".a" + keyEnter)
	s.wantExit(0)
	if got := f.read("out.txt"); got != "\"one\"\n" {
		t.Errorf("stdout is %q", got)
	}
}

func TestEnterOnTruncatedPreviewOutputsEverything(t *testing.T) {
	f := newFixture(t)
	s := f.start(`cmd /c ""{jitto}" data.json > out.txt"`)
	s.showingDoc()
	s.typeKeys("range(400000)")
	s.see("preview limited to 1 MiB")
	s.send(keyEnter)
	s.wantExit(0)
	if got := f.read("out.txt"); !strings.HasSuffix(got, "\n399999\n") {
		t.Errorf("stdout is incomplete: %d bytes", len(got))
	}
}

func TestScrollKeys(t *testing.T) {
	f := newFixture(t)
	s := f.start(`"{jitto}" data.json`)
	s.showingDoc()
	s.typeKeys(".items")
	s.see("  20,")
	s.gone(`"a": "one"`)

	s.send(keyPgDn)
	s.see("  42,")
	s.gone("  20,\n")
	s.send(keyUp)
	s.see("  20,")
	s.send(keyDown)
	s.gone("  20,\n")
	s.send(keyPgUp)
	s.see("  0,")
}

func TestPanKeys(t *testing.T) {
	f := newFixture(t)
	s := f.start(`"{jitto}" data.json`)
	s.showingDoc()
	s.typeKeys(".long")
	s.see(`"xxxx`)
	s.wantConsoleLacks("END")

	s.repeat(keyShiftRight, 20)
	s.see(`END"`)
	s.repeat(keyShiftLeft, 20)
	s.gone(`END"`)

	// Plain Left belongs to the input.
	s.send(keyLeft)
	s.typeKeys("Q")
	s.see(".lonQg")
}

func TestResize(t *testing.T) {
	f := newFixture(t)
	s := f.start(`"{jitto}" data.json`)
	s.showingDoc()
	s.typeKeys(".long")
	s.see(`"xxxx`)

	rowWidth := func() int { return len(s.scr.rows()[2]) }
	s.waitFor("line fills 80 columns", screenWait, func(string) bool { return rowWidth() == 80 })
	s.scr.resize(110, 30)
	if err := s.pty.Resize(110, 30); err != nil {
		t.Fatal(err)
	}
	s.waitFor("line fills 110 columns", screenWait, func(string) bool { return rowWidth() == 110 })
}

// showingCandidates types "." and waits for the keys of the sample input to be
// listed over the top rows of the pane, below the expression.
func (s *session) showingCandidates() {
	s.t.Helper()
	s.showingDoc()
	s.typeKeys(".")
	want := []string{"{  a", "   items", "   long", `   "名前"`}
	s.waitFor("candidates are listed", screenWait, func(string) bool {
		rows := s.scr.rows()
		for i, prefix := range want {
			if !strings.HasPrefix(rows[2+i], prefix) {
				return false
			}
		}
		return true
	})
}

// waitSelected waits for the candidate on the given row of the list to be the
// only one in reverse video. -1 means none.
func (s *session) waitSelected(row int) {
	s.t.Helper()
	s.waitFor(fmt.Sprintf("candidate %d is selected", row), screenWait, func(string) bool {
		for i := range 4 {
			if reverse, _ := s.scr.styled(2 + i); reverse != (i == row) {
				return false
			}
		}
		return true
	})
}

func TestCompletion(t *testing.T) {
	f := newFixture(t)
	s := f.start(`cmd /c ""{jitto}" data.json > out.txt"`)
	s.showingCandidates()
	s.waitSelected(-1)

	// Ctrl+N writes each candidate into the input, and the pane previews it.
	s.send(keyCtrlN)
	s.waitSelected(0)
	s.see("> .a\n")
	s.send(keyCtrlN)
	s.waitSelected(1)
	s.see("> .items\n")
	// The list covers the first rows of the result, so look below it.
	s.waitFor("preview of .items", screenWait, func(string) bool { return s.scr.rows()[7] == "  4," })

	// Ctrl+P goes back through the first candidate to the typed text.
	s.send(keyCtrlP)
	s.send(keyCtrlP)
	s.waitSelected(-1)
	s.see("> .\n")

	s.send(keyCtrlN)
	s.send(keyCtrlN)
	s.waitSelected(1)
	s.send(keyTab)
	s.gone("   long")
	s.see("> .items\n")
	s.send(keyEnter)
	s.wantExit(0)
	if got := f.read("out.txt"); !strings.HasPrefix(got, "[\n  0,\n  1,\n") {
		t.Errorf("stdout is %q", got)
	}
	s.wantConsoleContains(".items")
}

// Ctrl+Y and quoting, with the keys read from the console that replaces the pipe.
func TestCompletionWithPipedInput(t *testing.T) {
	f := newFixture(t)
	s := f.start(`cmd /c "type data.json | "{jitto}" > out.txt"`)
	s.showingCandidates()
	s.send(keyCtrlP) // Wraps around to the last candidate.
	s.waitSelected(3)
	s.send(keyCtrlY)
	s.see(`> ."名前"`)
	s.see(`"太郎"`)
	s.gone(`"a": "one"`)
	s.send(keyEnter)
	s.wantExit(0)
	if got := f.read("out.txt"); got != "\"太郎\"\n" {
		t.Errorf("stdout is %q", got)
	}
}

func TestEscClosesCandidates(t *testing.T) {
	f := newFixture(t)
	s := f.start(`"{jitto}" data.json`)
	s.showingCandidates()
	s.send(keyEsc)
	s.gone("   items")
	s.send(keyTab) // Inserts nothing while the list is closed.
	s.typeKeys("i")
	s.see(" items")
	s.see("> .i\n")
}

// The word keys stop at symbols, not only at spaces.
func TestWordKeys(t *testing.T) {
	f := newFixture(t)
	s := f.start(`"{jitto}" data.json`)
	s.showingDoc()
	s.typeKeys(".items[0].x")
	s.see("> .items[0].x")
	s.send(keyCtrlW)
	s.see("> .items[0].\n")
	s.send(keyCtrlW)
	s.see("> .items[0\n")
	s.send(keyCtrlLeft)
	s.typeKeys("Q")
	s.see("> .items[Q0")
}

func TestCommandLine(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		code int
		want string
	}{
		{"no input at the console", `"{jitto}"`, 2, "Usage:"},
		{"help", `"{jitto}" --help`, 0, "Usage:"},
		{"version", `"{jitto}" --version`, 0, "jitto " + testVersion},
		{"short version", `"{jitto}" -V`, 0, "jitto " + testVersion},
		{"unknown option", `"{jitto}" -r data.json`, 2, "unknown option -r"},
		{"too many arguments", `"{jitto}" a b`, 2, "too many arguments"},
		{"missing file", `"{jitto}" missing.json`, 1, "missing.json"},
		{"invalid JSON in a file", `"{jitto}" bad.json`, 1, "invalid JSON"},
		{"invalid JSON on stdin", `cmd /c "type bad.json | "{jitto}""`, 1, "invalid JSON"},
		{"odd UTF-16 file", `"{jitto}" utf16.json`, 1, "odd number of bytes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.write("bad.json", `{"a":}`)
			f.write("utf16.json", "\xff\xfe1\x00\x00")
			s := f.start(tt.cmd)
			s.wantExit(tt.code)
			s.wantConsoleContains(tt.want)
		})
	}
}

func TestStderrMustBeATerminal(t *testing.T) {
	f := newFixture(t)
	s := f.start(`cmd /c ""{jitto}" data.json 2> err.txt"`)
	s.wantExit(1)
	if got := f.read("err.txt"); !strings.Contains(got, "stderr must be a terminal") {
		t.Errorf("stderr is %q", got)
	}
}

func TestUTF16File(t *testing.T) {
	f := newFixture(t)
	units := utf16.Encode(append([]rune{0xFEFF}, []rune(`{"名前":"太郎"}`)...))
	b := make([]byte, 0, 2*len(units))
	for _, u := range units {
		b = append(b, byte(u), byte(u>>8))
	}
	f.write("utf16.json", string(b))

	s := f.start(`cmd /c ""{jitto}" utf16.json > out.txt"`)
	s.see(`"名前": "太郎"`)
	s.typeKeys(`."名前"`)
	s.gone(`"名前": "太郎"`)
	s.send(keyEnter)
	s.wantExit(0)
	if got := f.read("out.txt"); got != "\"太郎\"\n" {
		t.Errorf("stdout is %q", got)
	}
}

func TestByteOrderMarkIsAccepted(t *testing.T) {
	f := newFixture(t)
	f.write("bom.json", "\xef\xbb\xbf"+`{"a":"one"}`)
	s := f.start(`"{jitto}" bom.json`)
	s.showingDoc()
}

func TestEmptyStdinIsNotAnError(t *testing.T) {
	f := newFixture(t)
	s := f.start(`cmd /c "type nul | "{jitto}" > out.txt"`)
	s.see(helpShown)
	s.send(keyEnter)
	s.wantExit(0)
	if got := f.read("out.txt"); got != "" {
		t.Errorf("stdout is %q", got)
	}
}

// The screen is drawn on stderr, so redirecting stdout must not affect its styles.
func TestStylesWithStdoutRedirected(t *testing.T) {
	f := newFixture(t)
	s := f.start(`cmd /c "type data.json | "{jitto}" > out.txt"`)
	s.showingDoc()
	s.see(helpShown)
	if reverse, _ := s.scr.styled(0); !reverse {
		t.Error("cursor is not visible")
	}
	if _, color := s.scr.styled(1); !color {
		t.Error("status line has no color")
	}
}

func TestNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	f := newFixture(t)
	s := f.start(`cmd /c ""{jitto}" data.json > out.txt"`)
	s.showingDoc()
	s.see(helpShown)
	if reverse, _ := s.scr.styled(0); !reverse {
		t.Error("cursor is not visible")
	}
	if _, color := s.scr.styled(1); color {
		t.Error("status line has color despite NO_COLOR")
	}
}

func TestRunawayExpression(t *testing.T) {
	needSlow(t)
	f := newFixture(t)
	s := f.start(`cmd /c ""{jitto}" data.json > out.txt"`)
	s.showingDoc()
	s.typeKeys("def f: f; f")
	s.see("evaluating...")
	s.see("timed out after 5s")
	s.see(`"a": "one"`)

	s.send(keyEnter)
	s.see("evaluating all output")
	s.typeKeys(" ") // Editing stops the evaluation.
	s.gone("evaluating all output")

	s.send(keyEnter)
	s.see("evaluating all output")
	s.send(keyCtrlC)
	s.wantExit(130)
	if got := f.read("out.txt"); got != "" {
		t.Errorf("stdout is %q", got)
	}
}

func TestLargeInput(t *testing.T) {
	needSlow(t)
	f := newFixture(t)

	const records = 500_000
	var b strings.Builder
	b.WriteString("[")
	for i := range records {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"id":%d,"name":"user-%d","email":"user%d@example.com","tags":["a","b","c"],"active":true}`, i, i, i)
	}
	b.WriteString("]\n")
	f.write("large.json", b.String())
	t.Logf("input is %d MB", b.Len()/1_000_000)

	start := time.Now()
	s := f.start(`cmd /c ""{jitto}" large.json > out.txt"`)
	s.waitFor("first result", time.Minute, func(text string) bool { return strings.Contains(text, `"id": 0`) })
	t.Logf("first result after %v", time.Since(start).Round(time.Millisecond))

	start = time.Now()
	s.typeKeys(".[].id")
	s.see("preview limited to 1 MiB")
	t.Logf("preview after %v", time.Since(start).Round(time.Millisecond))

	start = time.Now()
	s.send(keyEnter)
	s.wantExit(0)
	t.Logf("all output after %v", time.Since(start).Round(time.Millisecond))
	if got := f.read("out.txt"); !strings.HasSuffix(got, fmt.Sprintf("\n%d\n", records-1)) {
		t.Errorf("stdout is incomplete: %d bytes", len(got))
	}
}
