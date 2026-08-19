package tui

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTrackReportNestsEachGamesEventsUnderItsLine(t *testing.T) {
	report := &TrackReport{}
	report.Added("Slay the Spire 2", "Slay the Spire II")
	report.SyncedWith("Slay the Spire 2", "Save 1", time.Now())
	report.Linked("Project Zomboid", "")
	report.Unbound("Project Zomboid")

	rendered := strings.Join(report.render(), "\n")
	expected := strings.Join([]string{
		"  + Slay the Spire 2  Save 1 · synced just now",
		"      Added to the library as Slay the Spire II",
		"  ✓ Project Zomboid",
		"      Linked to the library",
		"      Save needs omnisave bind",
	}, "\n")
	if rendered != expected {
		t.Fatalf("expected each event on its own line under its game, got:\n%s", rendered)
	}
}

func TestQuietBoundSavesNameTheirOmnisaveAndAge(t *testing.T) {
	report := &TrackReport{}
	report.SyncedWith("Slay the Spire 2", "Save 1", time.Now().Add(-2*time.Minute))

	rendered := strings.Join(report.render(), "\n")
	if !strings.Contains(rendered, "✓ Slay the Spire 2  Save 1 · synced 2m ago") {
		t.Fatalf("expected the quiet line to name the Omnisave with its age, got:\n%s", rendered)
	}

	legacy := &TrackReport{}
	legacy.SyncedWith("Chrono Trigger", "New Game+", time.Time{})
	if !strings.Contains(strings.Join(legacy.render(), "\n"), "New Game+ · up to date") {
		t.Fatalf("expected a binding without a timestamp to fall back, got %q", legacy.render())
	}
}

func TestTrackReportShowsUpToDateGamesInsteadOfHidingThem(t *testing.T) {
	report := &TrackReport{}
	report.UpToDate("Slay the Spire 2")

	rendered := strings.Join(report.render(), "\n")
	if !strings.Contains(rendered, "✓ Slay the Spire 2  Up to date") {
		t.Fatalf("expected a registered game with no binding info to show up to date, got:\n%s", rendered)
	}
}

func TestTheStandingTableDropsEventLinesAndKeepsTheCondition(t *testing.T) {
	report := &TrackReport{}
	report.Linked("Slay the Spire 2", "")
	report.SyncedWith("Slay the Spire 2", "Save 1", time.Now().Add(-2*time.Minute))
	report.Linked("Project Zomboid", "")
	report.Diverged("Project Zomboid", "Save 2", "Save 2 (Steam Deck)")

	rendered := strings.Join(ComposeStanding(report.Snapshot(), Marks{}, time.Now()), "\n")

	expected := strings.Join([]string{
		"  ✓ Slay the Spire 2  Save 1 · synced 2m ago",
		"  ○ Project Zomboid   Save 2 · diverged",
	}, "\n")
	if rendered != expected {
		t.Fatalf("expected one standing line per game, got:\n%s", rendered)
	}
}

// The live table states the condition and leaves the instruction to the
// footer's key, but a printed report has no key to offer, so its own line
// keeps naming the command that resolves the save.
func TestThePrintedReportStillNamesWhatResolvesADivergedSave(t *testing.T) {
	report := &TrackReport{}
	report.Linked("Project Zomboid", "")
	report.Diverged("Project Zomboid", "Save 2", "Save 2 (Steam Deck)")

	rendered := strings.Join(report.render(), "\n")

	if !strings.Contains(rendered, "Save diverged from Save 2, run omnisave track to resolve") {
		t.Fatalf("expected the printed report to name the resolving command, got:\n%s", rendered)
	}
}

func TestAStreamedEventCarriesItsClockAndGame(t *testing.T) {
	at := time.Date(2026, 7, 25, 14, 7, 0, 0, time.Local)

	line := EventLine(Event{Glyph: "✓", Title: "Slay the Spire 2", Sentence: "synced with Save 1", At: at})

	expected := "  14:07  ✓ Slay the Spire 2  Synced with Save 1"
	if line != expected {
		t.Fatalf("expected %q, got %q", expected, line)
	}
}

func TestTrackReportStreamsSnapshotsToLiveViews(t *testing.T) {
	var snapshots []ReportSnapshot
	report := &TrackReport{OnUpdate: func(snapshot ReportSnapshot) {
		snapshots = append(snapshots, snapshot)
	}}
	report.UpToDate("Slay the Spire 2")
	report.SyncedWith("Slay the Spire 2", "Save 1", time.Now())

	if len(snapshots) < 2 {
		t.Fatalf("expected a snapshot per event, got %d", len(snapshots))
	}
	last := strings.Join(ComposeReport(snapshots[len(snapshots)-1], time.Now()), "\n")
	if !strings.Contains(last, "Save 1 · synced just now") {
		t.Fatalf("expected the final snapshot to carry the bound state, got:\n%s", last)
	}
}

func TestTrackReportExplainsWhenAnAddedGameHasNoSave(t *testing.T) {
	report := &TrackReport{}
	report.Added("Project Zomboid", "")
	report.NoSave("Project Zomboid")

	rendered := strings.Join(report.render(), "\n")
	expected := strings.Join([]string{
		"  + Project Zomboid",
		"      Added to the library",
		"      No save available",
	}, "\n")
	if rendered != expected {
		t.Fatalf("expected the missing save under the added game, got:\n%s", rendered)
	}
}

func TestTrackReportGivesSavelessGamesTheIdleGlyph(t *testing.T) {
	report := &TrackReport{}
	report.NoSave("Project Zomboid")

	rendered := strings.Join(report.render(), "\n")
	if rendered != "  · Project Zomboid\n      No save available" {
		t.Fatalf("expected the idle glyph on a saveless game, got:\n%s", rendered)
	}
}

func TestTrackReportNamesTheJumpPreservationFork(t *testing.T) {
	report := &TrackReport{}
	report.Forked("Chrono Trigger", "New Game+ (diverged)")
	report.SyncedWith("Chrono Trigger", "New Game+", time.Now())

	rendered := strings.Join(report.render(), "\n")
	expected := strings.Join([]string{
		"  ✓ Chrono Trigger  New Game+ · synced just now",
		"      Save forked as New Game+ (diverged)",
	}, "\n")
	if rendered != expected {
		t.Fatalf("expected the preservation fork under the bound state, got:\n%s", rendered)
	}
}

func TestTrackReportSaysWhereAnUnmatchedLocalSaveWasPreserved(t *testing.T) {
	report := &TrackReport{}
	report.PreservedAs("Chrono Trigger", "New Game+ (Steam Deck)")

	rendered := strings.Join(report.render(), "\n")
	if !strings.Contains(rendered, "Local save preserved as New Game+ (Steam Deck)") {
		t.Fatalf("expected the preservation sentence, got:\n%s", rendered)
	}
}

func TestTrackReportExplainsAServerDeletionUnderTheUntrackedGame(t *testing.T) {
	report := &TrackReport{}
	report.SaveDeleted("Chrono Trigger")
	report.Removed("Chrono Trigger")

	rendered := report.render()
	if len(rendered) != 3 || rendered[0] != "  - Chrono Trigger" {
		t.Fatalf("expected the removal glyph alone on the game line, got %q", rendered)
	}
	if rendered[1] != "      Bound Omnisave deleted on the server" || rendered[2] != "      Untracked from this device" {
		t.Fatalf("expected the deletion reason above the untrack event, got %q", rendered)
	}
}

func TestTrackReportSpeaksSyncExceptions(t *testing.T) {
	report := &TrackReport{}
	report.Diverged("Chrono Trigger", "New Game+", "New Game+ (Steam Deck)")
	report.Stale("Stardew Valley", "Farm run")
	report.CurrentMoved("Project Zomboid", "Save 2")
	report.PullDeferred("Slay the Spire 2", "Main")

	rendered := strings.Join(report.render(), "\n")
	for _, sentence := range []string{
		"Save diverged from New Game+, run omnisave track to resolve",
		"Save diverged from Farm run, run omnisave track to resolve",
		"Save 2 moved on the server; the next sync pass will reconcile",
		"Main · waiting for game to close",
	} {
		if !strings.Contains(rendered, sentence) {
			t.Fatalf("expected %q in the report, got:\n%s", sentence, rendered)
		}
	}
	if !strings.Contains(rendered, "  ○ Chrono Trigger") {
		t.Fatalf("expected the diverged game to carry the attention glyph, got:\n%s", rendered)
	}
}

func TestTrackReportKeepsRunWideFailuresAboveTheGameLines(t *testing.T) {
	report := &TrackReport{}
	report.Failed("Chrono Trigger", errors.New("resolve timed out"))
	report.BindingFailed(errors.New("listing Omnisaves failed"))

	rendered := report.render()
	if len(rendered) != 3 || !strings.Contains(rendered[0], "✗ Save binding failed — listing Omnisaves failed") {
		t.Fatalf("expected the run-wide failure first, got %q", rendered)
	}
	if !strings.HasPrefix(rendered[1], "  ✗ Chrono Trigger") || rendered[2] != "      Failed — resolve timed out" {
		t.Fatalf("expected the game glyphed on its line with the failure under it, got %q", rendered)
	}
}

func TestTrackReportLetsASaveFailureClaimTheGameGlyph(t *testing.T) {
	report := &TrackReport{}
	report.Added("Stardew Valley", "")
	report.SaveFailed("Stardew Valley", errors.New("upload rejected"))

	rendered := report.render()
	if len(rendered) != 3 || !strings.HasPrefix(rendered[0], "  ✗ Stardew Valley") {
		t.Fatalf("expected the failure to claim the game glyph, got %q", rendered)
	}
	if rendered[2] != "      Save failed — upload rejected" {
		t.Fatalf("expected the save failure under the game, got %q", rendered)
	}
}
