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
// it: whether a source knew the game at all, which source answered, and what
// each of its rules did. A scan that finds nothing is otherwise
// indistinguishable from a scan that never looked.
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
	// RefusedMirror counts the locations a source offered inside a store's
	// cloud mirror, which are refused however they were arrived at.
	RefusedMirror int
	// Err is what a source said other than a plain miss: a failure, or its
	// own explanation for having no answer — Steam reporting that a game
	// keeps its cloud saves through the API and so has no folder anywhere.
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
			profileSaves, profileDestinations, trace, err := s.locateSaves(ctx, game)
			if err != nil {
				return nil, err
			}
			saves = append(saves, profileSaves...)
			destinations = append(destinations, profileDestinations...)
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

// locateSaves reports the saves and prospective save locations the game's
// save-location rules describe on this Device, and what those rules did.
//
// A game's saves live where the game itself reads and writes them, and rules
// are the only source that knows where that is: a store's cloud mirror is a
// transport and never a save (FDR-003, decision 10). When the primary source
// has no rule that applies here, a fallback source is consulted, so a game
// the community manifest cannot place is not left unprotected for want of an
// entry. Order matters and never reverses: the primary answers wherever it
// can, so a lineage already minted under its spelling of a location keeps it.
func (s *Scanner) locateSaves(
	ctx context.Context,
	game target.InstalledGame,
) ([]target.Save, []target.SaveDestination, ProfileTrace, error) {
	if s.profiles == nil {
		return nil, nil, ProfileTrace{}, nil
	}
	trace := ProfileTrace{Consulted: true}
	saves, destinations, err := s.applyProfile(ctx, s.profiles.Find, game, &trace)
	if err != nil {
		return nil, nil, trace, err
	}
	if anyRuleApplied(trace.Rules) {
		return saves, destinations, trace, nil
	}
	fallback, hasFallback := s.profiles.(saveprofile.FallbackProvider)
	if !hasFallback {
		return saves, destinations, trace, nil
	}
	// The primary's trace is what a report explains this game by, so it is
	// replaced only once the fallback has something to say in its place.
	fallbackTrace := ProfileTrace{Consulted: true}
	fallbackSaves, fallbackDestinations, err := s.applyProfile(
		ctx, fallback.FindFallback, game, &fallbackTrace)
	if err != nil {
		return nil, nil, trace, err
	}
	if !fallbackTrace.Found {
		// A fallback that cannot answer may still know why — that Steam
		// holds no folder for this game at all, say — which is the only
		// explanation this Device has for protecting nothing.
		if fallbackTrace.Err != nil && trace.Err == nil {
			trace.Err = fallbackTrace.Err
		}
		return saves, destinations, trace, nil
	}
	return fallbackSaves, fallbackDestinations, fallbackTrace, nil
}

// applyProfile finds and resolves one source's knowledge, recording what it
// did in trace. A source that does not know the game, or knows it keeps no
// save folder here, leaves the trace saying so; anything else is a failure
// the scan reports rather than reads as silence.
func (s *Scanner) applyProfile(
	ctx context.Context,
	find func(context.Context, target.GameIdentity) (*saveprofile.Profile, error),
	game target.InstalledGame,
	trace *ProfileTrace,
) ([]target.Save, []target.SaveDestination, error) {
	profile, err := find(ctx, game.Identity)
	switch {
	case errors.Is(err, saveprofile.ErrNotFound):
		return nil, nil, nil
	case errors.Is(err, saveprofile.ErrNoSaveFolder):
		// Not a failure: the source knows the game and knows it keeps no
		// save folder here, which is an answer a report needs to give.
		trace.Err = err
		return nil, nil, nil
	case err != nil:
		return nil, nil, fmt.Errorf("find save profile for %s: %w", game.ID, err)
	}
	trace.Found = true
	trace.Provider = profile.Provider
	trace.ProviderID = profile.ProviderID
	trace.Title = profile.Title
	saves, ruleTrace, err := saveprofile.ResolveWithTrace(game, *profile)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve save profile for %s: %w", game.ID, err)
	}
	destinations, err := saveprofile.ResolveDestinations(game, *profile)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve save profile locations for %s: %w", game.ID, err)
	}
	trace.Rules = ruleTrace
	// The last word on what a save is: whatever a source resolved, nothing
	// under a store's cloud mirror is a save or a place to put one, because
	// restoring there writes where the game may never read (FDR-003,
	// decision 10). Sources are meant to observe this themselves; enforcing
	// it here is what makes it a rule rather than a convention, and a
	// refusal is counted so a report can say it happened.
	saves, refusedSaves := refuseMirrorPaths(saves, game)
	destinations, refusedDestinations := refuseMirrorDestinations(destinations, game)
	trace.RefusedMirror += refusedSaves + refusedDestinations
	return saves, destinations, nil
}

// refuseMirrorPaths drops every file that lies inside a store's cloud
// mirror, and any save left holding nothing. Where a save may be placed does
// not wait for one to exist, so the same refusal covers destinations: a
// Device that has never played a game is offered the game's own folder and
// never the mirror (FDR-004).
func refuseMirrorPaths(saves []target.Save, game target.InstalledGame) ([]target.Save, int) {
	refused := 0
	kept := make([]target.Save, 0, len(saves))
	for _, save := range saves {
		files := make([]target.File, 0, len(save.Files))
		for _, file := range save.Files {
			if underCloudMirror(file.Path, game.Environment.StoreRoot) {
				refused++
				continue
			}
			files = append(files, file)
		}
		if len(files) == 0 {
			continue
		}
		save.Files = files
		kept = append(kept, save)
	}
	return kept, refused
}

func refuseMirrorDestinations(
	destinations []target.SaveDestination,
	game target.InstalledGame,
) ([]target.SaveDestination, int) {
	refused := 0
	kept := make([]target.SaveDestination, 0, len(destinations))
	for _, destination := range destinations {
		locations := make([]target.SaveLocation, 0, len(destination.Locations))
		for _, location := range destination.Locations {
			if underCloudMirror(location.Path, game.Environment.StoreRoot) {
				refused++
				continue
			}
			locations = append(locations, location)
		}
		if len(locations) == 0 {
			continue
		}
		destination.Locations = locations
		kept = append(kept, destination)
	}
	return kept, refused
}

// underCloudMirror reports whether a path lies in the store's own per-account
// area — `<store root>/userdata` and everything beneath it, which holds the
// cloud mirror a game's content is staged in. The whole tree is refused
// rather than the mirror directory alone: a location resolved from a mirror
// rule can land on an ancestor of it, which is no better a place to restore
// into, and nothing a game itself reads lives under there
// ([ADR-018](../../docs/adr/ADR-018-embedded-save-profiles.md) drops rules
// over it for the same reason).
func underCloudMirror(candidate, storeRoot string) bool {
	if storeRoot == "" {
		return false
	}
	relative, err := filepath.Rel(filepath.Clean(storeRoot), filepath.Clean(candidate))
	if err != nil {
		return false
	}
	segments := strings.Split(filepath.ToSlash(relative), "/")
	return len(segments) > 0 && strings.EqualFold(segments[0], "userdata")
}

// anyRuleApplied reports whether a source said anything about this Device.
// A rule that applied and found nothing has still spoken: the game simply has
// no save here yet, which a fallback source cannot improve on. A rule this
// environment cannot even expand has not spoken — it named a place this
// Device has no value for — so another source is still worth asking.
func anyRuleApplied(rules []saveprofile.RuleOutcome) bool {
	for _, rule := range rules {
		switch rule.Outcome {
		case saveprofile.OutcomeInapplicable, saveprofile.OutcomeUnexpandable:
			continue
		default:
			return true
		}
	}
	return false
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
