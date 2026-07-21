// Package steam discovers installed Steam games and their Cloud save sets.
package steam

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/target/steam/locator"
)

const adapterName = "steam"

var (
	libraryPath = regexp.MustCompile(`(?m)^\s*"path"\s+"([^"]+)"`)
	manifestRow = regexp.MustCompile(`(?m)^\s*"([^"]+)"\s+"([^"]*)"`)
)

type Adapter struct {
	locators []locator.Locator
}

// New creates a Steam adapter from installation locators.
func New(locators ...locator.Locator) *Adapter {
	return &Adapter{locators: locators}
}

// NewDefault creates an adapter for conventional Steam installations.
func NewDefault() *Adapter {
	return New(locator.NewInstaller())
}

func (a *Adapter) Name() string {
	return adapterName
}

func (a *Adapter) DiscoverTargets(ctx context.Context) ([]target.Target, error) {
	var targets []target.Target
	seen := make(map[string]bool)
	for _, targetLocator := range a.locators {
		candidates, err := targetLocator.Locate(ctx)
		if err != nil {
			return nil, err
		}
		for _, candidate := range candidates {
			root, err := canonicalRoot(candidate.Root)
			if err != nil {
				return nil, err
			}
			if seen[root] {
				continue
			}
			seen[root] = true
			targets = append(targets, target.Target{
				ID:       adapterName + ":" + root,
				Adapter:  adapterName,
				Source:   candidate.Source,
				Root:     root,
				Location: root,
			})
		}
	}
	return targets, nil
}

func (a *Adapter) DiscoverGames(ctx context.Context, discovered target.Target) ([]target.InstalledGame, error) {
	if discovered.Adapter != adapterName || discovered.Root == "" {
		return nil, fmt.Errorf("invalid Steam target")
	}
	libraries, err := steamLibraries(discovered.Root)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var games []target.InstalledGame
	for _, library := range libraries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		steamApps := filepath.Join(library, "steamapps")
		entries, err := os.ReadDir(steamApps)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasPrefix(name, "appmanifest_") || !strings.HasSuffix(name, ".acf") {
				continue
			}
			app, err := readAppManifest(filepath.Join(steamApps, name))
			if err != nil {
				return nil, err
			}
			if app.ID == "" || app.InstallDirectory == "" || seen[app.ID] {
				continue
			}
			installRoot := filepath.Join(steamApps, "common", app.InstallDirectory)
			info, err := os.Stat(installRoot)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if !info.IsDir() {
				continue
			}
			seen[app.ID] = true
			environment := target.CurrentEnvironment(library)
			prefixRoot := filepath.Join(steamApps, "compatdata", app.ID, "pfx")
			if prefixInfo, err := os.Stat(prefixRoot); err == nil && prefixInfo.IsDir() {
				environment.Runtime = target.RuntimeProton
				environment.PrefixRoot = prefixRoot
			} else if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
			games = append(games, target.InstalledGame{
				ID:       discovered.ID + ":" + app.ID,
				TargetID: discovered.ID,
				Identity: target.GameIdentity{
					Identifiers: []catalog.GameIdentifier{{Namespace: "steam.app", Value: app.ID}},
					Title:       app.Title,
				},
				InstallRoot: installRoot,
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

func (a *Adapter) DiscoverSaves(ctx context.Context, discovered target.Target, game target.InstalledGame) ([]target.Save, error) {
	appID, hasAppID := game.Identity.Identifier("steam.app")
	if discovered.Adapter != adapterName || discovered.Root == "" || game.TargetID != discovered.ID || !hasAppID {
		return nil, fmt.Errorf("invalid Steam game")
	}
	accounts, err := numericDirectories(filepath.Join(discovered.Root, "userdata"))
	if err != nil {
		return nil, err
	}

	var saves []target.Save
	for _, accountID := range accounts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remoteDirectory := filepath.Join(discovered.Root, "userdata", accountID, appID, "remote")
		files, err := cloudFiles(remoteDirectory)
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			continue
		}
		saves = append(saves, target.Save{
			ID:       game.ID + ":cloud:" + accountID,
			TargetID: discovered.ID,
			GameID:   game.ID,
			Kind:     "cloud",
			Files:    files,
			Metadata: map[string]string{"account_id": accountID},
		})
	}
	return saves, nil
}

// DiscoverSaveDestinations reports a Steam Cloud destination only when exactly one
// local account makes the destination unambiguous.
func (a *Adapter) DiscoverSaveDestinations(ctx context.Context, discovered target.Target, game target.InstalledGame) ([]target.SaveDestination, error) {
	appID, hasAppID := game.Identity.Identifier("steam.app")
	if discovered.Adapter != adapterName || discovered.Root == "" || game.TargetID != discovered.ID || !hasAppID {
		return nil, fmt.Errorf("invalid Steam game")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	accounts, err := numericDirectories(filepath.Join(discovered.Root, "userdata"))
	if err != nil {
		return nil, err
	}
	if len(accounts) != 1 {
		return nil, nil
	}
	accountID := accounts[0]
	return []target.SaveDestination{{
		ID:       game.ID + ":cloud:" + accountID,
		TargetID: discovered.ID,
		GameID:   game.ID,
		Kind:     "cloud",
		Locations: []target.SaveLocation{{
			ID:   "remote",
			Path: filepath.Join(discovered.Root, "userdata", accountID, appID, "remote"),
			Kind: target.SaveLocationDirectory,
		}},
		Metadata: map[string]string{"account_id": accountID},
	}}, nil
}

type appManifest struct {
	ID               string
	Title            string
	InstallDirectory string
}

func readAppManifest(path string) (appManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return appManifest{}, fmt.Errorf("read Steam app manifest %s: %w", path, err)
	}
	values := make(map[string]string)
	for _, match := range manifestRow.FindAllStringSubmatch(string(data), -1) {
		values[strings.ToLower(match[1])] = strings.ReplaceAll(match[2], `\\`, `\`)
	}
	if _, err := strconv.ParseUint(values["appid"], 10, 64); err != nil {
		return appManifest{}, nil
	}
	title := values["name"]
	if title == "" {
		title = values["appid"]
	}
	return appManifest{ID: values["appid"], Title: title, InstallDirectory: values["installdir"]}, nil
}

func steamLibraries(root string) ([]string, error) {
	libraries := []string{root}
	data, err := os.ReadFile(filepath.Join(root, "steamapps", "libraryfolders.vdf"))
	if os.IsNotExist(err) {
		return libraries, nil
	}
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{root: true}
	for _, match := range libraryPath.FindAllStringSubmatch(string(data), -1) {
		library := strings.ReplaceAll(match[1], `\\`, `\`)
		absolute, err := filepath.Abs(library)
		if err != nil {
			return nil, err
		}
		if !seen[absolute] {
			seen[absolute] = true
			libraries = append(libraries, absolute)
		}
	}
	return libraries, nil
}

func canonicalRoot(root string) (string, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("Steam root is not a directory: %s", root)
	}
	return resolved, nil
}

func numericDirectories(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.ParseUint(entry.Name(), 10, 64); err == nil {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

func cloudFiles(root string) ([]target.File, error) {
	var files []target.File
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) && path == root {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, target.File{
			Path:         path,
			LocationID:   "remote",
			RelativePath: relativePath,
			Size:         info.Size(),
			Modified:     info.ModTime(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(left, right int) bool {
		return files[left].RelativePath < files[right].RelativePath
	})
	return files, nil
}

var _ target.Adapter = (*Adapter)(nil)
