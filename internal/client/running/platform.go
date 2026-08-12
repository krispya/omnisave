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

// Processes lists live, non-defunct processes with readable executable paths.
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

// defunct reports Linux zombie processes; unreadable status is treated as defunct.
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

// maxOpenPathDescriptors bounds the cost of one process's descriptor walk.
const maxOpenPathDescriptors = 512

// OpenPaths returns Linux process file descriptors and its working directory.
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
