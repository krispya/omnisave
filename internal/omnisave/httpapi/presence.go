package httpapi

import (
	"sync"
	"time"
)

// presenceTTL is how long a playing report stays credible without being
// re-affirmed. Devices re-affirm every minute while a game runs, so a report
// older than this belongs to a device that crashed or lost its network —
// staleness must unblock, never block.
const presenceTTL = 3 * time.Minute

// devicePresence remembers, in memory only, which games each device recently
// reported as being played. Presence is liveness, not provenance: a server
// restart forgets it, and the next re-affirmation rebuilds it.
type devicePresence struct {
	mu      sync.Mutex
	now     func() time.Time
	reports map[string]presenceReport
}

type presenceReport struct {
	playing map[string]bool
	at      time.Time
}

func newDevicePresence() *devicePresence {
	return &devicePresence{now: time.Now, reports: make(map[string]presenceReport)}
}

// report stores one device's playing set and returns whether the effective
// picture changed — what decides if the change is worth an event.
func (p *devicePresence) report(deviceID string, gameIDs []string) bool {
	playing := make(map[string]bool, len(gameIDs))
	for _, gameID := range gameIDs {
		if gameID != "" {
			playing[gameID] = true
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	previous, existed := p.reports[deviceID]
	p.reports[deviceID] = presenceReport{playing: playing, at: p.now()}
	if !existed {
		return len(playing) > 0
	}
	if p.now().Sub(previous.at) >= presenceTTL {
		// A stale report already reads as "not playing", so only a
		// non-empty successor changes what anyone sees.
		return len(playing) > 0
	}
	return !sameSet(previous.playing, playing)
}

// playing returns when deviceID last credibly reported gameID as being
// played; false once the report has gone stale.
func (p *devicePresence) playing(deviceID, gameID string) (time.Time, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	report, exists := p.reports[deviceID]
	if !exists || !report.playing[gameID] || p.now().Sub(report.at) >= presenceTTL {
		return time.Time{}, false
	}
	return report.at, true
}

func sameSet(left, right map[string]bool) bool {
	if len(left) != len(right) {
		return false
	}
	for key := range left {
		if !right[key] {
			return false
		}
	}
	return true
}
