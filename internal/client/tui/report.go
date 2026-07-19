package tui

import (
	"fmt"
	"strings"
	"unicode"
)

// Track reporting groups events under the game they belong to: each game
// appears once with a state glyph, and every event beneath it has the same
// shape — a dim, indented sentence starting with a capital. Run-wide
// failures print first; games with no events do not appear at all.

// TrackReport buffers tracking results and renders them grouped by game.
type TrackReport struct {
	general []string
	order   []string
	games   map[string]*trackedGameReport
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
}

func (r *TrackReport) mark(title, glyph string) {
	r.game(title).glyph = glyph
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

// NoSave records that an installed game has no local save content to protect.
func (r *TrackReport) NoSave(title string) {
	r.event(title, "no save available to sync")
}

// FastForwarded records a stale local save advanced to a named Omnisave head.
func (r *TrackReport) FastForwarded(title, omnisaveName string) {
	r.event(title, "save jumped to the head of "+omnisaveName)
}

// Forked records a stale local save continued as a new named lineage.
func (r *TrackReport) Forked(title, omnisaveName string) {
	r.event(title, "save forked as "+omnisaveName)
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
}

// BindingFailed records a failure that prevented the whole binding pass.
func (r *TrackReport) BindingFailed(err error) {
	r.general = append(r.general, capitalized("save binding failed — "+strings.TrimSpace(err.Error())))
}

// Print renders run-wide failures, then each game with its events beneath.
func (r *TrackReport) Print() {
	for _, line := range r.render() {
		fmt.Println(line)
	}
}

func (r *TrackReport) render() []string {
	var lines []string
	for _, sentence := range r.general {
		lines = append(lines, "  "+errorStyle.Render("✗")+" "+mutedStyle.Render(sentence))
	}
	for _, title := range r.order {
		entry := r.games[title]
		glyph := entry.glyph
		if glyph == "" {
			// Only save events happened; the game itself synced quietly.
			glyph = successStyle.Render("✓")
		}
		lines = append(lines, "  "+glyph+" "+plainTitle(title))
		for _, event := range entry.events {
			lines = append(lines, "      "+mutedStyle.Render(event))
		}
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
	Unbound   int
	Failed    int
	Synced    bool
}

func (o TrackOutcome) changes() int {
	return o.Added + o.Linked + o.Untracked + o.Pending + o.Seeded + o.Rebound + o.Advanced + o.Forked + o.Unbound + o.Failed
}

// TrackSummary prints the closing dim tally, blank-line separated from any
// change lines above it.
func TrackSummary(outcome TrackOutcome) {
	if outcome.changes() > 0 {
		fmt.Println()
	}
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
	if outcome.Unbound > 0 {
		segments = append(segments, mutedStyle.Render(fmt.Sprintf("%d unbound", outcome.Unbound)))
	}
	if outcome.Failed > 0 {
		segments = append(segments, errorStyle.Render(fmt.Sprintf("%d failed", outcome.Failed)))
	}
	tracked := fmt.Sprintf("%d tracked", outcome.Tracked)
	if outcome.Synced && len(segments) == 0 {
		tracked += " · up to date"
	}
	segments = append(segments, mutedStyle.Render(tracked))
	fmt.Println("  " + strings.Join(segments, mutedStyle.Render(" · ")))
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
