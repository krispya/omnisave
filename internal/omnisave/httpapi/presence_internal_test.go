package httpapi

import (
	"testing"
	"time"
)

func TestAPlayingReportAgesOutInsteadOfBlockingForever(t *testing.T) {
	now := time.Now()
	presence := newDevicePresence(nil)
	presence.now = func() time.Time { return now }

	if !presence.report("device-1", []string{"game-1"}) {
		t.Fatal("expected the first playing report to read as a change")
	}
	if _, playing := presence.playing("device-1", "game-1"); !playing {
		t.Fatal("expected a fresh report to read as playing")
	}

	now = now.Add(presenceTTL)
	if _, playing := presence.playing("device-1", "game-1"); playing {
		t.Fatal("expected a report older than the credibility window to clear itself")
	}
	// A stale report already reads as "not playing", so clearing it is not
	// news — only a live session is.
	if presence.report("device-1", nil) {
		t.Fatal("expected clearing a stale report to publish nothing")
	}
	if !presence.report("device-1", []string{"game-1"}) {
		t.Fatal("expected a new session after staleness to read as a change")
	}
}

func TestAStaleReportIsPrunedByTheNextReport(t *testing.T) {
	now := time.Now()
	presence := newDevicePresence(nil)
	presence.now = func() time.Time { return now }

	presence.report("device-1", []string{"game-1"})
	now = now.Add(presenceTTL)
	presence.report("device-2", []string{"game-2"})

	presence.mu.Lock()
	_, kept := presence.reports["device-1"]
	presence.mu.Unlock()
	if kept {
		t.Fatal("expected a later report to prune the expired entry")
	}
	if _, playing := presence.playing("device-2", "game-2"); !playing {
		t.Fatal("expected the fresh report to survive the pruning")
	}
}

func TestAgingOutALiveSessionIsAnnouncedAsAChange(t *testing.T) {
	now := time.Now()
	announced := 0
	presence := newDevicePresence(func() { announced++ })
	presence.now = func() time.Time { return now }

	presence.report("device-1", []string{"game-1"})
	presence.report("device-2", nil)

	// Not yet: nothing has aged out, so the sweep has nothing to say.
	presence.expireStale()
	if announced != 0 {
		t.Fatalf("expected no announcement while the session is credible, got %d", announced)
	}

	now = now.Add(presenceTTL)
	presence.expireStale()
	if announced != 1 {
		t.Fatalf("expected aging out the live session to announce once, got %d", announced)
	}
	presence.mu.Lock()
	remaining := len(presence.reports)
	presence.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("expected the sweep to drop every stale report, kept %d", remaining)
	}

	// The idle device's empty report aged out too, but that changed nothing
	// anyone could see — one announcement is the right total.
	presence.expireStale()
	if announced != 1 {
		t.Fatalf("expected no further announcements from an empty store, got %d", announced)
	}
}

func TestReaffirmingTheSamePictureIsNotAChange(t *testing.T) {
	presence := newDevicePresence(nil)
	presence.report("device-1", []string{"game-1"})
	if presence.report("device-1", []string{"game-1"}) {
		t.Fatal("expected a re-affirmation to publish nothing")
	}
	if !presence.report("device-1", nil) {
		t.Fatal("expected the session ending to read as a change")
	}
	if _, playing := presence.playing("device-1", "game-1"); playing {
		t.Fatal("expected the cleared report to stop reading as playing")
	}
}
