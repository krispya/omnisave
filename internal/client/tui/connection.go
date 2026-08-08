package tui

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// ErrDisconnected reports a command the user ended at the connection panel:
// the failure is already on screen, so the caller only sets the exit status.
var ErrDisconnected = errors.New("the Omnisave server was not reachable")

// ConnectionFailure is one failed check as the panel shows it: the cause
// beside the disconnected glyph, an optional fix to act on, and whether the
// retry key could clear it.
type ConnectionFailure struct {
	Cause string
	Fix   string
	Retry bool
}

// ConnectionCheck contacts the server, returning nil once it answers. The
// caller owns the policy: which failures are worth retrying, and what to
// tell the user about each.
type ConnectionCheck func(context.Context) *ConnectionFailure

// AwaitConnection contacts the server before a command starts work. A server
// that answers leaves nothing on screen; one that does not gets the brand
// panel, the cause, and a key to try again — a NAS waking up is one press
// away rather than a command to type again.
func AwaitConnection(ctx context.Context, serverURL string, check ConnectionCheck) error {
	model := newConnectionModel(ctx, serverURL, check)
	result, err := tea.NewProgram(model, tea.WithOutput(os.Stderr)).Run()
	if err != nil {
		return err
	}
	final := result.(connectionModel)
	switch {
	case final.connected:
		return nil
	case ctx.Err() != nil:
		return ctx.Err()
	case final.failure == nil:
		// Quit before the first check answered: the run was called off
		// rather than refused, so it ends the way any prompt's escape does.
		return ErrAborted
	default:
		return ErrDisconnected
	}
}

type connectionCheckedMsg struct{ failure *ConnectionFailure }

type connectionModel struct {
	ctx       context.Context
	serverURL string
	check     ConnectionCheck
	spinner   spinner.Model
	failure   *ConnectionFailure
	checking  bool
	connected bool
	quitting  bool
}

func newConnectionModel(ctx context.Context, serverURL string, check ConnectionCheck) connectionModel {
	indicator := spinner.New()
	indicator.Spinner = spinner.MiniDot
	indicator.Style = accentStyle
	return connectionModel{ctx: ctx, serverURL: serverURL, check: check, spinner: indicator, checking: true}
}

func (m connectionModel) Init() tea.Cmd {
	return tea.Batch(m.contact(), m.spinner.Tick)
}

func (m connectionModel) contact() tea.Cmd {
	return func() tea.Msg {
		return connectionCheckedMsg{failure: m.check(m.ctx)}
	}
}

func (m connectionModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.KeyMsg:
		switch message.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "r", "enter":
			if m.canRetry() {
				m.checking = true
				return m, tea.Batch(m.contact(), m.spinner.Tick)
			}
		}
	case spinner.TickMsg:
		if m.checking {
			var command tea.Cmd
			m.spinner, command = m.spinner.Update(message)
			return m, command
		}
	case connectionCheckedMsg:
		m.checking = false
		m.failure = message.failure
		if m.failure == nil {
			m.connected = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m connectionModel) canRetry() bool {
	return !m.checking && m.failure != nil && m.failure.Retry
}

func (m connectionModel) View() string {
	if m.connected || (m.quitting && m.failure == nil) {
		// The command's own output follows; the check leaves no trace.
		return ""
	}
	if m.failure == nil {
		// Nothing to read yet: hold the compact activity line so a server
		// that answers never flashes a panel.
		return "  " + m.spinner.View() + " " + mutedStyle.Render("Contacting the Omnisave server") + "\n"
	}
	return strings.Join(m.lines(), "\n") + "\n"
}

func (m connectionModel) lines() []string {
	lines := []string{
		titleStyle.Render("▲ Omnisave"),
		mutedStyle.Render("- Server:") + "  " + plainTitle(m.serverURL),
		"",
	}
	if m.checking {
		lines = append(lines, m.spinner.View()+" "+mutedStyle.Render("Reconnecting"))
	} else {
		lines = append(lines, errorStyle.Render("✗")+" "+plainTitle("Disconnected")+"  "+
			mutedStyle.Render(m.failure.Cause))
		if m.failure.Fix != "" {
			lines = append(lines, mutedStyle.Render(m.failure.Fix))
		}
	}
	lines = append(lines, "")
	if keys := m.keys(); keys != "" {
		lines = append(lines, mutedStyle.Render(keys))
	}
	return lines
}

func (m connectionModel) keys() string {
	switch {
	case m.quitting:
		// The keys are gone with the view; the panel stays as the record.
		return ""
	case m.checking:
		return "q cancel"
	case m.failure.Retry:
		return "r retry · q quit"
	default:
		return "q quit"
	}
}
