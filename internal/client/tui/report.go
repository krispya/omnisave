package tui

import (
	"fmt"
	"strings"
)

// Track reporting renders one glyph line per library change and closes with a
// dim, glyph-free summary line — quiet about games that did not change.

// TrackOutcome tallies one track run for the summary line.
type TrackOutcome struct {
	Tracked   int
	Added     int
	Linked    int
	Untracked int
	Pending   int
	Seeded    int
	Unbound   int
	Failed    int
	Synced    bool
}

func (o TrackOutcome) changes() int {
	return o.Added + o.Linked + o.Untracked + o.Pending + o.Seeded + o.Unbound + o.Failed
}

// TrackAdded reports a game newly created in the Library.
func TrackAdded(title, canonical string) {
	fmt.Println("  " + successStyle.Render("+") + " " + trackedTitle(title, canonical))
}

// TrackLinked reports a game matched to an existing Library record.
func TrackLinked(title, canonical string) {
	fmt.Println("  " + successStyle.Render("✓") + " " + trackedTitle(title, canonical))
}

// TrackPending reports a tracked game with no evidence in this scan.
func TrackPending(title string) {
	fmt.Println("  " + mutedStyle.Render("○ "+title+" — resolves on a later scan"))
}

// TrackRemoved reports a game untracked on this device.
func TrackRemoved(title string) {
	fmt.Println("  " + mutedStyle.Render("- "+title))
}

// TrackFailed reports one game the server could not resolve or record.
func TrackFailed(title string, err error) {
	fmt.Println("  " + errorStyle.Render("✗") + " " + plainTitle(title) + "  " +
		mutedStyle.Render(strings.TrimSpace(err.Error())))
}

// BindSeeded reports a local save seeded into a new server Omnisave.
func BindSeeded(title, kind string) {
	fmt.Println("  " + successStyle.Render("↑") + " " + plainTitle(title) + "  " +
		mutedStyle.Render(kind+" seeded to a new Omnisave"))
}

// BindSkipped reports a local save left unbound because the server already
// has saves for its game.
func BindSkipped(title string) {
	fmt.Println("  " + mutedStyle.Render("○ "+title+" — server already has saves; run omnisave-client bind"))
}

// TrackSyncFailed reports that the server was unreachable for the whole run.
func TrackSyncFailed(err error) {
	fmt.Println("  " + errorStyle.Render("✗") + " library sync failed  " +
		mutedStyle.Render(strings.TrimSpace(err.Error())))
}

// TrackHint prints a muted follow-up suggestion under the summary.
func TrackHint(text string) {
	fmt.Println("  " + mutedStyle.Render(text))
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

func trackedTitle(title, canonical string) string {
	line := plainTitle(title)
	if canonical != "" && !strings.EqualFold(canonical, title) {
		line += " " + mutedStyle.Render("→ "+canonical)
	}
	return line
}

func plainTitle(text string) string {
	return nameStyle.UnsetBold().Render(text)
}
