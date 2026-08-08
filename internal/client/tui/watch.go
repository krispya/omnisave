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
	// Initial is the table the view opens with, so a hand-off from a run
	// that already reconciled shows what that pass established instead of an
	// empty block while the first pass waits for its poll.
	Initial ReportSnapshot
	// Synced is when that hand-off pass reached the server, so the footer
	// proves liveness from the first frame. Zero means this view owns its
	// first pass and has nothing to claim yet.
	Synced time.Time
}

// WatchRequest is a keyboard request from the watch view to its loop.
type WatchRequest int

// WatchSyncNow asks the loop to run a pass immediately.
const WatchSyncNow WatchRequest = iota

// PassResult is one completed pass as its sink shows it: what is true
// afterwards, and what changed on the way there.
type PassResult struct {
	Snapshot ReportSnapshot
	Events   []Event
	Summary  string
	Changed  bool
	Err      error
	At       time.Time
}

// WatchDisplay is the live watch view (FDR-005): every tracked game on its
// line, a footer that proves liveness, and s/q keys. Events scroll past
// above the view as they happen, so the block itself never grows and an
// idle watcher reads as one clean table. The watch loop feeds it passes; it
// never prompts.
type WatchDisplay struct {
	program  *tea.Program
	requests chan WatchRequest
}

// NewWatchDisplay builds the live view. Run blocks until the user quits.
func NewWatchDisplay(config WatchConfig) *WatchDisplay {
	requests := make(chan WatchRequest, 1)
	return &WatchDisplay{
		program:  tea.NewProgram(newWatchModel(config, requests)),
		requests: requests,
	}
}

// newWatchModel opens the view on what its caller already established.
func newWatchModel(config WatchConfig, requests chan<- WatchRequest) watchModel {
	indicator := spinner.New()
	indicator.Spinner = spinner.MiniDot
	indicator.Style = accentStyle
	return watchModel{
		config:   config,
		requests: requests,
		spinner:  indicator,
		snapshot: config.Initial,
		synced:   config.Synced,
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

// Playing replaces which games this device sees being played, by display
// title. The marker rides the standing table, so it survives pass swaps.
func (d *WatchDisplay) Playing(titles []string) {
	d.program.Send(watchPlayingMsg(titles))
}

// PassFinished settles the view with a pass's final table and prints its
// events above it. The table swaps in whole only when a pass completes —
// mid-pass the previous settled table stays put and the footer spinner is
// the activity signal, so running a pass never reflows the layout.
func (d *WatchDisplay) PassFinished(result PassResult) {
	d.program.Send(watchPassFinishedMsg{result: result})
}

type (
	watchWatchingMsg     struct{ files int }
	watchPassStartedMsg  struct{ at time.Time }
	watchPlayingMsg      []string
	watchPassFinishedMsg struct{ result PassResult }
	watchSpinDoneMsg     struct{ generation int }
	watchClockMsg        time.Time
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
	playing        map[string]bool
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
	case watchPlayingMsg:
		playing := make(map[string]bool, len(message))
		for _, title := range message {
			playing[title] = true
		}
		m.playing = playing
	case watchPassStartedMsg:
		m.syncing = true
		m.syncStarted = message.at
		m.spinGeneration++
	case watchPassFinishedMsg:
		result := message.result
		m.failure = passFailure(result)
		if m.failure == "" {
			// Only a pass that reached the server can claim the table and
			// the clock; an outage leaves both showing the last truth.
			m.snapshot = result.Snapshot
			m.synced = result.At
		} else if len(result.Snapshot.Games) > 0 {
			m.snapshot = result.Snapshot
		}
		// The pass's events leave the view for the terminal's scrollback,
		// in order, above the block that stays.
		commands := make([]tea.Cmd, 0, len(result.Events)+1)
		for _, event := range result.Events {
			commands = append(commands, tea.Println(EventLine(event)))
		}
		// Results land immediately; the spinner finishes its rotation.
		if remaining := spinnerMinimum - result.At.Sub(m.syncStarted); remaining > 0 {
			generation := m.spinGeneration
			commands = append(commands, tea.Tick(remaining, func(time.Time) tea.Msg {
				return watchSpinDoneMsg{generation: generation}
			}))
		} else {
			m.syncing = false
		}
		return m, tea.Sequence(commands...)
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
		header += " " + m.spinner.View()
	}
	view.WriteString(header + "\n\n")
	// The playing marker is stitched in at render time: presence moves on
	// its own cadence, and must not wait for a pass to swap the table.
	lines := ComposeStanding(m.snapshot, m.playing, m.now)
	if len(lines) == 0 {
		view.WriteString(mutedStyle.Render("  No tracked saves discovered yet") + "\n")
	}
	for _, line := range lines {
		view.WriteString(line + "\n")
	}
	view.WriteString("\n")
	if m.failure != "" {
		view.WriteString(FailureLine(m.failure) + "\n")
	}
	view.WriteString("  " + mutedStyle.Render(m.activity()+" · "+m.config.ServerURL) + "\n")
	view.WriteString("  " + mutedStyle.Render("s sync now · q quit") + "\n")
	return view.String()
}

// passFailure is what stopped a pass, as one sentence: the pass's own
// error, or the run-wide failure it reported. Both are conditions rather
// than events — they hold until a pass clears them — so the view keeps
// them while the stream announces them once.
func passFailure(result PassResult) string {
	if result.Err != nil {
		return Cause(result.Err)
	}
	if len(result.Snapshot.General) > 0 {
		return result.Snapshot.General[0]
	}
	return ""
}

func (m watchModel) activity() string {
	watching := "watching " + SavePaths(m.files)
	if m.synced.IsZero() {
		return watching
	}
	return watching + " · synced " + ago(m.now, m.synced)
}

// SavePaths counts what a watcher is polling, so the live footer and the
// plain log say it the same way.
func SavePaths(count int) string {
	if count == 1 {
		return "1 save path"
	}
	return fmt.Sprintf("%d save paths", count)
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
