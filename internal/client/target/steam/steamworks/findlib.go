package steamworks

import (
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// FindLibrary locates the Steamworks library a game ships inside its
// install directory. The game's own copy is used rather than a bundled one
// so reconciliation speaks the same dialect the game does — an interface
// version the game's library exports is one the game was built against.
func FindLibrary(installRoot string) (string, error) {
	wanted := libraryNames()
	var found []string
	walkErr := filepath.WalkDir(installRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		name := strings.ToLower(entry.Name())
		for _, candidate := range wanted {
			if name == candidate {
				found = append(found, path)
			}
		}
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	if len(found) == 0 {
		return "", fs.ErrNotExist
	}
	// A game can ship the library more than once (per-arch payloads); any
	// copy speaks for the game, so take the shallowest for stability.
	sort.Slice(found, func(left, right int) bool {
		leftDepth := strings.Count(found[left], string(filepath.Separator))
		rightDepth := strings.Count(found[right], string(filepath.Separator))
		if leftDepth == rightDepth {
			return found[left] < found[right]
		}
		return leftDepth < rightDepth
	})
	return found[0], nil
}

func libraryNames() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"libsteam_api.dylib"}
	case "windows":
		return []string{"steam_api64.dll", "steam_api.dll"}
	default:
		return []string{"libsteam_api.so"}
	}
}
