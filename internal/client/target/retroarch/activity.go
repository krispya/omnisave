package retroarch

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

// frontendName identifies a RetroArch frontend process by name wherever the
// install lives — including the installer-discovered case, where the target
// root is a config directory and the frontend binary is a system path like
// /usr/bin/retroarch that no root can claim.
const frontendName = "retroarch"

// RunningGames identifies live RetroArch games from content or save paths.
func (a *Adapter) RunningGames(ctx context.Context, snapshot *running.Snapshot, discovered target.Target, games []target.InstalledGame) (map[string]bool, error) {
	if discovered.Adapter != adapterName {
		return nil, fmt.Errorf("invalid RetroArch target")
	}
	roots := a.resolved.resolve(discovered.Root)
	var evidence []string
	for _, process := range snapshot.Processes() {
		if !isFrontend(process, roots) {
			continue
		}
		evidence = append(evidence, snapshot.Cmdline(ctx, process.PID)...)
		evidence = append(evidence, snapshot.OpenPaths(ctx, process.PID)...)
	}
	if len(evidence) == 0 {
		return map[string]bool{}, nil
	}
	playing := make(map[string]bool)
	for _, game := range games {
		if game.TargetID != discovered.ID {
			continue
		}
		for _, claim := range a.claimPaths(game) {
			if matchesAny(claim, evidence) {
				playing[game.ID] = true
				break
			}
		}
	}
	return playing, nil
}

// isFrontend recognizes a RetroArch frontend process, by executing under any
// form of the target's root or by carrying the frontend's name.
func isFrontend(process running.Process, roots []string) bool {
	if underAny(process.Executable, roots) {
		return true
	}
	base := process.Name
	if base == "" {
		base = filepath.Base(process.Executable)
	}
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return strings.EqualFold(base, frontendName)
}

// claimPaths returns original and resolved content and save paths for a game.
func (a *Adapter) claimPaths(game target.InstalledGame) []string {
	claims := slices.Clone(a.resolved.resolve(game.Identity.ContentPath))
	profile, ok := a.platform(game.Identity.Platform)
	if !ok {
		return claims
	}
	saveDirectory := game.Metadata["save_directory"]
	if saveDirectory == "" {
		saveDirectory = filepath.Dir(game.Identity.ContentPath)
	}
	base := filepath.Base(game.Identity.ContentPath)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	for _, directory := range a.resolved.resolve(saveDirectory) {
		for _, extension := range profile.SaveExtensions {
			claims = append(claims, filepath.Join(directory, name+extension))
		}
	}
	return claims
}

func matchesAny(claim string, evidence []string) bool {
	return slices.ContainsFunc(evidence, func(observed string) bool { return running.SamePath(observed, claim) })
}

func underAny(path string, roots []string) bool {
	return slices.ContainsFunc(roots, func(root string) bool { return running.UnderRoot(path, root) })
}

// resolver memoizes running.ResolveRoots per path. Roots and claim paths are
// fixed for a matcher's lifetime, but sweeps recur and resolution costs a
// stat per path segment — the first sweep pays it, every later sweep is a
// map lookup.
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
