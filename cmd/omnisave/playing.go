package main

import (
	"context"
	"slices"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/remote"
	"github.com/krisbaumgartner/omnisave/internal/client/running"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
)

// presenceWatch contains the state needed to refresh playing reports between passes.
type presenceWatch struct {
	deviceID string
	matchers []running.Matcher
	serverID map[string]string
	titles   map[string]string
}

// trackedPresence assembles presence state after Library identities are resolved.
func trackedPresence(scanner *client.Scanner, state *tracking.State, scans []client.TargetScan) presenceWatch {
	presence := presenceWatch{
		deviceID: state.Device.ID,
		serverID: make(map[string]string),
		titles:   make(map[string]string),
	}
	tracked := func(gameID string) bool {
		_, ok := state.Games[gameID]
		return ok
	}
	presence.matchers = scanner.PlayingMatchers(scans, tracked)
	for _, scan := range scans {
		for _, discovered := range scan.Games {
			if !tracked(discovered.Game.ID) {
				continue
			}
			presence.serverID[discovered.Game.ID] = state.Games[discovered.Game.ID].ServerGameID
			presence.titles[discovered.Game.ID] = discovered.Game.Identity.DisplayTitle(discovered.Game.ID)
		}
	}
	return presence
}

// passPlaying holds a shared process sweep, its presence report, and deferred pulls.
// swept distinguishes a valid empty result from a failed sweep.
type passPlaying struct {
	presence presenceWatch
	playing  map[string]bool
	swept    bool
	waiting  []string
}

// pullGate defers pulls for running games and records which exits unblock them.
// A nil gate fails open and defers nothing.
type pullGate struct {
	playing map[string]bool
	waiting []string
}

// holdPull reports whether the game's pull must wait for it to close,
// recording the game as waiting when it must.
func (g *pullGate) holdPull(gameID string) bool {
	if g == nil || !g.playing[gameID] {
		return false
	}
	if !slices.Contains(g.waiting, gameID) {
		g.waiting = append(g.waiting, gameID)
	}
	return true
}

// deferredGameExited reports whether a game holding back a pull has closed
// since the pass that deferred it — the moment that pull can safely land.
func deferredGameExited(deferred []string, playing map[string]bool) bool {
	for _, gameID := range deferred {
		if !playing[gameID] {
			return true
		}
	}
	return false
}

// playingNow reports running tracked games from one process sweep.
// Callers must not treat a failed sweep as an empty presence report.
func playingNow(ctx context.Context, detector *running.Detector, matchers []running.Matcher) (map[string]bool, error) {
	if detector == nil || len(matchers) == 0 {
		return nil, nil
	}
	return detector.Playing(ctx, matchers...)
}

// playingGames maps and sorts a process sweep for stable reports.
func playingGames(lookup map[string]string, playing map[string]bool) []string {
	names := make([]string, 0, len(playing))
	for localID, isPlaying := range playing {
		if isPlaying && lookup[localID] != "" {
			names = append(names, lookup[localID])
		}
	}
	slices.Sort(names)
	return names
}

// reportPlaying sends best-effort presence; failures do not fail the sync pass.
func reportPlaying(ctx context.Context, server *remote.Client, presence presenceWatch, playing map[string]bool) {
	if presence.deviceID == "" {
		return
	}
	_ = server.ReportDeviceStatus(ctx, presence.deviceID, playingGames(presence.serverID, playing))
}
