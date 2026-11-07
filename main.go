// Command jitto is an interactive jq: type an expression and see its result as you
// type. On Enter, the result goes to stdout and the expression to stderr.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
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

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	path, help, err := parseArgs(args)
	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, "jitto: %v\n\n%s", err, usage)
		return exitUsage
	case help:
		fmt.Print(usage)
		return exitOK
	case path == "" && isTerminal(os.Stdin):
		fmt.Fprint(os.Stderr, usage)
		return exitUsage
	}

	if !isTerminal(os.Stderr) {
		return fail(errors.New("stderr must be a terminal, because the screen is drawn there"))
	}

	docs, err := load(path)
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

// parseArgs returns the input file, or "" for standard input.
func parseArgs(args []string) (path string, help bool, err error) {
	for _, arg := range args {
		switch {
		case arg == "-h" || arg == "--help":
			return "", true, nil
		case strings.HasPrefix(arg, "-") && arg != "-":
			return "", false, fmt.Errorf("unknown option %s", arg)
		}
	}
	switch len(args) {
	case 0:
		return "", false, nil
	case 1:
		return args[0], false, nil
	}
	return "", false, errors.New("too many arguments")
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
