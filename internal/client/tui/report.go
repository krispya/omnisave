package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Track reports render one standing line per game with pass events beneath it.

// TrackReport buffers tracking results and renders them grouped by game.
type TrackReport struct {
	general []string
	order   []string
	games   map[string]*trackedGameReport

	// OnUpdate, when set, receives a fresh snapshot after every event, so
	// live views repaint as a pass progresses.
	OnUpdate func(snapshot ReportSnapshot)

	// OnWorking, when set, receives the game a pass has in hand, and "" when
	// it moves off one. It carries no report content: a live view marks the
	// row it names and keeps showing the settled table underneath, so a pass
	// in progress never replaces what is true with what is half-finished.
	OnWorking func(title string)
}

// Working names the game the pass has moved on to. A pass works one game at
// a time, so a later call replaces the mark rather than adding to it.
func (r *TrackReport) Working(title string) {
	if r.OnWorking != nil {
		r.OnWorking(title)
	}
}

// Idle clears the mark. A pass between games — scanning, sweeping
// processes, talking to the server about all of them — leaves no row
// claiming to be worked on.
func (r *TrackReport) Idle() {
	r.Working("")
}

func (r *TrackReport) changed() {
	if r.OnUpdate != nil {
		r.OnUpdate(r.Snapshot())
	}
}

// Lines returns the current rendered game lines.
func (r *TrackReport) Lines() []string {
	return r.render()
}

type trackedGameReport struct {
	glyph      string
	events     []string
	syncedWith string
	syncedAt   time.Time
	pending    *PendingDecision
}

// PendingKind names a question a headless pass reported and skipped. The
// sentence beside it says the same thing to a reader; this says it to a
// live view, which has to raise the question rather than print it.
type PendingKind string

// PendingDiverged is a save with new progress on both sides, waiting for the
// answer only a person can give (FDR-005, decision 4).
const PendingDiverged PendingKind = "diverged"

// PendingDecision is what a save is waiting to be asked, carried as data so
// the view can key an answer back to the save that asked. The game's title
// is the row it rides, so only the Omnisave has to be named here. ForkName
// and Keep let the raised question promise exactly what each answer does
// (see DivergedQuestion).
type PendingDecision struct {
	Kind         PendingKind
	OmnisaveName string
	ForkName     string
	Keep         DivergedKeep
}

func (r *TrackReport) game(title string) *trackedGameReport {
	if entry, ok := r.games[title]; ok {
		return entry
	}
	if r.games == nil {
		r.games = make(map[string]*trackedGameReport)
	}
	entry := &trackedGameReport{}
	r.games[title] = entry
	r.order = append(r.order, title)
	return entry
}

func (r *TrackReport) event(title, sentence string) {
	entry := r.game(title)
	entry.events = append(entry.events, capitalized(sentence))
	r.changed()
}

func (r *TrackReport) mark(title, glyph string) {
	r.game(title).glyph = glyph
	r.changed()
}

// Added records a game newly created in the Library.
func (r *TrackReport) Added(title, canonical string) {
	r.mark(title, successStyle.Render("+"))
	r.event(title, withCanonical("added to the library", title, canonical))
}

// Linked records a game matched to an existing Library record.
func (r *TrackReport) Linked(title, canonical string) {
	r.mark(title, successStyle.Render("✓"))
	r.event(title, withCanonical("linked to the library", title, canonical))
}

// Pending records a tracked game with no evidence in this scan.
func (r *TrackReport) Pending(title string) {
	r.mark(title, mutedStyle.Render("○"))
	r.event(title, "resolves on a later scan")
}

// Removed records a game untracked on this device.
func (r *TrackReport) Removed(title string) {
	r.mark(title, mutedStyle.Render("-"))
	r.event(title, "untracked from this device")
}

// DeletedOnServer records a tracked game the server no longer has.
func (r *TrackReport) DeletedOnServer(title string) {
	r.event(title, "deleted on the server")
}

// SaveDeleted records a save whose bound Omnisave was deleted on the server.
func (r *TrackReport) SaveDeleted(title string) {
	r.event(title, "bound Omnisave deleted on the server")
}

// Failed records a game the server could not resolve or record.
func (r *TrackReport) Failed(title string, err error) {
	r.mark(title, errorStyle.Render("✗"))
	r.event(title, "failed — "+Cause(err))
}

// NoSave records that an installed game has no local save content to
// protect. The idle glyph yields to any stronger state already reported.
func (r *TrackReport) NoSave(title string) {
	if entry := r.game(title); entry.glyph == "" {
		entry.glyph = mutedStyle.Render("·")
	}
	r.event(title, "no save available")
}

// SaveAvailable records a Device with no local save whose server save awaits an
// interactive choice.
func (r *TrackReport) SaveAvailable(title string) {
	if entry := r.game(title); entry.glyph == "" {
		entry.glyph = mutedStyle.Render("○")
	}
	r.event(title, "server save available; run omnisave track to choose")
}

// SaveLocationUnavailable records that this Device cannot safely determine
// where a server save belongs.
func (r *TrackReport) SaveLocationUnavailable(title string) {
	if entry := r.game(title); entry.glyph == "" {
		entry.glyph = mutedStyle.Render("·")
	}
	r.event(title, "waiting for a local save location")
}

// Forked records a stale local save continued as a new named lineage.
func (r *TrackReport) Forked(title, omnisaveName string) {
	r.event(title, "save forked as "+omnisaveName)
}

// Unlocked records achievements this pass watched a game unlock and reported.
// One is named; more are counted, because a run that finishes several at once
// would otherwise fill the report with them.
func (r *TrackReport) Unlocked(title string, names []string) {
	switch len(names) {
	case 0:
		return
	case 1:
		r.event(title, "unlocked "+names[0])
	default:
		r.event(title, fmt.Sprintf("unlocked %s and %d more", names[0], len(names)-1))
	}
}

// UpToDate records a game whose saves needed nothing this pass, so every
// tracked game still gets its line.
func (r *TrackReport) UpToDate(title string) {
	r.game(title)
	r.changed()
}

// SyncedWith records a bound save and its latest successful sync time.
func (r *TrackReport) SyncedWith(title, omnisaveName string, at time.Time) {
	entry := r.game(title)
	if entry.syncedWith == "" || at.After(entry.syncedAt) {
		entry.syncedWith = omnisaveName
		entry.syncedAt = at
	}
	r.changed()
}

// Stale records a save matching a revision that is not the Omnisave's
// current one — the device may be behind it or ahead of a restored current —
// waiting for an interactive run to choose between jumping and forking.
func (r *TrackReport) Stale(title, omnisaveName string) {
	r.mark(title, mutedStyle.Render("○"))
	r.event(title, "save matches a revision of "+omnisaveName+" that is not current, run omnisave track to resolve")
}

// CurrentMoved records a commit the server refused because the Omnisave's
// Current Revision moved during the pass — another device committed or a
// restore moved the pointer. The next pass reads the moved pointer and
// reconciles; retrying now would guess.
func (r *TrackReport) CurrentMoved(title, omnisaveName string) {
	r.mark(title, mutedStyle.Render("○"))
	r.event(title, omnisaveName+" moved on the server; the next sync pass will reconcile")
}

// PullDeferred records a pull held back because the game is being played:
// applying now would let the running game overwrite the pulled files from
// memory, so the sync waits for it to close.
func (r *TrackReport) PullDeferred(title, omnisaveName string) {
	r.mark(title, mutedStyle.Render("○"))
	r.event(title, omnisaveName+" · waiting for game to close")
}

// Branched records local progress committed as a new branch: a restore had
// moved the Omnisave's current revision off this device's baseline, and
// committing here made the local line current again. It reads as a sync, not
// a condition, because nothing is left to resolve.
func (r *TrackReport) Branched(title, omnisaveName string) {
	r.event(title, "save branched from "+omnisaveName)
}

// BranchKept records unsynced local progress preserved as a branch this
// device is about to leave: a divergence jump keeps the content in the
// Omnisave's tree, named for the device, before adopting the Current
// Revision — no separate fork to clean up.
func (r *TrackReport) BranchKept(title, omnisaveName string) {
	r.event(title, "progress kept as a branch of "+omnisaveName)
}

// Diverged records a save with new progress on both sides, waiting for an
// interactive run to resolve. forkName and keep describe what the answers
// would do, so a view raising the question can say so.
func (r *TrackReport) Diverged(title, omnisaveName, forkName string, keep DivergedKeep) {
	r.mark(title, mutedStyle.Render("○"))
	r.game(title).pending = &PendingDecision{
		Kind: PendingDiverged, OmnisaveName: omnisaveName, ForkName: forkName, Keep: keep,
	}
	r.event(title, "save diverged from "+omnisaveName+", run omnisave track to resolve")
}

// BoundUnsynced records a save mapped to a chosen Omnisave with no
// matching revision: the mapping exists, but nothing has synced yet.
func (r *TrackReport) BoundUnsynced(title, omnisaveName string) {
	r.event(title, "save bound to "+omnisaveName+" with nothing synced yet")
}

// Unbound records a save left for manual binding.
func (r *TrackReport) Unbound(title string) {
	r.event(title, "save needs omnisave bind")
}

// SaveFailed records one save whose binding pass failed; the failure claims
// the game's glyph so it is visible without reading every sentence.
func (r *TrackReport) SaveFailed(title string, err error) {
	r.mark(title, errorStyle.Render("✗"))
	r.event(title, "save failed — "+Cause(err))
}

// SyncFailed records a failure that prevented the whole server update.
func (r *TrackReport) SyncFailed(err error) {
	r.general = append(r.general, capitalized("library sync failed — "+Cause(err)))
	r.changed()
}

// BindingFailed records a failure that prevented the whole binding pass.
func (r *TrackReport) BindingFailed(err error) {
	r.general = append(r.general, capitalized("save binding failed — "+Cause(err)))
	r.changed()
}

// Print renders run-wide failures, then each game's line, blank-line
// separated from the summary that follows.
func (r *TrackReport) Print() {
	lines := r.render()
	for _, line := range lines {
		fmt.Println(line)
	}
	if len(lines) > 0 {
		fmt.Println()
	}
}

func (r *TrackReport) render() []string {
	return ComposeReport(r.Snapshot(), time.Now())
}

// GameStatus is one game's report entry as data, so live views can age the
// synced timestamp between passes.
type GameStatus struct {
	Glyph      string
	Title      string
	Events     []string
	SyncedWith string
	SyncedAt   time.Time
	// Pending is the question this game's save is waiting on, if any. A
	// printed report says so in its sentence; a live view raises it.
	Pending *PendingDecision
}

// ReportSnapshot is the report's current content in renderable form.
type ReportSnapshot struct {
	General []string
	Games   []GameStatus
}

// Snapshot exports the report for live rendering.
func (r *TrackReport) Snapshot() ReportSnapshot {
	games := make([]GameStatus, 0, len(r.order))
	for _, title := range r.order {
		entry := r.games[title]
		glyph := entry.glyph
		if glyph == "" {
			glyph = successStyle.Render("✓")
		}
		var pending *PendingDecision
		if entry.pending != nil {
			decision := *entry.pending
			pending = &decision
		}
		games = append(games, GameStatus{
			Glyph:      glyph,
			Title:      title,
			Events:     append([]string(nil), entry.events...),
			SyncedWith: entry.syncedWith,
			SyncedAt:   entry.syncedAt,
			Pending:    pending,
		})
	}
	return ReportSnapshot{General: append([]string(nil), r.general...), Games: games}
}

// ComposeReport renders a snapshot's lines as of now, so quiet bound saves
// read "Save 1 · synced 2m ago" with a truthful age.
func ComposeReport(snapshot ReportSnapshot, now time.Time) []string {
	var lines []string
	for _, sentence := range snapshot.General {
		lines = append(lines, FailureLine(sentence))
	}
	width := 0
	for _, game := range snapshot.Games {
		if count := utf8.RuneCountInString(game.Title); count > width {
			width = count
		}
	}
	for _, game := range snapshot.Games {
		lines = append(lines, gameLine(game, game.Glyph, standingState(game, now), width))
		for _, event := range game.Events {
			lines = append(lines, eventIndent+mutedStyle.Render(event))
		}
	}
	return lines
}

// Marks are what a live view stitches onto the settled table at render
// time. Presence and pass activity each move on their own cadence, so
// neither may wait for a pass to swap the table underneath them.
type Marks struct {
	// Playing is the set of game titles this device sees being played.
	Playing map[string]bool
	// Working is the game the running pass has in hand, if any.
	Working string
	// Phase is what the pass is doing to that game right now — "downloading",
	// "preparing (2/5)" — as the work itself reports it. It stands in for the
	// row's settled status while it lasts.
	Phase string
	// Spinner is the view's current spinner frame, already styled. The
	// working game's row wears it in place of its glyph.
	Spinner string
}

// ComposeStanding renders current game state with live marks and no event history.
func ComposeStanding(snapshot ReportSnapshot, marks Marks, now time.Time) []string {
	width := 0
	for _, game := range snapshot.Games {
		if count := utf8.RuneCountInString(game.Title); count > width {
			width = count
		}
	}
	lines := make([]string, 0, len(snapshot.Games))
	for _, game := range snapshot.Games {
		lines = append(lines, gameLine(game, rowGlyph(game, marks), rowStatus(game, marks, now), width))
	}
	return lines
}

// rowStatus is what a row says about itself. A game being worked on says
// what is happening to it instead of when it last succeeded: during a sync
// that is the more useful of the two, and the settled status comes back
// intact the moment the pass lets go of the row.
func rowStatus(game GameStatus, marks Marks, now time.Time) string {
	phase := RowPhase(marks.Phase, game.Title)
	if phase == "" || marks.Working != game.Title {
		return condition(game, now)
	}
	// The Omnisave keeps its place at the head of the status, so a row mid
	// sync reads like every other line about it — "Save 1 · downloading"
	// beside "Save 1 · waiting for game to close".
	if game.SyncedWith != "" {
		return game.SyncedWith + " · " + phase
	}
	return phase
}

// RowPhase is a phase as a row says it. The work names the game it is doing
// — the same text serves a one-line command spinner, which has no row and
// needs it — but on a row the title is already the first thing there, so
// saying it twice is noise.
func RowPhase(phase, title string) string {
	if title == "" {
		return phase
	}
	return strings.TrimSuffix(phase, " "+title)
}

// rowGlyph is the mark a row wears. Work outranks presence, which outranks
// the state the last pass settled: the spinner is the only place the view
// can say this game is being worked on right now — the whole point of
// watching it — and a live session is more current than a settled state.
func rowGlyph(game GameStatus, marks Marks) string {
	if marks.Spinner != "" && marks.Working == game.Title {
		return marks.Spinner
	}
	if marks.Playing[game.Title] {
		return successStyle.Render("▶")
	}
	return game.Glyph
}

// condition is what the live table says about a game. A save that is bound
// reads as its standing state; anything else falls back to the last thing
// reported about it, so an unresolved event — a diverged save, a missing
// one — stays visible until the pass that clears it.
func condition(game GameStatus, now time.Time) string {
	// A waiting question reads as the condition alone. The printed report
	// has to name the command that answers it; the live view has a key and
	// a footer to say so, and repeating it on every row would be noise.
	if game.Pending != nil {
		return game.Pending.OmnisaveName + " · " + string(game.Pending.Kind)
	}
	if status := standingState(game, now); status != "" {
		return status
	}
	return game.Events[len(game.Events)-1]
}

// Event is one thing that happened, written once into the terminal's
// scrollback rather than held in the live view. The clock time is what
// makes it readable hours later, where a live view's "just now" would lie.
type Event struct {
	Glyph    string
	Title    string
	Sentence string
	At       time.Time
}

// EventLine renders one streamed event.
func EventLine(event Event) string {
	line := "  " + mutedStyle.Render(event.At.Format("15:04")) + "  " + event.Glyph
	if event.Title != "" {
		return line + " " + plainTitle(event.Title) + "  " + mutedStyle.Render(capitalized(event.Sentence))
	}
	// A run-wide event has no game to name, so the sentence follows its
	// glyph the way a failure line reads.
	return line + " " + mutedStyle.Render(capitalized(event.Sentence))
}

// eventIndent nests a pass's events under the game they belong to.
const eventIndent = "      "

// gameLine is what is true of a game: the glyph it wears, its name, and the
// status it carries. What happened this pass reads underneath, so a game
// with nothing to report still holds exactly one line.
func gameLine(game GameStatus, glyph, status string, width int) string {
	line := "  " + glyph + " " + plainTitle(game.Title)
	if status == "" {
		return line
	}
	padding := strings.Repeat(" ", width-utf8.RuneCountInString(game.Title))
	return line + padding + "  " + mutedStyle.Render(status)
}

func standingState(game GameStatus, now time.Time) string {
	switch {
	case game.SyncedWith != "" && !game.SyncedAt.IsZero():
		return game.SyncedWith + " · synced " + ago(now, game.SyncedAt)
	case game.SyncedWith != "":
		return game.SyncedWith + " · up to date"
	case len(game.Events) == 0:
		// A game the pass never had to touch still says so.
		return "Up to date"
	default:
		return ""
	}
}

// TrackOutcome tallies one track run for the summary line.
type TrackOutcome struct {
	Tracked   int
	Added     int
	Linked    int
	Untracked int
	Pending   int
	Seeded    int
	Rebound   int
	Jumped    int
	Forked    int
	Bound     int
	Unbound   int
	Pushed    int
	Pulled    int
	Diverged  int
	// Branched counts local progress committed as a new branch because a
	// restore moved current off this Device's baseline.
	Branched int
	// Deferred counts pulls held back because the game is being played;
	// the pass after the game closes applies them.
	Deferred int
	// Conflicted counts commits the server refused because the Current
	// Revision moved mid-pass; the next pass reconciles them.
	Conflicted int
	Failed     int
	Synced     bool
}

// Changed reports whether the run did anything worth showing.
func (o TrackOutcome) Changed() bool {
	return o.changes() > 0
}

func (o TrackOutcome) changes() int {
	return o.Added + o.Linked + o.Untracked + o.Pending + o.Seeded + o.Rebound + o.Jumped +
		o.Forked + o.Bound + o.Unbound + o.Pushed + o.Pulled + o.Branched + o.Diverged + o.Deferred +
		o.Conflicted + o.Failed
}

// TrackSummary prints the closing dim tally.
func TrackSummary(outcome TrackOutcome) {
	fmt.Println(SummaryLine(outcome))
}

// SummaryLine renders the closing dim tally as one line.
func SummaryLine(outcome TrackOutcome) string {
	var segments []string
	if outcome.Added > 0 {
		segments = append(segments, mutedStyle.Render(fmt.Sprintf("%d added", outcome.Added)))
	}
	if outcome.Linked > 0 {
		segments = append(segments, mutedStyle.Render(fmt.Sprintf("%d linked", outcome.Linked)))
	}
	if outcome.Untracked > 0 {
		segments = append(segments, mutedStyle.Render(fmt.Sprintf("%d untracked", outcome.Untracked)))
	}
	if outcome.Pending > 0 {
		segments = append(segments, mutedStyle.Render(fmt.Sprintf("%d pending", outcome.Pending)))
	}
	if outcome.Seeded > 0 {
		segments = append(segments, mutedStyle.Render(fmt.Sprintf("%d seeded", outcome.Seeded)))
	}
	if outcome.Rebound > 0 {
		segments = append(segments, mutedStyle.Render(fmt.Sprintf("%d rebound", outcome.Rebound)))
	}
	if outcome.Jumped > 0 {
		segments = append(segments, mutedStyle.Render(fmt.Sprintf("%d jumped", outcome.Jumped)))
	}
	if outcome.Forked > 0 {
		segments = append(segments, mutedStyle.Render(fmt.Sprintf("%d forked", outcome.Forked)))
	}
	if outcome.Bound > 0 {
		segments = append(segments, mutedStyle.Render(fmt.Sprintf("%d bound", outcome.Bound)))
	}
	if outcome.Unbound > 0 {
		segments = append(segments, mutedStyle.Render(fmt.Sprintf("%d unbound", outcome.Unbound)))
	}
	if outcome.Pushed+outcome.Pulled > 0 {
		segments = append(segments, mutedStyle.Render(fmt.Sprintf("%d synced", outcome.Pushed+outcome.Pulled)))
	}
	if outcome.Branched > 0 {
		segments = append(segments, mutedStyle.Render(fmt.Sprintf("%d branched", outcome.Branched)))
	}
	if outcome.Diverged > 0 {
		segments = append(segments, mutedStyle.Render(fmt.Sprintf("%d diverged", outcome.Diverged)))
	}
	if outcome.Deferred > 0 {
		segments = append(segments, mutedStyle.Render(fmt.Sprintf("%d waiting", outcome.Deferred)))
	}
	if outcome.Conflicted > 0 {
		segments = append(segments, mutedStyle.Render(fmt.Sprintf("%d conflicted", outcome.Conflicted)))
	}
	if outcome.Failed > 0 {
		segments = append(segments, errorStyle.Render(fmt.Sprintf("%d failed", outcome.Failed)))
	}
	tracked := fmt.Sprintf("%d tracked", outcome.Tracked)
	if outcome.Synced && len(segments) == 0 {
		tracked += " · up to date"
	}
	segments = append(segments, mutedStyle.Render(tracked))
	return "  " + strings.Join(segments, mutedStyle.Render(" · "))
}

// FailureLine renders a run-wide failure: one glyph and one dim sentence,
// indented to the report's column so a failure before the pass and one
// during it read alike.
func FailureLine(sentence string) string {
	return "  " + FailureGlyph() + " " + mutedStyle.Render(capitalized(sentence))
}

// FailureGlyph is the mark a failure carries wherever it is rendered.
func FailureGlyph() string {
	return errorStyle.Render("✗")
}

func withCanonical(sentence, title, canonical string) string {
	if canonical != "" && !strings.EqualFold(canonical, title) {
		return sentence + " as " + canonical
	}
	return sentence
}

func capitalized(sentence string) string {
	runes := []rune(sentence)
	if len(runes) == 0 {
		return sentence
	}
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func plainTitle(text string) string {
	return nameStyle.UnsetBold().Render(text)
}
