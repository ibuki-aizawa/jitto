// Command jitto is an interactive jq: type an expression and see its result as you
// type. On Enter, the result goes to stdout and the expression to stderr.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/ibuki-aizawa/jitto/internal/eval"
	"github.com/ibuki-aizawa/jitto/internal/highlight"
	"github.com/ibuki-aizawa/jitto/internal/input"
	"github.com/ibuki-aizawa/jitto/internal/tui"
)

const usage = `Usage:
  jitto FILE
  COMMAND | jitto

Type a jq expression to see its result as you type.

Options:
  -r, --raw-output   write strings without quotes, like jq -r
  --encoding NAME    encoding of input without a byte order mark: utf-8
                     (the default) or shift_jis (also sjis, cp932)
  -h, --help         show this help
  -V, --version      show the version

Keys:
  Enter              print the result to stdout and the expression to stderr
  Ctrl+C             quit without printing anything
  Ctrl+T             show example expressions for the result
  Alt+R              switch -r on or off
  Up, Down           scroll the result
  PgUp, PgDn         scroll the result by a page
  Shift+Left, Right  pan the result sideways
`

const (
	exitOK       = 0
	exitFailure  = 1
	exitUsage    = 2
	exitCanceled = 130
)

// version is set at release time with -ldflags "-X main.version=1.2.3".
var version string

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	opts, err := parseArgs(args)
	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, "jitto: %v\n\n%s", err, usage)
		return exitUsage
	case opts.help:
		fmt.Print(usage)
		return exitOK
	case opts.version:
		fmt.Println("jitto", currentVersion())
		return exitOK
	case opts.path == "" && isTerminal(os.Stdin):
		fmt.Fprint(os.Stderr, usage)
		return exitUsage
	}

	if !isTerminal(os.Stderr) {
		return fail(errors.New("stderr must be a terminal, because the screen is drawn there"))
	}

	docs, notice, err := load(opts.path, opts.encoding)
	if err != nil {
		return fail(withInputHint(err))
	}
	if err := useConsoleInput(); err != nil {
		return fail(fmt.Errorf("open console input: %w", err))
	}

	// Decide on colors from stderr, where the screen is drawn. The default renderer
	// looks at stdout, which is typically redirected.
	lipgloss.SetDefaultRenderer(lipgloss.NewRenderer(os.Stderr))

	model := tui.New(docs)
	if notice != nil {
		model.SetNotice(notice.Error())
	}
	model.SetRaw(opts.raw)
	final, err := tea.NewProgram(model, tea.WithOutput(os.Stderr), tea.WithAltScreen()).Run()
	switch {
	case errors.Is(err, tea.ErrInterrupted):
		return exitCanceled
	case err != nil:
		return fail(err)
	}

	res, expr, ok := final.(*tui.Model).Outcome()
	if !ok {
		return exitCanceled
	}
	if err := writeResult(os.Stdout, res); err != nil {
		return fail(err)
	}
	fmt.Fprintln(os.Stderr, expr)
	return exitOK
}

// writeResult writes the output of res to w, colored like jq when w is a terminal
// with colors. A pipe or a file gets plain JSON, and so does a terminal with
// NO_COLOR set. Raw strings (jq -r) are written as they are, without colors.
func writeResult(w *os.File, res eval.Result) error {
	output := res.Output
	out := termenv.NewOutput(w)
	if out.EnvColorProfile() != termenv.Ascii {
		// Bubble Tea restores the console mode on exit, and a console without
		// virtual terminal processing would show the escape sequences as text. If
		// it cannot be turned on, the result stays plain.
		if restore, err := termenv.EnableVirtualTerminalProcessing(out); err == nil {
			defer restore()
			output = colorResult(res)
		}
	}
	_, err := io.WriteString(w, output)
	return err
}

// colorResult returns the output of res with its JSON lines colored. The output
// of a complete evaluation ends each line with a newline.
func colorResult(res eval.Result) string {
	var b strings.Builder
	b.Grow(len(res.Output) + len(res.Output)/2)
	for line, raw := range res.Lines() {
		if raw {
			b.WriteString(line)
		} else {
			b.WriteString(highlight.ColorLine(line))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// options is the parsed command line.
type options struct {
	// path is the input file, or "" for standard input.
	path     string
	encoding input.Encoding
	// raw starts with jq -r on.
	raw     bool
	help    bool
	version bool
}

func parseArgs(args []string) (options, error) {
	var opts options
	var paths []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			return options{help: true}, nil
		case arg == "-V" || arg == "--version":
			return options{version: true}, nil
		case arg == "-r" || arg == "--raw-output":
			opts.raw = true
		case arg == "--encoding" || strings.HasPrefix(arg, "--encoding="):
			name, ok := strings.CutPrefix(arg, "--encoding=")
			if !ok {
				if i+1 == len(args) {
					return options{}, errors.New("--encoding needs a value")
				}
				i++
				name = args[i]
			}
			enc, err := input.ParseEncoding(name)
			if err != nil {
				return options{}, err
			}
			opts.encoding = enc
		case strings.HasPrefix(arg, "-") && arg != "-":
			return options{}, fmt.Errorf("unknown option %s", arg)
		default:
			paths = append(paths, arg)
		}
	}
	switch len(paths) {
	case 0:
	case 1:
		opts.path = paths[0]
	default:
		return options{}, errors.New("too many arguments")
	}
	return opts, nil
}

func currentVersion() string {
	var built string
	if info, ok := debug.ReadBuildInfo(); ok {
		built = info.Main.Version
	}
	return resolveVersion(version, built)
}

// resolveVersion prefers the version set at release time, then the one recorded by
// the Go toolchain, which is how "go install" and plain "go build" report theirs.
func resolveVersion(injected, built string) string {
	v := injected
	if v == "" {
		v = built
	}
	if v == "" || v == "(devel)" {
		return "dev"
	}
	return strings.TrimPrefix(v, "v")
}

func load(path string, enc input.Encoding) (docs []any, notice, err error) {
	if path == "" {
		return input.Read(os.Stdin, enc)
	}
	return input.ReadFile(path, enc)
}

// isTerminal reports whether f is a character device, which is what a console is.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func fail(err error) int {
	fmt.Fprintf(os.Stderr, "jitto: %v\n", err)
	return exitFailure
}
