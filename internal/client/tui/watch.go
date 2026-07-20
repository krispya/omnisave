package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// WatchConfig describes the static parts of the watch view's footer.
type WatchConfig struct {
	ServerURL string
	Poll      time.Duration
	PullEvery time.Duration
}

// WatchRequest is a keyboard request from the watch view to its loop.
type WatchRequest int

// WatchSyncNow asks the loop to run a pass immediately.
const WatchSyncNow WatchRequest = iota

// WatchDisplay is the live watch view (FDR-005): every tracked game on its
// line, a footer that proves liveness, and s/q keys. The watch loop feeds
// it snapshots; it never prompts.
type WatchDisplay struct {
	program  *tea.Program
	requests chan WatchRequest
}

// NewWatchDisplay builds the live view. Run blocks until the user quits.
func NewWatchDisplay(config WatchConfig) *WatchDisplay {
	requests := make(chan WatchRequest, 1)
	indicator := spinner.New()
	indicator.Spinner = spinner.MiniDot
	indicator.Style = accentStyle
	model := watchModel{config: config, requests: requests, spinner: indicator}
	return &WatchDisplay{
		program:  tea.NewProgram(model),
		requests: requests,
	}
}

// Run renders the view until the user quits.
func (d *WatchDisplay) Run() error {
	_, err := d.program.Run()
	return err
}

// Quit ends the view, releasing Run.
func (d *WatchDisplay) Quit() {
	d.program.Quit()
}

// Requests carries the user's keyboard requests to the watch loop.
func (d *WatchDisplay) Requests() <-chan WatchRequest {
	return d.requests
}

// Watching reports how many save files the loop is polling.
func (d *WatchDisplay) Watching(files int) {
	d.program.Send(watchWatchingMsg{files: files})
}

// PassStarted marks the view as syncing.
func (d *WatchDisplay) PassStarted() {
	d.program.Send(watchPassStartedMsg{at: time.Now()})
}

// PassFinished settles the view with a pass's final table and tally. The
// table swaps in whole only when a pass completes — mid-pass the previous
// settled table stays put and the footer spinner is the activity signal,
// so running a pass never reflows the layout. The live view always
// repaints, so the changed flag is only for log sinks.
func (d *WatchDisplay) PassFinished(snapshot ReportSnapshot, summary string, changed bool, err error) {
	d.program.Send(watchPassFinishedMsg{snapshot: snapshot, summary: summary, err: err, at: time.Now()})
}

type (
	watchWatchingMsg     struct{ files int }
	watchPassStartedMsg  struct{ at time.Time }
	watchPassFinishedMsg struct {
		snapshot ReportSnapshot
		summary  string
		err      error
		at       time.Time
	}
	watchSpinDoneMsg struct{ generation int }
	watchClockMsg    time.Time
)

// spinnerMinimum keeps the header spinner visible for at least one full
// rotation, so a fast pass still visibly acknowledges the command.
const spinnerMinimum = time.Second

func watchClock() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return watchClockMsg(t) })
}

type watchModel struct {
	config         WatchConfig
	requests       chan<- WatchRequest
	spinner        spinner.Model
	files          int
	snapshot       ReportSnapshot
	summary        string
	failure        string
	syncing        bool
	syncStarted    time.Time
	spinGeneration int
	synced         time.Time
	now            time.Time
}

func (m watchModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, watchClock())
}

func (m watchModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.KeyMsg:
		switch message.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "s":
			if !m.syncing {
				select {
				case m.requests <- WatchSyncNow:
				default:
				}
			}
		}
	case spinner.TickMsg:
		var command tea.Cmd
		m.spinner, command = m.spinner.Update(message)
		return m, command
	case watchClockMsg:
		m.now = time.Time(message)
		return m, watchClock()
	case watchWatchingMsg:
		m.files = message.files
	case watchPassStartedMsg:
		m.syncing = true
		m.syncStarted = message.at
		m.spinGeneration++
	case watchPassFinishedMsg:
		m.synced = message.at
		m.snapshot = message.snapshot
		m.summary = message.summary
		m.failure = ""
		if message.err != nil {
			m.failure = message.err.Error()
		}
		// Results land immediately; the spinner finishes its rotation.
		if remaining := spinnerMinimum - message.at.Sub(m.syncStarted); remaining > 0 {
			generation := m.spinGeneration
			return m, tea.Tick(remaining, func(time.Time) tea.Msg {
				return watchSpinDoneMsg{generation: generation}
			})
		}
		m.syncing = false
	case watchSpinDoneMsg:
		if message.generation == m.spinGeneration {
			m.syncing = false
		}
	}
	return m, nil
}

func (m watchModel) View() string {
	var view strings.Builder
	header := titleStyle.Render("▲ Omnisave") + mutedStyle.Render(" · watching")
	if m.syncing {
		// Activity lives here, appended, so no other line ever reflows.
		header += " " + m.spinner.View()
	}
	view.WriteString(header + "\n\n")
	lines := ComposeReport(m.snapshot, m.now)
	if len(lines) == 0 {
		view.WriteString(mutedStyle.Render("  No tracked saves discovered yet") + "\n")
	}
	for _, line := range lines {
		view.WriteString(line + "\n")
	}
	view.WriteString("\n")
	if m.summary != "" {
		view.WriteString(m.summary + "\n")
	}
	if m.failure != "" {
		view.WriteString("  " + errorStyle.Render("✗") + " " + mutedStyle.Render(m.failure) + "\n")
	}
	view.WriteString("  " + mutedStyle.Render(m.activity()+" · "+m.config.ServerURL) + "\n")
	view.WriteString("  " + mutedStyle.Render("s sync now · q quit") + "\n")
	return view.String()
}

func (m watchModel) activity() string {
	if m.synced.IsZero() {
		return fmt.Sprintf("watching %d save paths", m.files)
	}
	return fmt.Sprintf("watching %d save paths · synced %s", m.files, ago(m.now, m.synced))
}

func ago(now, then time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	elapsed := now.Sub(then)
	switch {
	case elapsed < 5*time.Second:
		return "just now"
	case elapsed < time.Minute:
		return fmt.Sprintf("%ds ago", int(elapsed.Seconds()))
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm ago", int(elapsed.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(elapsed.Hours()))
	}
}
