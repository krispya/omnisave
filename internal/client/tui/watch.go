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
	d.program.Send(watchPassStartedMsg{})
}

// Snapshot repaints the game table mid-pass.
func (d *WatchDisplay) Snapshot(lines []string) {
	d.program.Send(watchSnapshotMsg{lines: lines})
}

// PassFinished settles the view with a pass's final table and tally. The
// live view always repaints, so the changed flag is only for log sinks.
func (d *WatchDisplay) PassFinished(lines []string, summary string, changed bool, err error) {
	d.program.Send(watchPassFinishedMsg{lines: lines, summary: summary, err: err, at: time.Now()})
}

type (
	watchWatchingMsg     struct{ files int }
	watchPassStartedMsg  struct{}
	watchSnapshotMsg     struct{ lines []string }
	watchPassFinishedMsg struct {
		lines   []string
		summary string
		err     error
		at      time.Time
	}
	watchClockMsg time.Time
)

func watchClock() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return watchClockMsg(t) })
}

type watchModel struct {
	config   WatchConfig
	requests chan<- WatchRequest
	spinner  spinner.Model
	files    int
	lines    []string
	summary  string
	failure  string
	syncing  bool
	synced   time.Time
	now      time.Time
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
	case watchSnapshotMsg:
		m.lines = message.lines
	case watchPassFinishedMsg:
		m.syncing = false
		m.synced = message.at
		m.lines = message.lines
		m.summary = message.summary
		m.failure = ""
		if message.err != nil {
			m.failure = message.err.Error()
		}
	}
	return m, nil
}

func (m watchModel) View() string {
	var view strings.Builder
	view.WriteString(titleStyle.Render("▲ Omnisave") + mutedStyle.Render(" · watching") + "\n\n")
	if len(m.lines) == 0 {
		view.WriteString(mutedStyle.Render("  No tracked saves discovered yet") + "\n")
	}
	for _, line := range m.lines {
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
	if m.syncing {
		return m.spinner.View() + " syncing"
	}
	if m.synced.IsZero() {
		return fmt.Sprintf("watching %d save files", m.files)
	}
	return fmt.Sprintf("watching %d save files · synced %s", m.files, ago(m.now, m.synced))
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
