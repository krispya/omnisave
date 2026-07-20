package tui

import (
	"errors"
	"strings"
	"testing"
)

func TestTrackReportGivesEachGameOneAlignedLine(t *testing.T) {
	report := &TrackReport{}
	report.Added("Slay the Spire 2", "Slay the Spire II")
	report.Seeded("Slay the Spire 2", "Slay the Spire II save")
	report.Linked("Project Zomboid", "")
	report.Unbound("Project Zomboid")

	rendered := strings.Join(report.render(), "\n")
	expected := strings.Join([]string{
		"  + Slay the Spire 2  Added to the library as Slay the Spire II · Save seeded as Slay the Spire II save",
		"  ✓ Project Zomboid   Linked to the library · Save needs omnisave-client bind",
	}, "\n")
	if rendered != expected {
		t.Fatalf("expected one aligned line per game with dot-joined statuses, got:\n%s", rendered)
	}
}

func TestTrackReportShowsUpToDateGamesInsteadOfHidingThem(t *testing.T) {
	report := &TrackReport{}
	report.UpToDate("Slay the Spire 2")
	report.SyncedUp("Stardew Valley", "Farm run")

	rendered := strings.Join(report.render(), "\n")
	if !strings.Contains(rendered, "✓ Slay the Spire 2  Up to date") {
		t.Fatalf("expected an in-sync game to earn an up-to-date line, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Save synced to Farm run") {
		t.Fatalf("expected the synced game's status, got:\n%s", rendered)
	}
}

func TestTrackReportStreamsSnapshotsToLiveViews(t *testing.T) {
	var snapshots [][]string
	report := &TrackReport{OnUpdate: func(lines []string) {
		snapshots = append(snapshots, lines)
	}}
	report.UpToDate("Slay the Spire 2")
	report.SyncedUp("Slay the Spire 2", "Save 1")

	if len(snapshots) < 2 {
		t.Fatalf("expected a snapshot per event, got %d", len(snapshots))
	}
	last := strings.Join(snapshots[len(snapshots)-1], "\n")
	if !strings.Contains(last, "Save synced to Save 1") {
		t.Fatalf("expected the final snapshot to carry the latest status, got:\n%s", last)
	}
}

func TestTrackReportSeedingWithoutADisplayNameStaysGeneric(t *testing.T) {
	report := &TrackReport{}
	report.Seeded("Slay the Spire 2", "")

	rendered := strings.Join(report.render(), "\n")
	if !strings.Contains(rendered, "Save seeded to a new Omnisave") {
		t.Fatalf("expected an unnamed seed to fall back to the generic sentence, got %q", rendered)
	}
}

func TestTrackReportExplainsWhenAnAddedGameHasNoSave(t *testing.T) {
	report := &TrackReport{}
	report.Added("Project Zomboid", "")
	report.NoSave("Project Zomboid")

	rendered := strings.Join(report.render(), "\n")
	if rendered != "  + Project Zomboid  Added to the library · No save available" {
		t.Fatalf("expected the missing save beside the added game, got:\n%s", rendered)
	}
}

func TestTrackReportGivesSavelessGamesTheIdleGlyph(t *testing.T) {
	report := &TrackReport{}
	report.NoSave("Project Zomboid")

	rendered := strings.Join(report.render(), "\n")
	if rendered != "  · Project Zomboid  No save available" {
		t.Fatalf("expected the idle glyph on a saveless game, got:\n%s", rendered)
	}
}

func TestTrackReportShowsAnAutomaticExistingSaveMatch(t *testing.T) {
	report := &TrackReport{}
	report.Rebound("Chrono Trigger", "New Game+")

	rendered := report.render()
	if len(rendered) != 1 || !strings.HasPrefix(rendered[0], "  ✓ Chrono Trigger") ||
		!strings.Contains(rendered[0], "Save matches the head of New Game+ and is resyncing") {
		t.Fatalf("expected the automatic match beside the game, got %q", rendered)
	}
}

func TestTrackReportNamesBothStaleMatchOutcomes(t *testing.T) {
	report := &TrackReport{}
	report.FastForwarded("Chrono Trigger", "New Game+")
	report.Forked("Chrono Trigger", "New Game+ (fork)")

	rendered := strings.Join(report.render(), "\n")
	if !strings.Contains(rendered, "Save jumped to the head of New Game+") ||
		!strings.Contains(rendered, "Save forked as New Game+ (fork)") {
		t.Fatalf("expected both stale-match outcomes to name their Omnisaves, got %q", rendered)
	}
}

func TestTrackReportExplainsAServerDeletionOnTheUntrackLine(t *testing.T) {
	report := &TrackReport{}
	report.SaveDeleted("Chrono Trigger")
	report.Removed("Chrono Trigger")

	rendered := report.render()
	if len(rendered) != 1 || !strings.HasPrefix(rendered[0], "  - Chrono Trigger") {
		t.Fatalf("expected the removal glyph on the game line, got %q", rendered)
	}
	if !strings.Contains(rendered[0], "Bound Omnisave deleted on the server · Untracked from this device") {
		t.Fatalf("expected the deletion reason beside the untrack status, got %q", rendered)
	}
}

func TestTrackReportSpeaksSyncInBothDirections(t *testing.T) {
	report := &TrackReport{}
	report.SyncedUp("Slay the Spire 2", "Save 1")
	report.SyncedDown("Stardew Valley", "Farm run")
	report.Diverged("Chrono Trigger", "New Game+")

	rendered := strings.Join(report.render(), "\n")
	for _, sentence := range []string{
		"Save synced to Save 1",
		"Save synced from Farm run",
		"Save diverged from New Game+, run omnisave-client track to resolve",
	} {
		if !strings.Contains(rendered, sentence) {
			t.Fatalf("expected %q in the sync report, got:\n%s", sentence, rendered)
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
	if len(rendered) != 2 || !strings.Contains(rendered[0], "✗ Save binding failed — listing Omnisaves failed") {
		t.Fatalf("expected the run-wide failure first, got %q", rendered)
	}
	if !strings.HasPrefix(rendered[1], "  ✗ Chrono Trigger") ||
		!strings.Contains(rendered[1], "Failed — resolve timed out") {
		t.Fatalf("expected the game failure glyphed on its line, got %q", rendered)
	}
}

func TestTrackReportLetsASaveFailureClaimTheGameGlyph(t *testing.T) {
	report := &TrackReport{}
	report.Added("Stardew Valley", "")
	report.SaveFailed("Stardew Valley", errors.New("upload rejected"))

	rendered := report.render()
	if len(rendered) != 1 || !strings.HasPrefix(rendered[0], "  ✗ Stardew Valley") {
		t.Fatalf("expected the failure to claim the game glyph, got %q", rendered)
	}
	if !strings.Contains(rendered[0], "Save failed — upload rejected") {
		t.Fatalf("expected the save failure beside the game, got %q", rendered)
	}
}
