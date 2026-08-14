package tui

import (
	"context"
	"fmt"
	"io"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
)

type fileDescriptor interface {
	Fd() uintptr
}

func Run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, backend Backend, options Options) int {
	input, inputOK := stdin.(fileDescriptor)
	output, outputOK := stdout.(fileDescriptor)
	if !inputOK || !outputOK || !term.IsTerminal(input.Fd()) || !term.IsTerminal(output.Fd()) {
		writeError(stderr, "vgxness tui: stdin and stdout must be interactive terminals; run `vgxness tui` from a terminal")
		return 2
	}
	if ctx == nil {
		ctx = context.Background()
	}
	program := tea.NewProgram(
		NewModel(ctx, backend, options),
		tea.WithContext(ctx),
		tea.WithInput(stdin),
		tea.WithOutput(stdout),
		tea.WithoutSignalHandler(),
	)
	stopActivity := attachSessionActivity(backend, program)
	_, err := program.Run()
	stopActivity()
	if err != nil {
		if ctx.Err() != nil {
			return 130
		}
		writeError(stderr, "vgxness tui: terminal session failed; retry from an interactive terminal")
		return 1
	}
	return 0
}

func writeError(writer io.Writer, message string) {
	if writer != nil {
		_, _ = fmt.Fprintln(writer, message)
	}
}
