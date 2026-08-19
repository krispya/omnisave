package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// WatchRequestKind names what the view is asking its loop to do.
type WatchRequestKind int

const (
	// WatchSyncNow asks the loop to run a pass immediately.
	WatchSyncNow WatchRequestKind = iota
	// WatchQuestionOpened tells the loop a question is on screen, so it holds
	// its triggers: an answer has to land on the table it was built from, and
	// a pass swapping that table underneath would answer a different one.
	WatchQuestionOpened
	// WatchQuestionDismissed releases the hold with nothing decided.
	WatchQuestionDismissed
	// WatchAnswered carries a decision and releases the hold. The loop
	// replays it into the pass that follows rather than acting on it here.
	WatchAnswered
)

// WatchRequest is a keyboard request from the watch view to its loop.
type WatchRequest struct {
	Kind WatchRequestKind
	// Title and Omnisave name the save an answer belongs to — the same pair
	// the question showed, which is how the pass finds it again.
	Title    string
	Omnisave string
	Diverged DivergedBindingChoice
}

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

// WatchDisplay renders tracked-game state and streams completed-pass events above it.
type WatchDisplay struct {
	program  *tea.Program
	requests chan WatchRequest
}

// NewWatchDisplay builds the live view. Run blocks until the user quits.
func NewWatchDisplay(config WatchConfig) *WatchDisplay {
	// Opening a question, answering it, and asking for a sync are separate
	// messages a user can produce faster than a pass drains them.
	requests := make(chan WatchRequest, 4)
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

// Working names the game the running pass has in hand, by display title, so
// the row spins while its save is being worked on. An empty title clears the
// mark: the pass is between games and no row may claim the spinner.
func (d *WatchDisplay) Working(title string) {
	d.program.Send(watchWorkingMsg(title))
}

// Phase carries what the pass is doing right now, as the work itself reports
// it. It belongs to whatever the pass has in hand: a game's row while one is
// held, and the header while the pass is between games.
func (d *WatchDisplay) Phase(message string) {
	d.program.Send(watchPhaseMsg(message))
}

// PassFinished atomically replaces settled state and prints the pass's events.
func (d *WatchDisplay) PassFinished(result PassResult) {
	d.program.Send(watchPassFinishedMsg{result: result})
}

type (
	watchWatchingMsg     struct{ files int }
	watchPassStartedMsg  struct{ at time.Time }
	watchPlayingMsg      []string
	watchWorkingMsg      string
	watchPhaseMsg        string
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
	config   WatchConfig
	requests chan<- WatchRequest
	spinner  spinner.Model
	files    int
	snapshot ReportSnapshot
	playing  map[string]bool
	// working is the game the running pass has in hand, and phase is what it
	// is doing. They are what the view learns while a pass is in flight: the
	// row working names spins, and it says phase instead of its settled
	// status until the pass lets go.
	working        string
	phase          string
	failure        string
	syncing        bool
	syncStarted    time.Time
	spinGeneration int
	synced         time.Time
	now            time.Time
	// question is the modal, open over the table. While it is open the loop
	// holds its passes, so the table behind it cannot move under the answer.
	question *watchQuestion
}

// watchQuestion is one waiting decision, taken off the settled table and
// raised as a modal. It carries the pair the answer is keyed by.
type watchQuestion struct {
	title    string
	omnisave string
	options  []DivergedOption
	cursor   int
}

// firstPending finds the save waiting to be asked. Several waiting saves are
// answered one at a time, in table order: the modal names the game it is
// asking about, so the next press raises the next one.
func firstPending(snapshot ReportSnapshot) *watchQuestion {
	for _, game := range snapshot.Games {
		if game.Pending == nil || game.Pending.Kind != PendingDiverged {
			continue
		}
		return &watchQuestion{
			title:    game.Title,
			omnisave: game.Pending.OmnisaveName,
			options: DivergedOptions(DivergedQuestion{
				GameTitle:    game.Title,
				OmnisaveName: game.Pending.OmnisaveName,
				ForkName:     game.Pending.ForkName,
				Keep:         game.Pending.Keep,
			}),
		}
	}
	return nil
}

// ask sends a request without ever blocking the update loop.
func (m watchModel) ask(request WatchRequest) {
	select {
	case m.requests <- request:
	default:
	}
}

// updateQuestion owns the keyboard while the modal is open. Escape decides
// nothing: no answer is recorded and no pass runs, so the row stays exactly
// as it was and the question can be asked again.
func (m watchModel) updateQuestion(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch message.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.ask(WatchRequest{Kind: WatchQuestionDismissed})
		m.question = nil
	case "up", "k":
		if m.question.cursor > 0 {
			moved := *m.question
			moved.cursor--
			m.question = &moved
		}
	case "down", "j":
		if m.question.cursor < len(m.question.options)-1 {
			moved := *m.question
			moved.cursor++
			m.question = &moved
		}
	case "enter":
		answer := WatchRequest{
			Kind:     WatchAnswered,
			Title:    m.question.title,
			Omnisave: m.question.omnisave,
			Diverged: m.question.options[m.question.cursor].Choice,
		}
		m.question = nil
		m.ask(answer)
	}
	return m, nil
}

func (m watchModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, watchClock())
}

func (m watchModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.KeyMsg:
		if m.question != nil {
			return m.updateQuestion(message)
		}
		switch message.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "s":
			if !m.syncing {
				m.ask(WatchRequest{Kind: WatchSyncNow})
			}
		case "r":
			// A pass in flight owns the table, so the question waits for it
			// the same way a sync request does. What it asks about has to be
			// settled before it is asked.
			if !m.syncing {
				if question := firstPending(m.snapshot); question != nil {
					m.question = question
					m.ask(WatchRequest{Kind: WatchQuestionOpened})
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
	case watchWorkingMsg:
		// A phase belongs to the game it was reported under, so moving to the
		// next one starts with nothing said about it rather than inheriting
		// what the last game was doing.
		m.working = string(message)
		m.phase = ""
	case watchPhaseMsg:
		m.phase = string(message)
	case watchPassStartedMsg:
		m.syncing = true
		m.syncStarted = message.at
		m.spinGeneration++
	case watchPassFinishedMsg:
		result := message.result
		// A finished pass has nothing in hand, whatever it last reported
		// working on, so no row is left spinning between passes.
		m.working = ""
		m.phase = ""
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
		commands := make([]tea.Cmd, 0, 2)
		if scrollback := eventScrollback(result.Events); scrollback != "" {
			// One print command keeps the batch together. Printing each event
			// separately repaints the live block between them and leaves gaps.
			commands = append(commands, tea.Println(scrollback))
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

// eventScrollback keeps events adjacent even when separate passes print them.
// The live view owns the blank line before its title so it is not committed
// between event batches.
func eventScrollback(events []Event) string {
	if len(events) == 0 {
		return ""
	}
	lines := make([]string, 0, len(events))
	for _, event := range events {
		lines = append(lines, EventLine(event))
	}
	return strings.Join(lines, "\n")
}

// questionStyle boxes the modal. The rest of the client is borderless, so
// the outline is what says the watch has stopped to ask something — a
// takeover of the pinned block rather than one more line in it.
var questionStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(mutedStyle.GetForeground()).
	Padding(0, 1).
	MarginLeft(2)

// view renders the modal: what is being asked about, then the answers as the
// same "label · description" line the track form shows, so both surfaces
// promise identically what each answer would do.
func (q watchQuestion) view() string {
	var body strings.Builder
	body.WriteString(nameStyle.Render(q.title) + "\n")
	body.WriteString(mutedStyle.Render(q.omnisave+" diverges between this device and the server") + "\n\n")
	for index, option := range q.options {
		if index > 0 {
			body.WriteString("\n")
		}
		if index == q.cursor {
			body.WriteString(accentStyle.Render("› ") +
				nameStyle.UnsetBold().Render(option.Label) +
				mutedStyle.Render(" · "+option.Description))
			continue
		}
		body.WriteString("  " + mutedStyle.Render(option.Label+" · "+option.Description))
	}
	return questionStyle.Render(body.String())
}

func (m watchModel) View() string {
	var view strings.Builder
	header := titleStyle.Render("▲ Omnisave") + mutedStyle.Render(" · watching")
	// A question owns the block, so the header carries nothing beside it: no
	// pass is running behind it to spin for.
	if m.syncing && m.question == nil {
		header += " " + m.spinner.View()
	}
	// The separator belongs to the live block. If it were printed with each
	// event batch, separate passes would leave blank lines between events.
	view.WriteString("\n" + header + "\n\n")
	if m.question != nil {
		view.WriteString(m.question.view() + "\n\n")
		view.WriteString("  " + mutedStyle.Render("↑↓ choose · enter confirm · esc dismiss") + "\n")
		return view.String()
	}
	// The live marks are stitched in at render time: presence and the pass's
	// current game each move on their own cadence, and must not wait for a
	// pass to swap the table.
	lines := ComposeStanding(m.snapshot, Marks{
		Playing: m.playing,
		Working: m.working,
		Phase:   m.phase,
		Spinner: m.spinner.View(),
	}, m.now)
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
	keys := "s sync now · q quit"
	// The resolve key appears only when a save is actually waiting on it:
	// a standing offer to answer nothing reads as something being wrong.
	if firstPending(m.snapshot) != nil {
		keys = "s sync now · r resolve · q quit"
	}
	view.WriteString("  " + mutedStyle.Render(keys) + "\n")
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
