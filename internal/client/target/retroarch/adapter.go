// Package retroarch discovers native saves managed by RetroArch.
package retroarch

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/client/target/retroarch/locator"
	"github.com/krisbaumgartner/omnisave/internal/client/target/retroarch/platform"
)

const adapterName = "retroarch"

type Adapter struct {
	locators  []locator.Locator
	platforms []platform.Profile
}

// New creates a RetroArch adapter from installation locators.
func New(locators ...locator.Locator) *Adapter {
	return &Adapter{
		locators:  locators,
		platforms: []platform.Profile{platform.SNES()},
	}
}

// NewDefault creates an adapter for the currently supported installation types.
func NewDefault() *Adapter {
	return New(locator.NewSteam(), locator.NewApplication(), locator.NewInstaller())
}

func (a *Adapter) Name() string {
	return adapterName
}

func (a *Adapter) DiscoverTargets(ctx context.Context) ([]target.Target, error) {
	var targets []target.Target
	seen := make(map[string]bool)
	for _, locator := range a.locators {
		candidates, err := locator.Locate(ctx)
		if err != nil {
			return nil, err
		}
		for _, candidate := range candidates {
			configPath := ""
			if candidate.ConfigPath != "" {
				configPath, err = canonicalConfigPath(candidate.ConfigPath)
				if err != nil {
					return nil, err
				}
			}
			root := candidate.Root
			if root == "" && configPath != "" {
				root = filepath.Dir(configPath)
			}
			root, err = canonicalRoot(root)
			if err != nil {
				return nil, err
			}
			key := "root:" + root
			if configPath != "" {
				key = "config:" + configPath
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			targets = append(targets, target.Target{
				ID:       adapterName + ":" + root,
				Adapter:  adapterName,
				Source:   candidate.Source,
				Root:     root,
				Location: configPath,
			})
		}
	}
	return targets, nil
}

func canonicalRoot(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("RetroArch installation has no root")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("RetroArch installation is not a directory: %s", path)
	}
	return absolute, nil
}

func canonicalConfigPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve RetroArch config %s: %w", path, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("RetroArch config is not a file: %s", path)
	}
	return resolved, nil
}

func (a *Adapter) DiscoverGames(ctx context.Context, discovered target.Target) ([]target.InstalledGame, error) {
	if discovered.Adapter != adapterName {
		return nil, fmt.Errorf("invalid RetroArch target")
	}
	if discovered.Location == "" {
		return nil, nil
	}
	config, err := readConfig(discovered.Location)
	if err != nil {
		return nil, err
	}
	configDirectory := filepath.Dir(discovered.Location)
	saveDirectory := resolveDirectory(config["savefile_directory"], "", configDirectory)
	playlistDirectory := resolveDirectory(config["playlist_directory"], filepath.Join(configDirectory, "playlists"), configDirectory)

	var games []target.InstalledGame
	seen := make(map[string]bool)
	for _, profile := range a.platforms {
		for _, playlistName := range profile.PlaylistNames {
			discoveredGames, err := discoverPlaylistGames(
				ctx,
				discovered,
				profile,
				filepath.Join(playlistDirectory, playlistName),
				saveDirectory,
			)
			if err != nil {
				return nil, err
			}
			for _, game := range discoveredGames {
				if !seen[game.ID] {
					seen[game.ID] = true
					games = append(games, game)
				}
			}
		}
	}
	return games, nil
}

type playlist struct {
	Items []playlistItem `json:"items"`
}

type playlistItem struct {
	Path  string `json:"path"`
	Label string `json:"label"`
	CRC32 string `json:"crc32"`
}

func discoverPlaylistGames(
	ctx context.Context,
	discovered target.Target,
	profile platform.Profile,
	playlistPath string,
	saveDirectory string,
) ([]target.InstalledGame, error) {
	data, err := os.ReadFile(playlistPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read playlist %s: %w", playlistPath, err)
	}
	var contents playlist
	if err := json.Unmarshal(data, &contents); err != nil {
		return nil, fmt.Errorf("parse playlist %s: %w", playlistPath, err)
	}

	var games []target.InstalledGame
	for _, item := range contents.Items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if item.Path == "" {
			continue
		}
		identity := target.GameIdentity{
			Title:       item.Label,
			Platform:    profile.ID,
			ContentPath: item.Path,
		}
		if crc32 := playlistCRC32(item.CRC32); crc32 != "" {
			identity.Fingerprints = []catalog.GameFingerprint{{
				Platform: profile.ID, Algorithm: "crc32", Value: crc32,
			}}
		}
		games = append(games, target.InstalledGame{
			ID:          discovered.ID + ":" + profile.ID + ":" + item.Path,
			TargetID:    discovered.ID,
			Identity:    identity,
			InstallRoot: filepath.Dir(item.Path),
			Environment: target.CurrentEnvironment(discovered.Root),
			Metadata:    map[string]string{"save_directory": saveDirectory},
		})
	}
	return games, nil
}

func (a *Adapter) DiscoverSaves(ctx context.Context, discovered target.Target, game target.InstalledGame) ([]target.Save, error) {
	if discovered.Adapter != adapterName || game.TargetID != discovered.ID {
		return nil, fmt.Errorf("invalid RetroArch game")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	profile, ok := a.platform(game.Identity.Platform)
	if !ok {
		return nil, nil
	}
	saveDirectory := game.Metadata["save_directory"]
	if saveDirectory == "" {
		saveDirectory = filepath.Dir(game.Identity.ContentPath)
	}
	base := filepath.Base(game.Identity.ContentPath)
	name := strings.TrimSuffix(base, filepath.Ext(base))

	var saves []target.Save
	for _, extension := range profile.SaveExtensions {
		savePath := filepath.Join(saveDirectory, name+extension)
		info, err := os.Stat(savePath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect save %s: %w", savePath, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		saves = append(saves, target.Save{
			ID:       game.ID + ":" + savePath,
			TargetID: discovered.ID,
			GameID:   game.ID,
			Kind:     "battery",
			Files: []target.File{{
				Path:         savePath,
				LocationID:   "battery",
				RelativePath: filepath.Base(savePath),
				Size:         info.Size(),
				Modified:     info.ModTime(),
			}},
		})
	}
	return saves, nil
}

// DiscoverSaveDestinations reports the native battery-save paths even before the
// files exist, allowing a server head to be placed on a fresh Device.
func (a *Adapter) DiscoverSaveDestinations(ctx context.Context, discovered target.Target, game target.InstalledGame) ([]target.SaveDestination, error) {
	if discovered.Adapter != adapterName || game.TargetID != discovered.ID {
		return nil, fmt.Errorf("invalid RetroArch game")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	profile, ok := a.platform(game.Identity.Platform)
	if !ok {
		return nil, nil
	}
	saveDirectory := game.Metadata["save_directory"]
	if saveDirectory == "" {
		saveDirectory = filepath.Dir(game.Identity.ContentPath)
	}
	base := filepath.Base(game.Identity.ContentPath)
	name := strings.TrimSuffix(base, filepath.Ext(base))

	destinations := make([]target.SaveDestination, 0, len(profile.SaveExtensions))
	for _, extension := range profile.SaveExtensions {
		savePath := filepath.Join(saveDirectory, name+extension)
		destinations = append(destinations, target.SaveDestination{
			ID:       game.ID + ":" + savePath,
			TargetID: discovered.ID,
			GameID:   game.ID,
			Kind:     "battery",
			Locations: []target.SaveLocation{{
				ID: "battery", Path: saveDirectory, Kind: target.SaveLocationDirectory,
			}},
		})
	}
	return destinations, nil
}

func (a *Adapter) platform(id string) (platform.Profile, bool) {
	for _, profile := range a.platforms {
		if profile.ID == id {
			return profile, true
		}
	}
	return platform.Profile{}, false
}

func readConfig(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read RetroArch config: %w", err)
	}
	defer file.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func resolveDirectory(configured, fallback, relativeTo string) string {
	if configured == "" || configured == "default" {
		return fallback
	}
	if configured == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(configured, "~/") || strings.HasPrefix(configured, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, configured[2:])
		}
	}
	if filepath.IsAbs(configured) {
		return filepath.Clean(configured)
	}
	return filepath.Join(relativeTo, configured)
}

func playlistCRC32(value string) string {
	value, _, _ = strings.Cut(value, "|")
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "detect" {
		return ""
	}
	return value
}

var _ target.Adapter = (*Adapter)(nil)
