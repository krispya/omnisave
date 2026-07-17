package locator

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

const retroArchSteamAppID = "1118310"

var steamLibraryPath = regexp.MustCompile(`^\s*"path"\s+"([^"]+)"`)

type steam struct {
	roots []string
}

// NewSteam finds RetroArch in Steam library folders.
func NewSteam(roots ...string) Locator {
	return steam{roots: roots}
}

func (l steam) Locate(ctx context.Context) ([]Candidate, error) {
	roots := l.roots
	if len(roots) == 0 {
		var err error
		roots, err = defaultSteamRoots()
		if err != nil {
			return nil, err
		}
	}

	var candidates []Candidate
	seenLibraries := make(map[string]bool)
	for _, root := range roots {
		libraries, err := steamLibraries(root)
		if err != nil {
			return nil, err
		}
		for _, library := range libraries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if seenLibraries[library] {
				continue
			}
			seenLibraries[library] = true
			steamApps := filepath.Join(library, "steamapps")
			if _, err := os.Stat(filepath.Join(steamApps, "appmanifest_"+retroArchSteamAppID+".acf")); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return nil, err
			}
			installationRoot := filepath.Join(steamApps, "common", "RetroArch")
			configPath := filepath.Join(installationRoot, "retroarch.cfg")
			found, err := existingCandidates(ctx, "steam", []string{configPath})
			if err != nil {
				return nil, err
			}
			for index := range found {
				found[index].Root = installationRoot
			}
			candidates = append(candidates, found...)
		}
	}
	return candidates, nil
}

func defaultSteamRoots() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return defaultSteamRootsFor(runtime.GOOS, home), nil
}

func defaultSteamRootsFor(hostOS, home string) []string {
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

func steamLibraries(root string) ([]string, error) {
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	libraries := []string{root}
	file, err := os.Open(filepath.Join(root, "steamapps", "libraryfolders.vdf"))
	if errors.Is(err, os.ErrNotExist) {
		return libraries, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		match := steamLibraryPath.FindStringSubmatch(scanner.Text())
		if len(match) == 2 {
			libraries = append(libraries, strings.ReplaceAll(match[1], `\\`, `\`))
		}
	}
	return libraries, scanner.Err()
}
