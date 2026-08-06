package main

import (
	"context"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/running"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
)

// trackedRunningGames lists the tracked games a process sweep can identify:
// each with its own install root and its target's, so an emulated game is
// recognized by its frontend's process.
func trackedRunningGames(state *tracking.State, scans []client.TargetScan) []running.Game {
	var games []running.Game
	for _, scan := range scans {
		for _, discovered := range scan.Games {
			if _, tracked := state.Games[discovered.Game.ID]; !tracked {
				continue
			}
			games = append(games, running.Game{
				ID:    discovered.Game.ID,
				Roots: []string{discovered.Game.InstallRoot, scan.Target.Root},
			})
		}
	}
	return games
}

// playingNow reports which of the games are being played, from one process
// sweep. Detection is best-effort and fails open: a sweep that errors gates
// nothing, because the staged placement re-verifies local content anyway and
// a platform whose process list is unreadable must not defer pulls forever.
func playingNow(ctx context.Context, detector *running.Detector, games []running.Game) map[string]bool {
	if detector == nil || len(games) == 0 {
		return nil
	}
	playing, err := detector.Playing(ctx, games)
	if err != nil {
		return nil
	}
	return playing
}
