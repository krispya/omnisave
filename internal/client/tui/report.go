package tui

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Track reporting gives every game one line: a state glyph, the title
// padded to a shared column, and its status — dim sentences joined by
// dots, or "Up to date" when the pass needed nothing. Run-wide failures
// print first.

// TrackReport buffers tracking results and renders them grouped by game.
type TrackReport struct {
	general []string
	order   []string
	games   map[string]*trackedGameReport

	// OnUpdate, when set, receives a fresh snapshot of the rendered game
	// lines after every event, so live views repaint as a pass progresses.
	OnUpdate func(lines []string)
}

func (r *TrackReport) changed() {
	if r.OnUpdate != nil {
		r.OnUpdate(r.render())
	}
}

// Lines returns the current rendered game lines.
func (r *TrackReport) Lines() []string {
	return r.render()
}

type trackedGameReport struct {
	glyph  string
	events []string
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
	r.event(title, "failed — "+strings.TrimSpace(err.Error()))
}

// Seeded records a game's local save seeded into a new Omnisave, named
// when the server reports a display name for it.
func (r *TrackReport) Seeded(title, omnisaveName string) {
	if omnisaveName == "" {
		r.event(title, "save seeded to a new Omnisave")
		return
	}
	r.event(title, "save seeded as "+omnisaveName)
}

// Rebound records a local save automatically reattached to a named Omnisave.
func (r *TrackReport) Rebound(title, omnisaveName string) {
	r.event(title, "save matches the head of "+omnisaveName+" and is resyncing")
}

// NoSave records that an installed game has no local save content to
// protect. The idle glyph yields to any stronger state already reported.
func (r *TrackReport) NoSave(title string) {
	if entry := r.game(title); entry.glyph == "" {
		entry.glyph = mutedStyle.Render("·")
	}
	r.event(title, "no save available")
}

// FastForwarded records a stale local save advanced to a named Omnisave head.
func (r *TrackReport) FastForwarded(title, omnisaveName string) {
	r.event(title, "save jumped to the head of "+omnisaveName)
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

// SyncedUp records local progress committed to the bound Omnisave.
func (r *TrackReport) SyncedUp(title, omnisaveName string) {
	r.event(title, "save synced to "+omnisaveName)
}

// SyncedDown records the bound Omnisave's head applied to the local save.
func (r *TrackReport) SyncedDown(title, omnisaveName string) {
	r.event(title, "save synced from "+omnisaveName)
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

// Bound records a save mapped to the Omnisave chosen in the ambiguity
// prompt: with a matching revision the sync baseline is set there; without
// one the mapping waits for synchronization to reconcile the content.
func (r *TrackReport) Bound(title, omnisaveName string, matched bool) {
	if matched {
		r.event(title, "save bound to "+omnisaveName+" at a matching revision")
		return
	}
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
	r.event(title, "save failed — "+strings.TrimSpace(err.Error()))
}

// SyncFailed records a failure that prevented the whole server update.
func (r *TrackReport) SyncFailed(err error) {
	r.general = append(r.general, capitalized("library sync failed — "+strings.TrimSpace(err.Error())))
	r.changed()
}

// BindingFailed records a failure that prevented the whole binding pass.
func (r *TrackReport) BindingFailed(err error) {
	r.general = append(r.general, capitalized("save binding failed — "+strings.TrimSpace(err.Error())))
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
	var lines []string
	for _, sentence := range r.general {
		lines = append(lines, "  "+errorStyle.Render("✗")+" "+mutedStyle.Render(sentence))
	}
	width := 0
	for _, title := range r.order {
		if count := utf8.RuneCountInString(title); count > width {
			width = count
		}
	}
	for _, title := range r.order {
		entry := r.games[title]
		glyph := entry.glyph
		if glyph == "" {
			glyph = successStyle.Render("✓")
		}
		status := "Up to date"
		if len(entry.events) > 0 {
			status = strings.Join(entry.events, " · ")
		}
		padding := strings.Repeat(" ", width-utf8.RuneCountInString(title))
		lines = append(lines, "  "+glyph+" "+plainTitle(title)+padding+"  "+mutedStyle.Render(status))
	}
	return lines
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
