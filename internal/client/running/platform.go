package running

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"

	"github.com/shirou/gopsutil/v4/process"
)

// PlatformDetector detects against this machine's live processes.
func PlatformDetector() *Detector {
	return NewDetector(&platformProvider{known: make(map[int32]knownProcess)})
}

// platformProvider reads the live process table, remembering executable
// paths between sweeps: a process's executable never changes while it lives,
// and re-reading every path each sweep was measured (in prior art) to be the
// bulk of a detector's idle cost.
type platformProvider struct {
	mu    sync.Mutex
	known map[int32]knownProcess
}

// knownProcess caches the immutable identity of one PID. The creation time
// guards against PID reuse: a recycled PID is a different process.
type knownProcess struct {
	created    int64
	name       string
	executable string
}

// Processes lists live processes that expose an executable path. Processes
// that refuse theirs — kernel threads, other users' processes — cannot be a
// game this user is playing, and zombies are already dead: a quit Proton
// game routinely lingers as a defunct .exe until its prefix tears down, and
// counting it would pin the game as playing forever.
func (p *platformProvider) Processes(ctx context.Context) ([]Process, error) {
	live, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	seen := make(map[int32]knownProcess, len(live))
	processes := make([]Process, 0, len(live))
	for _, candidate := range live {
		if defunct(ctx, candidate) {
			continue
		}
		created, err := candidate.CreateTimeWithContext(ctx)
		if err != nil {
			continue
		}
		identity, ok := p.known[candidate.Pid]
		if !ok || identity.created != created {
			executable, err := candidate.ExeWithContext(ctx)
			if err != nil || executable == "" {
				continue
			}
			name, err := candidate.NameWithContext(ctx)
			if err != nil {
				name = filepath.Base(executable)
			}
			identity = knownProcess{created: created, name: name, executable: executable}
		}
		seen[candidate.Pid] = identity
		processes = append(processes, Process{
			PID:        candidate.Pid,
			Name:       identity.name,
			Executable: identity.executable,
		})
	}
	p.known = seen
	return processes, nil
}

// defunct reports whether the OS lists a process that can no longer run
// code. Only Linux checks — that is where the problem lives (a quit Proton
// game lingers as a defunct .exe) and where status is a cheap procfs read;
// on macOS each status costs an exec'd ps, which measured at ~1.4ms per
// process — most of a second per sweep — for a corpse Unix-style reaping
// makes rare there anyway. Status can be unreadable for a process mid-exit;
// treating that as defunct errs toward not detecting, never toward a stuck
// "playing".
func defunct(ctx context.Context, candidate *process.Process) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	statuses, err := candidate.StatusWithContext(ctx)
	if err != nil {
		return true
	}
	for _, status := range statuses {
		if status == process.Zombie {
			return true
		}
	}
	return false
}

// Cmdline returns a process's arguments.
func (p *platformProvider) Cmdline(ctx context.Context, pid int32) ([]string, error) {
	candidate, err := process.NewProcessWithContext(ctx, pid)
	if err != nil {
		return nil, err
	}
	return candidate.CmdlineSliceWithContext(ctx)
}

// maxOpenPathDescriptors bounds the descriptor walk: a process holding
// thousands of descriptors is not a game whose save evidence sits past the
// bound, and an unbounded walk would let one pathological process slow every
// sweep.
const maxOpenPathDescriptors = 512

// OpenPaths returns the filesystem paths a process holds open, plus its
// working directory. Only Linux answers: /proc/<pid>/fd and /proc/<pid>/cwd
// are free to read for the user's own processes. Other platforms would need
// an lsof-grade walk, so they report nothing and matchers fall back to
// other evidence.
func (p *platformProvider) OpenPaths(ctx context.Context, pid int32) ([]string, error) {
	if runtime.GOOS != "linux" {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	base := filepath.Join("/proc", strconv.Itoa(int(pid)))
	var paths []string
	if cwd, err := os.Readlink(filepath.Join(base, "cwd")); err == nil && filepath.IsAbs(cwd) {
		paths = append(paths, cwd)
	}
	entries, err := os.ReadDir(filepath.Join(base, "fd"))
	if err != nil {
		// A process that vanished or hides its descriptors offers no
		// evidence; the working directory alone may still have answered.
		return paths, nil
	}
	if len(entries) > maxOpenPathDescriptors {
		entries = entries[:maxOpenPathDescriptors]
	}
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join(base, "fd", entry.Name()))
		// Sockets, pipes, and anonymous inodes read as "socket:[7]"-style
		// pseudo-paths; only real filesystem paths can be save evidence.
		if err == nil && filepath.IsAbs(target) {
			paths = append(paths, target)
		}
	}
	return paths, nil
}
