package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Track reporting gives every game its own line — a state glyph, the title
// padded to a shared column, and the standing state of its save — with what
// happened this pass indented beneath it, one dim sentence per line. A game
// that needed nothing keeps its single line. Run-wide failures print first.

// TrackReport buffers tracking results and renders them grouped by game.
type TrackReport struct {
	general []string
	order   []string
	games   map[string]*trackedGameReport

	// OnUpdate, when set, receives a fresh snapshot after every event, so
	// live views repaint as a pass progresses.
	OnUpdate func(snapshot ReportSnapshot)
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
	r.event(title, "server save available; run omnisave-client track to choose")
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

// UpToDate records a game whose saves needed nothing this pass, so every
// tracked game still gets its line.
func (r *TrackReport) UpToDate(title string) {
	r.game(title)
	r.changed()
}

// SyncedWith records a bound save's standing state: the Omnisave it syncs
// with and when the last sync event happened — a seed, push, pull, or
// rebind now, or the recorded time of an earlier one. Every healthy bound
// save reads "Save 1 · synced 2m ago"; what happened this pass lives in
// the summary tally. Live views age the timestamp between passes.
func (r *TrackReport) SyncedWith(title, omnisaveName string, at time.Time) {
	entry := r.game(title)
	if entry.syncedWith == "" || at.After(entry.syncedAt) {
		entry.syncedWith = omnisaveName
		entry.syncedAt = at
	}
	r.changed()
}

// Behind records a save matching an older revision, waiting for an
// interactive run to choose between advancing and forking.
func (r *TrackReport) Behind(title, omnisaveName string) {
	r.mark(title, mutedStyle.Render("○"))
	r.event(title, "save is behind "+omnisaveName+", run omnisave-client track to resolve")
}

// Diverged records a save with new progress on both sides, waiting for an
// interactive run to resolve.
func (r *TrackReport) Diverged(title, omnisaveName string) {
	r.mark(title, mutedStyle.Render("○"))
	r.event(title, "save diverged from "+omnisaveName+", run omnisave-client track to resolve")
}

// BoundUnsynced records a save mapped to a chosen Omnisave with no
// matching revision: the mapping exists, but nothing has synced yet.
func (r *TrackReport) BoundUnsynced(title, omnisaveName string) {
	r.event(title, "save bound to "+omnisaveName+" with nothing synced yet")
}

// Unbound records a save left for manual binding.
func (r *TrackReport) Unbound(title string) {
	r.event(title, "save needs omnisave-client bind")
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
		games = append(games, GameStatus{
			Glyph:      glyph,
			Title:      title,
			Events:     append([]string(nil), entry.events...),
			SyncedWith: entry.syncedWith,
			SyncedAt:   entry.syncedAt,
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
		lines = append(lines, gameLine(game, standingState(game, now), width))
		for _, event := range game.Events {
			lines = append(lines, eventIndent+mutedStyle.Render(event))
		}
	}
	return lines
}

// ComposeStanding renders only what is true right now — one line per game,
// no event lines. It is the live view's table: a pass's events scroll past
// as their own lines (see Event), so the pinned block never grows.
func ComposeStanding(snapshot ReportSnapshot, now time.Time) []string {
	width := 0
	for _, game := range snapshot.Games {
		if count := utf8.RuneCountInString(game.Title); count > width {
			width = count
		}
	}
	lines := make([]string, 0, len(snapshot.Games))
	for _, game := range snapshot.Games {
		lines = append(lines, gameLine(game, condition(game, now), width))
	}
	return lines
}

// condition is what the live table says about a game. A save that is bound
// reads as its standing state; anything else falls back to the last thing
// reported about it, so an unresolved event — a diverged save, a missing
// one — stays visible until the pass that clears it.
func condition(game GameStatus, now time.Time) string {
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

// gameLine is what is true of a game: its state glyph, its name, and the
// status it carries. What happened this pass reads underneath, so a game
// with nothing to report still holds exactly one line.
func gameLine(game GameStatus, status string, width int) string {
	line := "  " + game.Glyph + " " + plainTitle(game.Title)
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
	Advanced  int
	Forked    int
	Bound     int
	Unbound   int
	Pushed    int
	Pulled    int
	Diverged  int
	Failed    int
	Synced    bool
}

// Changed reports whether the run did anything worth showing.
func (o TrackOutcome) Changed() bool {
	return o.changes() > 0
}

func (o TrackOutcome) changes() int {
	return o.Added + o.Linked + o.Untracked + o.Pending + o.Seeded + o.Rebound + o.Advanced +
		o.Forked + o.Bound + o.Unbound + o.Pushed + o.Pulled + o.Diverged + o.Failed
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
	if outcome.Advanced > 0 {
		segments = append(segments, mutedStyle.Render(fmt.Sprintf("%d advanced", outcome.Advanced)))
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
	if outcome.Diverged > 0 {
		segments = append(segments, mutedStyle.Render(fmt.Sprintf("%d diverged", outcome.Diverged)))
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
