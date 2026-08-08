package steam

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/krisbaumgartner/omnisave/internal/client/running"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

// reaperProcess is the wrapper Steam on Linux launches every title through,
// native and Proton alike. Its arguments carry the exact app being played,
// which survives every case where executable paths lie — Proton games whose
// live process is a Wine loader, pressure-vessel containers remapping paths.
const reaperProcess = "reaper"

// appIDArgumentPrefix marks the reaper argument naming the launched app,
// as in "AppId=1091500".
const appIDArgumentPrefix = "AppId="

// RunningGames reports which installed Steam games have a live process. The
// launch wrapper's AppId is the primary evidence; a process executing under
// the game's install root corroborates on the platforms Steam launches
// natively, where no wrapper exists.
func (a *Adapter) RunningGames(ctx context.Context, snapshot *running.Snapshot, discovered target.Target, games []target.InstalledGame) (map[string]bool, error) {
	if discovered.Adapter != adapterName {
		return nil, fmt.Errorf("invalid Steam target")
	}
	launched := launchedAppIDs(ctx, snapshot)
	playing := make(map[string]bool)
	for _, game := range games {
		if game.TargetID != discovered.ID {
			continue
		}
		if appID, ok := game.Identity.Identifier("steam.app"); ok && launched[appID] {
			playing[game.ID] = true
			continue
		}
		roots := a.resolved.resolve(game.InstallRoot)
		for _, process := range snapshot.Processes() {
			if underAny(process.Executable, roots) {
				playing[game.ID] = true
				break
			}
		}
	}
	return playing, nil
}

// launchedAppIDs reads the app IDs out of every live launch wrapper. Only
// wrapper processes pay the argument read; everything else is skipped by
// name.
func launchedAppIDs(ctx context.Context, snapshot *running.Snapshot) map[string]bool {
	launched := make(map[string]bool)
	for _, process := range snapshot.Processes() {
		if process.Name != reaperProcess && filepath.Base(process.Executable) != reaperProcess {
			continue
		}
		for _, argument := range snapshot.Cmdline(ctx, process.PID) {
			if appID, ok := strings.CutPrefix(argument, appIDArgumentPrefix); ok && appID != "" {
				launched[appID] = true
			}
		}
	}
	return launched
}

func underAny(path string, roots []string) bool {
	return slices.ContainsFunc(roots, func(root string) bool { return running.UnderRoot(path, root) })
}

// resolver memoizes running.ResolveRoots per path. Install roots are fixed
// for a matcher's lifetime, but sweeps recur and resolution costs a stat per
// path segment — the first sweep pays it, every later sweep is a map lookup.
type resolver struct {
	mu    sync.Mutex
	cache map[string][]string
}

// resolve returns a path with its symlink-resolved form added, as
// running.ResolveRoots does. Callers must not modify the returned slice.
func (r *resolver) resolve(path string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cached, ok := r.cache[path]; ok {
		return cached
	}
	if r.cache == nil {
		r.cache = make(map[string][]string)
	}
	resolved := running.ResolveRoots([]string{path})
	r.cache[path] = resolved
	return resolved
}

var _ target.Activity = (*Adapter)(nil)
