package main

import (
	"testing"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/client/tui"
)

func sentences(events []tui.Event) []string {
	spoken := make([]string, 0, len(events))
	for _, event := range events {
		spoken = append(spoken, event.Sentence)
	}
	return spoken
}

func TestAConditionIsAnnouncedOnceAndAgainOnlyIfItComesBack(t *testing.T) {
	saveless := tui.ReportSnapshot{Games: []tui.GameStatus{
		{Title: "Project Zomboid", Events: []string{"No save available"}},
	}}
	cleared := tui.ReportSnapshot{Games: []tui.GameStatus{{Title: "Project Zomboid"}}}
	watcher := newAnnouncer()
	now := time.Now()

	first := watcher.announce(saveless, now, now)
	repeat := watcher.announce(saveless, now, now)

	if len(first) != 1 || first[0].Sentence != "No save available" || first[0].Title != "Project Zomboid" {
		t.Fatalf("expected the condition announced once, got %v", sentences(first))
	}
	if len(repeat) != 0 {
		t.Fatalf("expected a standing condition to stay quiet, got %v", sentences(repeat))
	}

	watcher.announce(cleared, now, now)
	returned := watcher.announce(saveless, now, now)
	if len(returned) != 1 {
		t.Fatalf("expected a condition that cleared to announce again, got %v", sentences(returned))
	}
}

func TestASyncIsAnnouncedWhenItHappensAndNotWhenItIsMerelyFound(t *testing.T) {
	started := time.Now()
	old := tui.ReportSnapshot{Games: []tui.GameStatus{
		{Title: "Slay the Spire 2", SyncedWith: "Save 1", SyncedAt: started.Add(-3 * time.Hour)},
	}}
	fresh := tui.ReportSnapshot{Games: []tui.GameStatus{
		{Title: "Slay the Spire 2", SyncedWith: "Save 1", SyncedAt: started.Add(time.Second)},
	}}

	watcher := newAnnouncer()
	found := watcher.announce(old, started, started)
	if len(found) != 0 {
		t.Fatalf("expected a save synced hours ago to be a baseline, got %v", sentences(found))
	}

	moved := watcher.announce(fresh, started, started)
	if len(moved) != 1 || moved[0].Sentence != "synced with Save 1" {
		t.Fatalf("expected the sync announced when it moved, got %v", sentences(moved))
	}
	if quiet := watcher.announce(fresh, started, started); len(quiet) != 0 {
		t.Fatalf("expected the same sync to stay quiet, got %v", sentences(quiet))
	}
}

func TestASaveSeededDuringAPassIsAnnouncedOnFirstSight(t *testing.T) {
	started := time.Now()
	seeded := tui.ReportSnapshot{Games: []tui.GameStatus{
		{Title: "Chrono Trigger", SyncedWith: "Save 1", SyncedAt: started.Add(2 * time.Second)},
	}}

	events := newAnnouncer().announce(seeded, started, started)

	if len(events) != 1 || events[0].Sentence != "synced with Save 1" {
		t.Fatalf("expected a save synced during the pass to be announced, got %v", sentences(events))
	}
}

func TestATrackRunsReportIsNotAnnouncedAgainByTheWatcher(t *testing.T) {
	report := &tui.TrackReport{}
	report.Linked("Slay the Spire 2", "Slay the Spire II")
	report.SyncedWith("Slay the Spire 2", "Save 1", time.Now())
	snapshot := report.Snapshot()

	watcher := newAnnouncer()
	watcher.seen(snapshot)
	events := watcher.announce(snapshot, time.Now(), time.Now())

	if len(events) != 0 {
		t.Fatalf("expected the hand-off to stay quiet about what track printed, got %v", sentences(events))
	}
}

func TestARunWideFailureIsAnnouncedOnce(t *testing.T) {
	failing := tui.ReportSnapshot{General: []string{"Library sync failed — connection refused"}}
	watcher := newAnnouncer()
	now := time.Now()

	first := watcher.announce(failing, now, now)
	repeat := watcher.announce(failing, now, now)

	if len(first) != 1 || first[0].Title != "" {
		t.Fatalf("expected the run-wide failure announced without a game, got %+v", first)
	}
	if len(repeat) != 0 {
		t.Fatalf("expected a persisting failure to stay quiet, got %v", sentences(repeat))
	}
}
