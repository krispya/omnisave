package savesync

import (
	"path/filepath"
	"slices"
	"sort"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/tracking"
)

// TrackableGames is every game a scan found, as the local identity tracking
// would retain for it.
func TrackableGames(scans []client.TargetScan) []tracking.Game {
	var games []tracking.Game
	for _, scan := range scans {
		for _, discovered := range scan.Games {
			games = append(games, tracking.Game{
				ID:       discovered.Game.ID,
				Adapter:  scan.Target.Adapter,
				TargetID: scan.Target.ID,
				Title:    discovered.Game.Identity.DisplayTitle(discovered.Game.ID),
			})
		}
	}
	return games
}

// LocalSaves is every save a scan found, as bindable identities.
func LocalSaves(scans []client.TargetScan) []tracking.LocalSave {
	var saves []tracking.LocalSave
	for _, scan := range scans {
		for _, discovered := range scan.Games {
			for _, save := range discovered.Saves {
				saves = append(saves, LocalSaveFrom(scan, discovered, save))
			}
		}
	}
	return saves
}

// LocalSaveFrom describes one discovered native save as the identity its
// binding and every other tracked fact about it are keyed by.
func LocalSaveFrom(scan client.TargetScan, discovered client.GameScan, save target.Save) tracking.LocalSave {
	local := tracking.LocalSave{
		ID:        save.ID,
		Adapter:   scan.Target.Adapter,
		TargetID:  scan.Target.ID,
		GameID:    discovered.Game.ID,
		GameTitle: discovered.Game.Identity.DisplayTitle(discovered.Game.ID),
		Kind:      save.Kind,
		FileCount: len(save.Files),
	}
	for _, file := range save.Files {
		local.Size += file.Size
	}
	return local
}

// WatchedFiles is every path whose change should start a pass: each tracked
// save's files and their parent directories, so a new file is noticed, and
// every prospective save location, so a game's first save is noticed too.
func WatchedFiles(state *tracking.State, scans []client.TargetScan) []string {
	paths := make(map[string]bool)
	for _, scan := range scans {
		for _, discovered := range scan.Games {
			if _, tracked := state.Games[discovered.Game.ID]; !tracked {
				continue
			}
			for _, save := range discovered.Saves {
				for _, file := range save.Files {
					paths[file.Path] = true
					paths[filepath.Dir(file.Path)] = true
				}
			}
			for _, destination := range discovered.Destinations {
				for _, location := range destination.Locations {
					// Missing paths are watched too: a directory's mtime moves
					// when a save first appears in it.
					paths[location.Path] = true
				}
			}
		}
	}
	sorted := make([]string, 0, len(paths))
	for path := range paths {
		sorted = append(sorted, path)
	}
	sort.Strings(sorted)
	return sorted
}

func installedGameIdentities(scans []client.TargetScan) map[string]target.GameIdentity {
	identities := make(map[string]target.GameIdentity, len(scans))
	for _, scan := range scans {
		for _, discovered := range scan.Games {
			identities[discovered.Game.ID] = discovered.Game.Identity
		}
	}
	return identities
}

func sortedGameIDs(games map[string]tracking.Game) []string {
	ids := make([]string, 0, len(games))
	for id := range games {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
