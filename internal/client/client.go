// Package client coordinates local save adapters.
package client

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/client/running"
	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

// Scanner performs one read-only pass across local target adapters.
type Scanner struct {
	adapters []target.Adapter
	profiles saveprofile.Provider
}

// GameScan reports the native saves found for one installed game.
type GameScan struct {
	Game  target.InstalledGame
	Saves []target.Save
	// Destinations describe native save destinations whether or not files exist.
	Destinations []target.SaveDestination
	// Profile explains what the game's save profile did this pass. It is
	// recorded on every scan and read only by views that ask for it.
	Profile ProfileTrace
}

// ProfileTrace explains a game's save-location knowledge and what became of
// it: whether the community manifest knows the game at all, what each of its
// rules did, and whether the resolved files were set aside because the
// adapter's own save already held them. A scan that finds nothing is
// otherwise indistinguishable from a scan that never looked.
type ProfileTrace struct {
	// Consulted is false when the scanner had no profile provider.
	Consulted bool
	// Found reports whether the provider knew this game.
	Found bool
	// Provider, ProviderID, and Title identify the entry that answered.
	Provider   string
	ProviderID string
	Title      string
	// Rules is what each of the entry's rules did, in entry order.
	Rules []saveprofile.RuleOutcome
	// Suppressed reports that resolved files were dropped because an
	// adapter's own save already held the same save family.
	Suppressed bool
	// Err is a provider failure other than a plain miss.
	Err error
}

// TargetScan reports the installed games found through one application target.
type TargetScan struct {
	Target target.Target
	Games  []GameScan
}

// ScanStage describes an adapter's progress through one scan.
type ScanStage string

const (
	ScanStarted   ScanStage = "started"
	ScanCompleted ScanStage = "completed"
	ScanFailed    ScanStage = "failed"
)

// ScanProgress reports observable progress for one adapter.
type ScanProgress struct {
	Adapter string
	Stage   ScanStage
	Results []TargetScan
	Err     error
}

// NewScanner creates a one-shot scanner from save profiles and target adapters.
func NewScanner(profiles saveprofile.Provider, adapters ...target.Adapter) *Scanner {
	return &Scanner{adapters: adapters, profiles: profiles}
}

// AdapterNames returns configured adapters in scan order.
func (s *Scanner) AdapterNames() []string {
	names := make([]string, 0, len(s.adapters))
	for _, adapter := range s.adapters {
		names = append(names, adapter.Name())
	}
	return names
}

// Scan discovers available targets and their current saves without modifying them.
func (s *Scanner) Scan(ctx context.Context) ([]TargetScan, error) {
	return s.ScanWithProgress(ctx, nil)
}

// ScanWithProgress runs a scan and reports each adapter as it starts and finishes.
func (s *Scanner) ScanWithProgress(ctx context.Context, report func(ScanProgress)) ([]TargetScan, error) {
	var scans []TargetScan
	for _, adapter := range s.adapters {
		progress(report, ScanProgress{Adapter: adapter.Name(), Stage: ScanStarted})
		adapterScans, err := s.scanAdapter(ctx, adapter)
		if err != nil {
			progress(report, ScanProgress{Adapter: adapter.Name(), Stage: ScanFailed, Err: err})
			return nil, err
		}
		scans = append(scans, adapterScans...)
		progress(report, ScanProgress{Adapter: adapter.Name(), Stage: ScanCompleted, Results: adapterScans})
	}
	return scans, nil
}

func (s *Scanner) scanAdapter(ctx context.Context, adapter target.Adapter) ([]TargetScan, error) {
	targets, err := adapter.DiscoverTargets(ctx)
	if err != nil {
		return nil, fmt.Errorf("discover %s targets: %w", adapter.Name(), err)
	}

	var scans []TargetScan
	for _, discovered := range targets {
		games, err := adapter.DiscoverGames(ctx, discovered)
		if err != nil {
			return nil, fmt.Errorf("discover games in %s: %w", discovered.ID, err)
		}
		scan := TargetScan{Target: discovered}
		for _, game := range games {
			saves, err := adapter.DiscoverSaves(ctx, discovered, game)
			if err != nil {
				return nil, fmt.Errorf("discover saves for %s: %w", game.ID, err)
			}
			destinations, err := adapter.DiscoverSaveDestinations(ctx, discovered, game)
			if err != nil {
				return nil, fmt.Errorf("discover save locations for %s: %w", game.ID, err)
			}
			var trace ProfileTrace
			if s.profiles != nil {
				trace.Consulted = true
				profile, err := s.profiles.Find(ctx, game.Identity)
				if err != nil && !errors.Is(err, saveprofile.ErrNotFound) {
					return nil, fmt.Errorf("find save profile for %s: %w", game.ID, err)
				}
				if err == nil {
					trace.Found = true
					trace.Provider = profile.Provider
					trace.ProviderID = profile.ProviderID
					trace.Title = profile.Title
					resolved, ruleTrace, err := saveprofile.ResolveWithTrace(game, *profile)
					if err != nil {
						return nil, fmt.Errorf("resolve save profile for %s: %w", game.ID, err)
					}
					trace.Rules = ruleTrace
					// One representation per game: a save the adapter itself
					// found — Steam Cloud's mirror — carries the device-neutral
					// layout every Device shares, so the profile stands aside
					// when that save demonstrably holds the same save family:
					// files by the same names the profile rules locate.
					// Tracking both would sync the same progress as two saves
					// whose lineages can never converge (FDR-003, decision 10).
					// Mere existence is not enough — a mirror carrying only
					// auxiliary files, or a subset synced for another OS, would
					// otherwise silently displace the save that holds the real
					// progress.
					trace.Suppressed = adapterCoversProfile(saves, resolved)
					if !trace.Suppressed {
						saves = append(saves, resolved...)
						resolvedDestinations, err := saveprofile.ResolveDestinations(game, *profile)
						if err != nil {
							return nil, fmt.Errorf("resolve save profile locations for %s: %w", game.ID, err)
						}
						destinations = append(destinations, resolvedDestinations...)
					}
				}
			}
			scan.Games = append(scan.Games, GameScan{
				Game: game, Saves: saves, Destinations: destinations, Profile: trace,
			})
		}
		scans = append(scans, scan)
	}
	return scans, nil
}

func progress(report func(ScanProgress), event ScanProgress) {
	if report != nil {
		report(event)
	}
}

// adapterCoversProfile reports whether the adapter's own saves already carry
// the save family the profile rules resolved: some profile file exists in an
// adapter save under the same name. Names, not content, because the two
// representations hold the same family at different moments — a mirror can
// trail the native folder by a session — and because deciding must stay as
// cheap as the scan it runs in. A profile that resolved no files leaves
// nothing the adapter could be failing to cover.
func adapterCoversProfile(adapterSaves, profileSaves []target.Save) bool {
	names := make(map[string]bool)
	for _, save := range adapterSaves {
		for _, file := range save.Files {
			names[strings.ToLower(filepath.Base(file.Path))] = true
		}
	}
	if len(names) == 0 {
		return false
	}
	profileHasFiles := false
	for _, save := range profileSaves {
		for _, file := range save.Files {
			profileHasFiles = true
			if names[strings.ToLower(filepath.Base(file.Path))] {
				return true
			}
		}
	}
	return !profileHasFiles
}

// UnlockedAchievements asks the save's own adapter which achievements its
// target records as unlocked. An adapter that cannot see achievements reports
// none, which is how every save on such a target stays unmarked.
func (s *Scanner) UnlockedAchievements(
	ctx context.Context,
	discovered target.Target,
	game target.InstalledGame,
	save target.Save,
) ([]target.Achievement, error) {
	for _, adapter := range s.adapters {
		if adapter.Name() != discovered.Adapter {
			continue
		}
		source, supported := adapter.(target.Achievements)
		if !supported {
			return nil, nil
		}
		return source.UnlockedAchievements(ctx, discovered, game, save)
	}
	return nil, nil
}

// PlayingMatchers builds activity matchers for tracked games on supported targets.
func (s *Scanner) PlayingMatchers(scans []TargetScan, tracked func(gameID string) bool) []running.Matcher {
	adapters := make(map[string]target.Adapter, len(s.adapters))
	for _, adapter := range s.adapters {
		adapters[adapter.Name()] = adapter
	}
	var matchers []running.Matcher
	for _, scan := range scans {
		activity, ok := adapters[scan.Target.Adapter].(target.Activity)
		if !ok {
			continue
		}
		games := make([]target.InstalledGame, 0, len(scan.Games))
		for _, discovered := range scan.Games {
			if tracked(discovered.Game.ID) {
				games = append(games, discovered.Game)
			}
		}
		if len(games) == 0 {
			continue
		}
		discoveredTarget := scan.Target
		matchers = append(matchers, func(ctx context.Context, snapshot *running.Snapshot) (map[string]bool, error) {
			return activity.RunningGames(ctx, snapshot, discoveredTarget, games)
		})
	}
	return matchers
}
