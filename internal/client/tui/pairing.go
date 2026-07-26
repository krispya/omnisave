package tui

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/krisbaumgartner/omnisave/internal/access"
)

// pollInterval is how often a waiting client asks again. A person walking to
// another room is the thing being waited on, so a second is soon enough and
// keeps a headless box from hammering the server.
const pollInterval = time.Second

// PollPairing asks the server whether the owner has answered yet. It is called
// until it reports something other than pending.
type PollPairing func(context.Context) (access.CollectionStatus, error)

// AwaitApproval shows the code the owner matches in the Dash, and waits.
//
// The code is the whole point of this screen: it is the one part of the
// request the owner can check against the Device in front of them, so it is
// what the Device puts on its screen (FDR-006).
func AwaitApproval(ctx context.Context, serverURL, code string, poll PollPairing) (access.CollectionStatus, error) {
	model := newPairingModel(ctx, serverURL, code, poll)
	result, err := tea.NewProgram(model, tea.WithOutput(os.Stderr)).Run()
	if err != nil {
		return access.CollectionPending, err
	}
	final := result.(pairingModel)
	if final.err != nil {
		return access.CollectionPending, final.err
	}
	if final.aborted {
		return access.CollectionPending, ErrAborted
	}
	return final.state, nil
}

type pairingPolledMsg struct {
	state access.CollectionStatus
	err   error
}

type pairingModel struct {
	ctx       context.Context
	serverURL string
	code      string
	poll      PollPairing
	spinner   spinner.Model
	state     access.CollectionStatus
	err       error
	aborted   bool
}

func newPairingModel(ctx context.Context, serverURL, code string, poll PollPairing) pairingModel {
	indicator := spinner.New()
	indicator.Spinner = spinner.MiniDot
	indicator.Style = accentStyle
	return pairingModel{
		ctx: ctx, serverURL: serverURL, code: code, poll: poll,
		spinner: indicator, state: access.CollectionPending,
	}
}

func (m pairingModel) Init() tea.Cmd {
	return tea.Batch(m.ask(), m.spinner.Tick)
}

func (m pairingModel) ask() tea.Cmd {
	return func() tea.Msg {
		state, err := m.poll(m.ctx)
		return pairingPolledMsg{state: state, err: err}
	}
}

func (m pairingModel) askLater() tea.Cmd {
	return tea.Tick(pollInterval, func(time.Time) tea.Msg {
		state, err := m.poll(m.ctx)
		return pairingPolledMsg{state: state, err: err}
	})
}

func (m pairingModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.KeyMsg:
		switch message.String() {
		case "q", "ctrl+c", "esc":
			m.aborted = true
			return m, tea.Quit
		}
	case spinner.TickMsg:
		if m.state == access.CollectionPending {
			var command tea.Cmd
			m.spinner, command = m.spinner.Update(message)
			return m, command
		}
	case pairingPolledMsg:
		if message.err != nil {
			m.err = message.err
			return m, tea.Quit
		}
		m.state = message.state
		if m.state != access.CollectionPending {
			return m, tea.Quit
		}
		return m, m.askLater()
	}
	return m, nil
}

func (m pairingModel) View() string {
	if m.state != access.CollectionPending || m.err != nil {
		// The outcome is the command's own output; the wait leaves no trace.
		return ""
	}
	lines := []string{
		accentStyle.Render("▲") + " " + titleStyle.Render("Omnisave"),
		mutedStyle.Render("- Server:") + "  " + plainTitle(m.serverURL),
		mutedStyle.Render("- Code:") + "    " + titleStyle.Render(spaced(m.code)),
		"",
		m.spinner.View() + " " + mutedStyle.Render("Waiting for approval"),
		mutedStyle.Render("Open the Dash's server settings and approve this code."),
		"",
		mutedStyle.Render("q cancel"),
	}
	return strings.Join(lines, "\n") + "\n"
}

// spaced widens the code so it is read a character at a time. It is retyped
// by a person looking between two screens, which is the only reason it is
// short enough to need the help.
func spaced(code string) string {
	return strings.Join(strings.Split(code, ""), " ")
}
