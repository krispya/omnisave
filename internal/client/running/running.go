// Package running detects whether an installed game is being played right
// now. The package owns the process mechanics — one shared sweep per check,
// zombie filtering, and a stop grace that keeps a brief process flicker from
// reading as "stopped playing" — while adapters own the meaning: each knows
// what a running game looks like for its store on this platform, and answers
// through a Matcher over the shared Snapshot.
package running

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Process is one live, runnable process. Name is the short process name the
// OS reports, which can differ from the executable's basename — under Wine
// the name keeps the .exe while the executable points at the loader.
type Process struct {
	PID        int32
	Name       string
	Executable string
}

// Provider enumerates live processes and answers per-process questions. The
// platform provider is the real one; tests supply their own. Cmdline and
// OpenPaths are separated from Processes because they cost extra syscalls per
// process — a matcher asks them only about the few processes it cares about.
type Provider interface {
	Processes(ctx context.Context) ([]Process, error)
	Cmdline(ctx context.Context, pid int32) ([]string, error)
	OpenPaths(ctx context.Context, pid int32) ([]string, error)
}

// Snapshot is one sweep of the process table, shared by every matcher in a
// check. Per-process answers are fetched lazily and cached, so two matchers
// asking about the same process cost one syscall.
type Snapshot struct {
	provider  Provider
	processes []Process
	cmdlines  map[int32][]string
	openPaths map[int32][]string
}

// Processes returns every live process in the snapshot.
func (s *Snapshot) Processes() []Process {
	return s.processes
}

// Cmdline returns a process's arguments. A process that vanished since the
// sweep, or refuses its arguments, yields none — it cannot be evidence.
func (s *Snapshot) Cmdline(ctx context.Context, pid int32) []string {
	if cached, ok := s.cmdlines[pid]; ok {
		return cached
	}
	arguments, err := s.provider.Cmdline(ctx, pid)
	if err != nil {
		arguments = nil
	}
	s.cmdlines[pid] = arguments
	return arguments
}

// OpenPaths returns the filesystem paths a process holds open, including its
// working directory. Platforms that cannot answer cheaply yield none.
func (s *Snapshot) OpenPaths(ctx context.Context, pid int32) []string {
	if cached, ok := s.openPaths[pid]; ok {
		return cached
	}
	paths, err := s.provider.OpenPaths(ctx, pid)
	if err != nil {
		paths = nil
	}
	s.openPaths[pid] = paths
	return paths
}

// Matcher reports which game IDs have a live process in the snapshot.
// Adapters supply these: only the adapter knows what running means for its
// store on this device.
type Matcher func(ctx context.Context, snapshot *Snapshot) (map[string]bool, error)

// StopGrace is how long a game stays "playing" after its process disappears
// from a sweep. Process tables flicker — a game relaunching itself, a Proton
// prefix tearing down and back up — and without grace every flicker would
// flap the playing state on and off.
const StopGrace = 6 * time.Second

// Detector answers which games have a live process, smoothing sweeps with
// the stop grace.
type Detector struct {
	provider Provider
	grace    time.Duration
	now      func() time.Time
	lastSeen map[string]time.Time
}

// NewDetector builds a detector over any process source.
func NewDetector(provider Provider) *Detector {
	return &Detector{
		provider: provider,
		grace:    StopGrace,
		now:      time.Now,
		lastSeen: make(map[string]time.Time),
	}
}

// Playing reports the IDs of the games with a live process, from one process
// sweep shared by every matcher, holding recently-seen games through the
// stop grace.
func (d *Detector) Playing(ctx context.Context, matchers ...Matcher) (map[string]bool, error) {
	processes, err := d.provider.Processes(ctx)
	if err != nil {
		return nil, err
	}
	snapshot := &Snapshot{
		provider:  d.provider,
		processes: processes,
		cmdlines:  make(map[int32][]string),
		openPaths: make(map[int32][]string),
	}
	playing := make(map[string]bool)
	for _, matcher := range matchers {
		matched, err := matcher(ctx, snapshot)
		if err != nil {
			return nil, err
		}
		for id, live := range matched {
			if live {
				playing[id] = true
			}
		}
	}
	now := d.now()
	for id := range playing {
		d.lastSeen[id] = now
	}
	for id, seen := range d.lastSeen {
		if playing[id] {
			continue
		}
		if now.Sub(seen) < d.grace {
			playing[id] = true
		} else {
			delete(d.lastSeen, id)
		}
	}
	return playing, nil
}

// ResolveRoots returns roots with their symlink-resolved forms added:
// process sweeps report fully resolved executable paths, while adapters may
// know an install through a symlink — a linked Steam library, or macOS's
// /var living under /private. Resolution costs a stat per path segment, so
// callers resolve once per game, never per process.
func ResolveRoots(roots []string) []string {
	resolved := make([]string, 0, 2*len(roots))
	for _, root := range roots {
		if root == "" {
			continue
		}
		resolved = append(resolved, root)
		if canonical, err := filepath.EvalSymlinks(root); err == nil && canonical != root {
			resolved = append(resolved, canonical)
		}
	}
	return resolved
}

// UnderRoot reports whether a path lives at or below a root. The comparison
// folds case where filesystems do, and only crosses whole path segments, so
// a root never claims its similarly-named sibling. A root that is itself a
// filesystem root — "/" or a bare drive — already ends in the separator and
// claims everything beneath it.
func UnderRoot(path, root string) bool {
	if path == "" || root == "" {
		return false
	}
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if foldCase {
		path = strings.ToLower(path)
		root = strings.ToLower(root)
	}
	if path == root {
		return true
	}
	separator := string(filepath.Separator)
	if !strings.HasSuffix(root, separator) {
		root += separator
	}
	return strings.HasPrefix(path, root)
}

// SamePath reports whether two paths name the same file, under the same
// cleaning and case rules as UnderRoot.
func SamePath(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if foldCase {
		left = strings.ToLower(left)
		right = strings.ToLower(right)
	}
	return left == right
}

// foldCase matches the default filesystem behavior of the platforms whose
// filesystems ignore case.
var foldCase = runtime.GOOS == "darwin" || runtime.GOOS == "windows"
