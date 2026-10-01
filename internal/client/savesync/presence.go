package savesync

import (
	"context"
	"slices"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/running"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
)

// Presence is what a pass learned about telling which tracked games are being
// played on this Device: the matchers a process sweep runs, and how a local
// game is named to the server and to a person. It outlives the pass that
// built it, so presence can be re-affirmed between passes. The zero Presence
// has nothing to sweep and nobody to report for.
type Presence struct {
	deviceID  string
	matchers  []running.Matcher
	serverIDs map[string]string
	titles    map[string]string
}

// TrackedPresence builds presence for the tracked games in scans. It is built
// after SyncTracking, so every game it can name to the server has its
// Library identity.
func TrackedPresence(adapters Adapters, state *tracking.State, scans []client.TargetScan) Presence {
	presence := Presence{
		deviceID:  state.Device.ID,
		serverIDs: make(map[string]string),
		titles:    make(map[string]string),
	}
	tracked := func(gameID string) bool {
		_, ok := state.Games[gameID]
		return ok
	}
	if adapters != nil {
		presence.matchers = adapters.PlayingMatchers(scans, tracked)
	}
	for _, scan := range scans {
		for _, discovered := range scan.Games {
			if !tracked(discovered.Game.ID) {
				continue
			}
			presence.serverIDs[discovered.Game.ID] = state.Games[discovered.Game.ID].ServerGameID
			presence.titles[discovered.Game.ID] = discovered.Game.Identity.DisplayTitle(discovered.Game.ID)
		}
	}
	return presence
}

// Reportable reports whether this presence names a Device the server can
// hear it for. A pass that failed before it knew its Device leaves an
// unreportable presence, and the caller keeps the last reportable one.
func (p Presence) Reportable() bool {
	return p.deviceID != ""
}

// Sweep reports which tracked games are running, by local game ID, from one
// process sweep. A failed sweep is an error, never an empty report: an empty
// report would clear games still being played. A nil detector, or nothing to
// match, sweeps nothing.
func (p Presence) Sweep(ctx context.Context, detector *running.Detector) (map[string]bool, error) {
	if detector == nil || len(p.matchers) == 0 {
		return nil, nil
	}
	return detector.Playing(ctx, p.matchers...)
}

// Titles is a sweep's running games by display title, sorted for a stable
// view.
func (p Presence) Titles(playing map[string]bool) []string {
	return mapPlaying(p.titles, playing)
}

// Report tells the server which tracked games this Device sees being played.
// It is best-effort: a failed report never fails a pass.
func (p Presence) Report(ctx context.Context, server Server, playing map[string]bool) {
	if !p.Reportable() {
		return
	}
	_ = server.ReportDeviceStatus(ctx, p.deviceID, mapPlaying(p.serverIDs, playing))
}

func mapPlaying(lookup map[string]string, playing map[string]bool) []string {
	names := make([]string, 0, len(playing))
	for localID, isPlaying := range playing {
		if isPlaying && lookup[localID] != "" {
			names = append(names, lookup[localID])
		}
	}
	slices.Sort(names)
	return names
}

// Played is one pass's process sweep: the presence it swept with, which
// tracked games were running, and whose pulls it deferred because of it.
type Played struct {
	Presence Presence
	// Playing is the sweep's running games by local game ID.
	Playing map[string]bool
	// Swept distinguishes a sweep that found nothing from one that failed or
	// never ran; only a swept result may replace a standing presence report.
	Swept bool
	// Waiting lists games whose pull this pass deferred. Each one's exit is
	// the moment its pull can safely land.
	Waiting []string
}

// PullGate defers pulls for running games, so a game cannot overwrite a
// placed revision from memory (FDR-005, decision 8), and records which games
// are waiting. A nil gate defers nothing: detection is best-effort, and the
// pre-placement check remains the safety boundary.
type PullGate struct {
	playing map[string]bool
	waiting []string
}

// NewPullGate gates pulls on one sweep's running games, by local game ID.
func NewPullGate(playing map[string]bool) *PullGate {
	return &PullGate{playing: playing}
}

// Waiting lists the games whose pulls the gate held back.
func (g *PullGate) Waiting() []string {
	if g == nil {
		return nil
	}
	return g.waiting
}

// holdPull reports whether the game's pull must wait for it to close,
// recording the game as waiting when it must.
func (g *PullGate) holdPull(gameID string) bool {
	if g == nil || !g.playing[gameID] {
		return false
	}
	if !slices.Contains(g.waiting, gameID) {
		g.waiting = append(g.waiting, gameID)
	}
	return true
}
