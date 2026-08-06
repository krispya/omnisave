package running

import (
	"context"

	"github.com/shirou/gopsutil/v4/process"
)

// PlatformDetector detects against this machine's live processes.
func PlatformDetector() *Detector {
	return NewDetector(platformLister{})
}

type platformLister struct{}

// Processes lists live processes that expose an executable path. Processes
// that refuse theirs — kernel threads, other users' processes — cannot be a
// game this user is playing, so they are skipped rather than failing the
// sweep.
func (platformLister) Processes(ctx context.Context) ([]Process, error) {
	live, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}
	processes := make([]Process, 0, len(live))
	for _, candidate := range live {
		executable, err := candidate.ExeWithContext(ctx)
		if err != nil || executable == "" {
			continue
		}
		processes = append(processes, Process{PID: candidate.Pid, Executable: executable})
	}
	return processes, nil
}
