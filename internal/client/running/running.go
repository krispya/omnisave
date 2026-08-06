// Package running detects whether an installed game is being played right
// now. A game counts as running while any live process executes from inside
// one of its install locations — the adapter knows where the game lives, so
// no process-name heuristics are involved.
package running

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
)

// Process is one live process that carries an executable on disk.
type Process struct {
	PID        int32
	Executable string
}

// Lister enumerates live processes and their executable paths. The platform
// lister is the real one; tests supply their own.
type Lister interface {
	Processes(ctx context.Context) ([]Process, error)
}

// Game names the install locations that identify one game's processes: its
// own install root, and the root of the target that runs it — an emulated
// game's live process is the frontend's, which lives under the target.
type Game struct {
	ID    string
	Roots []string
}

// Detector answers which games have a live process.
type Detector struct {
	lister Lister
}

// NewDetector builds a detector over any process source.
func NewDetector(lister Lister) *Detector {
	return &Detector{lister: lister}
}

// Playing reports the IDs of the games with a live process, from one process
// sweep shared by every game.
func (d *Detector) Playing(ctx context.Context, games []Game) (map[string]bool, error) {
	processes, err := d.lister.Processes(ctx)
	if err != nil {
		return nil, err
	}
	playing := make(map[string]bool)
	for _, process := range processes {
		for _, game := range games {
			if playing[game.ID] {
				continue
			}
			for _, root := range gameRoots(game) {
				if executesUnder(process.Executable, root) {
					playing[game.ID] = true
					break
				}
			}
		}
	}
	return playing, nil
}

// gameRoots returns a game's roots with their symlink-resolved forms added:
// process sweeps report fully resolved executable paths, while adapters may
// know an install through a symlink — a linked Steam library, or macOS's
// /var living under /private.
func gameRoots(game Game) []string {
	roots := make([]string, 0, 2*len(game.Roots))
	for _, root := range game.Roots {
		if root == "" {
			continue
		}
		roots = append(roots, root)
		if resolved, err := filepath.EvalSymlinks(root); err == nil && resolved != root {
			roots = append(roots, resolved)
		}
	}
	return roots
}

// executesUnder reports whether an executable path lives at or below a root.
// The comparison folds case where filesystems do, and only crosses whole
// path segments, so a root never claims its similarly-named sibling.
func executesUnder(executable, root string) bool {
	if executable == "" || root == "" {
		return false
	}
	executable = filepath.Clean(executable)
	root = filepath.Clean(root)
	if foldCase {
		executable = strings.ToLower(executable)
		root = strings.ToLower(root)
	}
	return executable == root || strings.HasPrefix(executable, root+string(filepath.Separator))
}

// foldCase matches the default filesystem behavior of the platforms whose
// filesystems ignore case.
var foldCase = runtime.GOOS == "darwin" || runtime.GOOS == "windows"
