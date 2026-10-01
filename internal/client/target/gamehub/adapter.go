// Package gamehub discovers the Steam games GameHub has installed. GameHub is
// a third-party Steam client for macOS: it installs Steam's own Windows builds
// into Steam-format libraries and runs each in a Wine prefix of its own.
package gamehub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/target/steam/steamapps"
)

const adapterName = "gamehub"

// Adapter finds GameHub's installation and the Steam games it has installed.
// Its games carry their Steam identity, so one played through GameHub here
// and through Steam elsewhere is one Game, and their saves are located by the
// same save-location rules as any Steam game's (FDR-003, decision 10).
type Adapter struct {
	// root is GameHub's data directory; empty where GameHub does not run.
	root string
}

// New creates an adapter reading GameHub's data directory at root.
func New(root string) *Adapter {
	return &Adapter{root: root}
}

// NewDefault creates an adapter for GameHub's conventional data directory.
func NewDefault() *Adapter {
	return New(DefaultRoot())
}

// DefaultRoot is GameHub's data directory on this host, or empty on a host
// GameHub does not run on.
func DefaultRoot() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", "com.gamemac.www")
}

// SteamClientRoots reports the directories under a GameHub data directory
// laid out as a Steam client root for its app cache, one per signed-in
// account: each holds appcache/appinfo.vdf, the same configuration Valve's
// client caches, but none of Valve's userdata.
func SteamClientRoots(root string) []string {
	if root == "" {
		return nil
	}
	accounts := filepath.Join(root, "steam-client", "accounts")
	entries, err := os.ReadDir(accounts)
	if err != nil {
		return nil
	}
	var roots []string
	for _, entry := range entries {
		if entry.IsDir() {
			roots = append(roots, filepath.Join(accounts, entry.Name()))
		}
	}
	return roots
}

func (a *Adapter) Name() string {
	return adapterName
}

func (a *Adapter) DiscoverTargets(context.Context) ([]target.Target, error) {
	if a.root == "" {
		return nil, nil
	}
	root, err := filepath.EvalSymlinks(a.root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return nil, err
	}
	return []target.Target{{
		ID:       adapterName + ":" + root,
		Adapter:  adapterName,
		Source:   "installer",
		Root:     root,
		Location: root,
	}}, nil
}

// DiscoverGames reports the Steam games installed in GameHub's libraries.
// GameHub runs only Windows builds, so every game is a Wine game; one GameHub
// has not yet given a prefix keeps its user folders nowhere yet.
func (a *Adapter) DiscoverGames(ctx context.Context, discovered target.Target) ([]target.InstalledGame, error) {
	if discovered.Adapter != adapterName || discovered.Root == "" {
		return nil, fmt.Errorf("invalid GameHub target")
	}
	prefixes := steamPrefixes(discovered.Root)
	seen := make(map[string]bool)
	var games []target.InstalledGame
	for _, library := range windowsLibraries(discovered.Root) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		apps, err := steamapps.Installed(library)
		if err != nil {
			return nil, err
		}
		for _, app := range apps {
			if seen[app.ID] {
				continue
			}
			seen[app.ID] = true
			environment := target.CurrentEnvironment(library)
			environment.Runtime = target.RuntimeWine
			environment.PrefixRoot = prefixes[app.ID]
			games = append(games, target.InstalledGame{
				ID:       discovered.ID + ":" + app.ID,
				TargetID: discovered.ID,
				Identity: target.GameIdentity{
					Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: app.ID}},
					Title:       app.Title,
					// The same Steam purchase as on any other client.
					Platform: "PC",
				},
				InstallRoot: app.InstallRoot,
				Environment: environment,
				Metadata:    map[string]string{"library_root": library},
			})
		}
	}
	sort.Slice(games, func(left, right int) bool {
		return games[left].ID < games[right].ID
	})
	return games, nil
}

// DiscoverSaves reports no saves. A GameHub game's saves are wherever the
// game writes them inside its prefix, which only save-location rules know;
// the Steam Cloud bookkeeping GameHub keeps in the prefix is a mirror, never
// a save (FDR-003, decision 10).
func (a *Adapter) DiscoverSaves(_ context.Context, discovered target.Target, game target.InstalledGame) ([]target.Save, error) {
	return nil, validateGame(discovered, game)
}

// DiscoverSaveDestinations reports no destinations, for the same reason
// DiscoverSaves reports no saves.
func (a *Adapter) DiscoverSaveDestinations(_ context.Context, discovered target.Target, game target.InstalledGame) ([]target.SaveDestination, error) {
	return nil, validateGame(discovered, game)
}

func validateGame(discovered target.Target, game target.InstalledGame) error {
	_, hasAppID := game.Identity.Identifier("steam.app")
	if discovered.Adapter != adapterName || discovered.Root == "" || game.TargetID != discovered.ID || !hasAppID {
		return fmt.Errorf("invalid GameHub game")
	}
	return nil
}

// libraryRegistry is GameHub's list of the Steam libraries it installs into.
type libraryRegistry struct {
	Libraries []struct {
		Platform      string `json:"platform"`
		CanonicalRoot string `json:"canonicalRoot"`
	} `json:"libraries"`
}

// containerStore binds each game GameHub runs to the container it runs in.
type containerStore struct {
	Bindings []struct {
		Platform           string `json:"platform"`
		PlatformAppID      string `json:"platform_app_id"`
		VirtualContainerID string `json:"virtual_container_id"`
	} `json:"bindings"`
}

// virtualContainers names each container's Wine prefix.
type virtualContainers struct {
	Containers map[string]struct {
		PrefixPath string `json:"prefix_path"`
	} `json:"virtual_containers"`
}

// windowsLibraries reads GameHub's registry of Steam libraries. A library for
// another platform is one this adapter cannot say how to run, so it is left
// out rather than guessed at.
func windowsLibraries(root string) []string {
	registry := readJSON[libraryRegistry](filepath.Join(root, "steam-client", "steam-library-registry.v1.json"))
	var libraries []string
	for _, library := range registry.Libraries {
		if strings.EqualFold(library.Platform, "windows") && filepath.IsAbs(library.CanonicalRoot) {
			libraries = append(libraries, library.CanonicalRoot)
		}
	}
	return libraries
}

// steamPrefixes maps each Steam app GameHub has bound to a container onto
// that container's Wine prefix, as GameHub itself resolves one at launch: the
// binding names a virtual container, and the container names its prefix. A
// prefix not yet on disk is left out, so nothing is placed into one GameHub
// has not created.
func steamPrefixes(root string) map[string]string {
	store := readJSON[containerStore](filepath.Join(root, "gamehub", "game_container_store.json"))
	containers := readJSON[virtualContainers](filepath.Join(root, "wine-engine", "container", "wine_virtual_containers.json"))
	prefixes := make(map[string]string)
	for _, binding := range store.Bindings {
		if !strings.EqualFold(binding.Platform, "steam") || binding.PlatformAppID == "" {
			continue
		}
		if _, bound := prefixes[binding.PlatformAppID]; bound {
			continue
		}
		prefix := containers.Containers[binding.VirtualContainerID].PrefixPath
		if !filepath.IsAbs(prefix) {
			continue
		}
		if info, err := os.Stat(prefix); err == nil && info.IsDir() {
			prefixes[binding.PlatformAppID] = prefix
		}
	}
	return prefixes
}

// readJSON decodes one of GameHub's own files. They are GameHub's private,
// versioned bookkeeping rather than a published format, so one this Device
// cannot read whole — absent, mid-rewrite, or reshaped by a GameHub update —
// answers nothing rather than failing every adapter's scan, as an unreadable
// Steam app cache does.
func readJSON[T any](path string) T {
	var value T
	data, err := os.ReadFile(path)
	if err != nil {
		return value
	}
	if err := json.Unmarshal(data, &value); err != nil {
		var nothing T
		return nothing
	}
	return value
}

var _ target.Adapter = (*Adapter)(nil)
