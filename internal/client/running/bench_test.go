package running_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/krisbaumgartner/omnisave/internal/client/running"
)

// BenchmarkPlatformSweep measures one real process-table sweep on this
// machine, the fixed cost every detection check pays.
func BenchmarkPlatformSweep(b *testing.B) {
	detector := running.PlatformDetector()
	ctx := context.Background()
	none := func(context.Context, *running.Snapshot) (map[string]bool, error) {
		return nil, nil
	}
	if _, err := detector.Playing(ctx, none); err != nil {
		b.Skipf("process table unreadable: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := detector.Playing(ctx, none); err != nil {
			b.Fatal(err)
		}
	}
}

// crowd builds a synthetic process table of the given size.
func crowd(processes int) []running.Process {
	table := make([]running.Process, 0, processes)
	for i := 0; i < processes; i++ {
		table = append(table, running.Process{
			PID:        int32(i + 2),
			Name:       fmt.Sprintf("proc%d", i),
			Executable: fmt.Sprintf("/usr/lib/system/daemons/proc%d/binary", i),
		})
	}
	return table
}

// BenchmarkRootMatching measures the pure-CPU cost of prefix-matching a
// desktop-sized process table against a tracked library, the inner loop of
// the Steam matcher's fallback.
func BenchmarkRootMatching(b *testing.B) {
	table := crowd(500)
	roots := make([][]string, 20)
	for i := range roots {
		roots[i] = []string{fmt.Sprintf("/steam/steamapps/common/game-%d", i)}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, gameRoots := range roots {
			for _, process := range table {
				for _, root := range gameRoots {
					if running.UnderRoot(process.Executable, root) {
						b.Fatal("unexpected match")
					}
				}
			}
		}
	}
}

// BenchmarkEvidenceMatching measures claim-versus-evidence comparison at
// RetroArch scale: a frontend holding many descriptors against a large
// playlist library.
func BenchmarkEvidenceMatching(b *testing.B) {
	evidence := make([]string, 512)
	for i := range evidence {
		evidence[i] = fmt.Sprintf("/proc/self/fd/target-%d.dat", i)
	}
	claims := make([]string, 100)
	for i := range claims {
		claims[i] = fmt.Sprintf("/roms/game-%d.sfc", i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, claim := range claims {
			for _, observed := range evidence {
				if running.SamePath(observed, claim) {
					b.Fatal("unexpected match")
				}
			}
		}
	}
}
