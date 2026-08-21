package locator

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
)

type installer struct {
	roots []string
}

// NewInstaller finds conventional Steam client installations.
func NewInstaller(roots ...string) Locator {
	return installer{roots: roots}
}

func (l installer) Locate(ctx context.Context) ([]Candidate, error) {
	roots := l.roots
	if len(roots) == 0 {
		var err error
		roots, err = defaultRoots()
		if err != nil {
			return nil, err
		}
	}

	var candidates []Candidate
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info, err := os.Stat(root)
		if err == nil && info.IsDir() {
			candidates = append(candidates, Candidate{Source: "installer", Root: root})
		} else if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	return candidates, nil
}

// DefaultRoots are the conventional Steam installation directories for this
// host. Callers that read Steam's own files without discovering targets —
// its cloud configuration, say — need the same list this locator walks.
func DefaultRoots() ([]string, error) {
	return defaultRoots()
}

func defaultRoots() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return defaultRootsFor(runtime.GOOS, home), nil
}

func defaultRootsFor(hostOS, home string) []string {
	switch hostOS {
	case "windows":
		var roots []string
		if programFiles := os.Getenv("ProgramFiles(x86)"); programFiles != "" {
			roots = append(roots, filepath.Join(programFiles, "Steam"))
		}
		if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
			roots = append(roots, filepath.Join(programFiles, "Steam"))
		}
		return roots
	case "darwin":
		return []string{filepath.Join(home, "Library", "Application Support", "Steam")}
	default:
		return []string{
			filepath.Join(home, ".steam", "steam"),
			filepath.Join(home, ".local", "share", "Steam"),
		}
	}
}
