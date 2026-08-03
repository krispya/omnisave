package tui

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

type waitFinishedMsg struct{}
type waitLabelMsg string

// WaitSession lets the running task hand the terminal to an interactive
// prompt and take it back, keeping one activity line across a phase that
// mixes server waits with occasional questions.
type WaitSession struct {
	program *tea.Program
}

// SetLabel replaces the activity line's description without restarting its
// spinner. It is safe to call from the task goroutine.
func (s *WaitSession) SetLabel(label string) {
	s.program.Send(waitLabelMsg(label))
}

// Interact releases the terminal, runs fn — which may run its own
// bubbletea program, such as a huh prompt — and resumes the activity line.
func (s *WaitSession) Interact(fn func()) {
	_ = s.program.ReleaseTerminal()
	defer func() { _ = s.program.RestoreTerminal() }()
	fn()
}

type waitModel struct {
	ctx     context.Context
	cancel  context.CancelFunc
	session *WaitSession
	task    func(context.Context, *WaitSession)
	label   string
	spinner spinner.Model
	done    bool
	aborted bool
}

// Wait shows a braille activity line until task finishes, then clears it so
// the caller's own output stands alone. A non-empty label appears beside the
// spinner. Pressing q, esc, or ctrl+c cancels the task and reports ErrAborted.
func Wait(ctx context.Context, label string, task func(context.Context, *WaitSession)) error {
	return runWait(ctx, os.Stderr, label, task)
}

func runWait(ctx context.Context, output io.Writer, label string, task func(context.Context, *WaitSession), options ...tea.ProgramOption) error {
	taskCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	session := &WaitSession{}
	programOptions := append([]tea.ProgramOption{tea.WithOutput(output)}, options...)
	program := tea.NewProgram(newWaitModel(taskCtx, cancel, session, label, task), programOptions...)
	session.program = program
	final, err := program.Run()
	if err != nil {
		return err
	}
	completed := final.(waitModel)
	if completed.aborted {
		return ErrAborted
	}
	return ctx.Err()
}

func newWaitModel(ctx context.Context, cancel context.CancelFunc, session *WaitSession, label string, task func(context.Context, *WaitSession)) waitModel {
	indicator := spinner.New()
	indicator.Spinner = spinner.MiniDot
	indicator.Style = accentStyle
	return waitModel{ctx: ctx, cancel: cancel, session: session, label: label, task: task, spinner: indicator}
}

func (m waitModel) Init() tea.Cmd {
	return tea.Batch(m.startTask(), m.spinner.Tick)
}

func (m waitModel) startTask() tea.Cmd {
	return func() tea.Msg {
		m.task(m.ctx, m.session)
		return waitFinishedMsg{}
	}
}

func (m waitModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.KeyMsg:
		switch message.String() {
		case "q", "ctrl+c", "esc":
			m.aborted = true
			m.cancel()
		}
	case spinner.TickMsg:
		if !m.done {
			var command tea.Cmd
			m.spinner, command = m.spinner.Update(message)
			return m, command
		}
	case waitFinishedMsg:
		m.done = true
		return m, tea.Quit
	case waitLabelMsg:
		m.label = string(message)
	}
	return m, nil
}

func (m waitModel) View() string {
	if m.done {
		return ""
	}
	if m.label == "" {
		return fmt.Sprintf("  %s\n", m.spinner.View())
	}
	return fmt.Sprintf("  %s %s\n", m.spinner.View(), mutedStyle.Render(m.label))
}
