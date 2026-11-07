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

	"github.com/ibuki-aizawa/jitto/internal/input"
	"github.com/ibuki-aizawa/jitto/internal/tui"
)

const usage = `Usage:
  jitto FILE
  COMMAND | jitto

Type a jq expression to see its result as you type.

Options:
  -h, --help         show this help
  -V, --version      show the version

Keys:
  Enter              print the result to stdout and the expression to stderr
  Ctrl+C             quit without printing anything
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

	docs, err := load(opts.path)
	if err != nil {
		return fail(err)
	}
	if err := useConsoleInput(); err != nil {
		return fail(fmt.Errorf("open console input: %w", err))
	}

	// Decide on colors from stderr, where the screen is drawn. The default renderer
	// looks at stdout, which is typically redirected.
	lipgloss.SetDefaultRenderer(lipgloss.NewRenderer(os.Stderr))

	final, err := tea.NewProgram(tui.New(docs), tea.WithOutput(os.Stderr), tea.WithAltScreen()).Run()
	switch {
	case errors.Is(err, tea.ErrInterrupted):
		return exitCanceled
	case err != nil:
		return fail(err)
	}

	output, expr, ok := final.(*tui.Model).Outcome()
	if !ok {
		return exitCanceled
	}
	if _, err := io.WriteString(os.Stdout, output); err != nil {
		return fail(err)
	}
	fmt.Fprintln(os.Stderr, expr)
	return exitOK
}

// options is the parsed command line.
type options struct {
	// path is the input file, or "" for standard input.
	path    string
	help    bool
	version bool
}

func parseArgs(args []string) (options, error) {
	for _, arg := range args {
		switch {
		case arg == "-h" || arg == "--help":
			return options{help: true}, nil
		case arg == "-V" || arg == "--version":
			return options{version: true}, nil
		case strings.HasPrefix(arg, "-") && arg != "-":
			return options{}, fmt.Errorf("unknown option %s", arg)
		}
	}
	switch len(args) {
	case 0:
		return options{}, nil
	case 1:
		return options{path: args[0]}, nil
	}
	return options{}, errors.New("too many arguments")
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

func load(path string) ([]any, error) {
	if path == "" {
		return input.Read(os.Stdin)
	}
	return input.ReadFile(path)
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
