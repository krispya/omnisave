package saveprofile

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

// Resolve finds the existing files described by a save profile.
func Resolve(game target.InstalledGame, profile Profile) ([]target.Save, error) {
	seen := make(map[string]bool)
	var files []target.File
	for _, rule := range profile.Rules {
		if !applies(rule, game) {
			continue
		}
		pattern := expand(rule.Path, game)
		if pattern == "" {
			continue
		}
		resolved, err := collect(pattern, rule.ID)
		if err != nil {
			return nil, fmt.Errorf("resolve rule %s: %w", rule.ID, err)
		}
		for _, file := range resolved {
			if !seen[file.Path] {
				seen[file.Path] = true
				files = append(files, file)
			}
		}
	}
	if len(files) == 0 {
		return nil, nil
	}
	sort.Slice(files, func(left, right int) bool {
		if files[left].LocationID == files[right].LocationID {
			return files[left].RelativePath < files[right].RelativePath
		}
		return files[left].LocationID < files[right].LocationID
	})
	return []target.Save{{
		ID:       game.ID + ":profile:" + profile.Provider + ":" + profile.ProviderID,
		TargetID: game.TargetID,
		GameID:   game.ID,
		Kind:     "local",
		Files:    files,
		Metadata: map[string]string{
			"profile_provider":    profile.Provider,
			"profile_provider_id": profile.ProviderID,
		},
	}}, nil
}

// ResolveDestinations expands applicable profile rules into a prospective Local Save
// even when none of its files exist yet.
func ResolveDestinations(game target.InstalledGame, profile Profile) ([]target.SaveDestination, error) {
	var locations []target.SaveLocation
	seen := make(map[string]bool)
	for _, rule := range profile.Rules {
		if !applies(rule, game) || seen[rule.ID] {
			continue
		}
		expanded := expand(rule.Path, game)
		if expanded == "" {
			continue
		}
		location := target.SaveLocation{ID: rule.ID, Path: expanded, Kind: target.SaveLocationUnknown}
		if hasMeta(expanded) {
			location.Path = globBase(expanded)
			location.Kind = target.SaveLocationDirectory
		} else if info, err := os.Lstat(expanded); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			if info.IsDir() {
				location.Kind = target.SaveLocationDirectory
			} else if info.Mode().IsRegular() {
				location.Kind = target.SaveLocationFile
			} else {
				continue
			}
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("inspect rule %s: %w", rule.ID, err)
		}
		seen[rule.ID] = true
		locations = append(locations, location)
	}
	if len(locations) == 0 {
		return nil, nil
	}
	return []target.SaveDestination{{
		ID:        game.ID + ":profile:" + profile.Provider + ":" + profile.ProviderID,
		TargetID:  game.TargetID,
		GameID:    game.ID,
		Kind:      "local",
		Locations: locations,
		Metadata: map[string]string{
			"profile_provider":    profile.Provider,
			"profile_provider_id": profile.ProviderID,
		},
	}}, nil
}

func applies(rule Rule, game target.InstalledGame) bool {
	if rule.Store != "" {
		if _, ok := game.Identity.Identifier(strings.ToLower(rule.Store) + ".app"); !ok {
			return false
		}
	}
	if game.Environment.Runtime == target.RuntimeProton {
		return rule.OS == "" || rule.OS == OSWindows
	}
	return rule.OS == "" || rule.OS == game.Environment.HostOS
}

func expand(template string, game target.InstalledGame) string {
	environment := game.Environment
	home := environment.Home
	_, storeGameID, _ := game.Identity.StoreIdentifier()
	values := map[string]string{
		"base":        game.InstallRoot,
		"game":        filepath.Base(game.InstallRoot),
		"home":        home,
		"root":        environment.StoreRoot,
		"storeGameId": storeGameID,
		// The store account owning a save is unknown at discovery time, so
		// the placeholder matches any account directory, as Ludusavi does.
		"storeUserId": "*",
		// A rule may name the OS account owning the save. Native
		// environments derive it from the home directory; Proton prefixes
		// always call the account steamuser.
		"osUserName":      filepath.Base(home),
		"winAppData":      environment.Variables["APPDATA"],
		"winLocalAppData": environment.Variables["LOCALAPPDATA"],
		"winProgramData":  environment.Variables["PROGRAMDATA"],
		"winPublic":       environment.Variables["PUBLIC"],
		"winDir":          environment.Variables["WINDIR"],
		"xdgData":         environment.Variables["XDG_DATA_HOME"],
		"xdgConfig":       environment.Variables["XDG_CONFIG_HOME"],
		"macAppSupport":   filepath.Join(home, "Library", "Application Support"),
		"macPreferences":  filepath.Join(home, "Library", "Preferences"),
	}
	if values["winAppData"] == "" {
		values["winAppData"] = filepath.Join(home, "AppData", "Roaming")
	}
	if values["winLocalAppData"] == "" {
		values["winLocalAppData"] = filepath.Join(home, "AppData", "Local")
	}
	values["winLocalAppDataLow"] = filepath.Join(home, "AppData", "LocalLow")
	values["winDocuments"] = filepath.Join(home, "Documents")
	values["winSavedGames"] = filepath.Join(home, "Saved Games")
	if values["xdgData"] == "" {
		values["xdgData"] = filepath.Join(home, ".local", "share")
	}
	if values["xdgConfig"] == "" {
		values["xdgConfig"] = filepath.Join(home, ".config")
	}

	if environment.Runtime == target.RuntimeProton {
		// <root> stays the native store root: a Proton game's library is
		// still where it is, and <root>/steamapps/... rules depend on that.
		// Only user-relative placeholders move inside the prefix.
		userHome := filepath.Join(environment.PrefixRoot, "drive_c", "users", "steamuser")
		drive := filepath.Join(environment.PrefixRoot, "drive_c")
		values["home"] = userHome
		values["osUserName"] = "steamuser"
		values["winAppData"] = filepath.Join(userHome, "AppData", "Roaming")
		values["winLocalAppData"] = filepath.Join(userHome, "AppData", "Local")
		values["winLocalAppDataLow"] = filepath.Join(userHome, "AppData", "LocalLow")
		values["winDocuments"] = filepath.Join(userHome, "Documents")
		values["winSavedGames"] = filepath.Join(userHome, "Saved Games")
		values["winPublic"] = filepath.Join(drive, "users", "Public")
		values["winProgramData"] = filepath.Join(drive, "ProgramData")
		values["winDir"] = filepath.Join(drive, "windows")
	}

	expanded := strings.ReplaceAll(template, `\`, "/")
	for name, value := range values {
		if value != "" {
			expanded = strings.ReplaceAll(expanded, "<"+name+">", filepath.ToSlash(value))
		}
	}
	if strings.Contains(expanded, "<") || strings.Contains(expanded, ">") {
		return ""
	}
	return filepath.Clean(filepath.FromSlash(expanded))
}

func collect(pattern, locationID string) ([]target.File, error) {
	matches := []string{pattern}
	base := pattern
	if hasMeta(pattern) {
		var err error
		matches, err = expandGlob(pattern)
		if err != nil {
			return nil, err
		}
		base = globBase(pattern)
	}

	var files []target.File
	for _, match := range matches {
		info, err := os.Lstat(match)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if info.IsDir() {
			relativeBase := match
			if hasMeta(pattern) {
				relativeBase = base
			}
			err := filepath.WalkDir(match, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.Type()&os.ModeSymlink != 0 {
					if entry.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				if entry.IsDir() {
					return nil
				}
				entryInfo, err := entry.Info()
				if err != nil {
					return err
				}
				if !entryInfo.Mode().IsRegular() {
					return nil
				}
				file, err := nativeFile(path, relativeBase, locationID, entryInfo)
				if err != nil {
					return err
				}
				files = append(files, file)
				return nil
			})
			if err != nil {
				return nil, err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		relativeBase := filepath.Dir(match)
		if hasMeta(pattern) {
			relativeBase = base
		}
		file, err := nativeFile(match, relativeBase, locationID, info)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

func nativeFile(path, base, locationID string, info fs.FileInfo) (target.File, error) {
	relativePath, err := filepath.Rel(base, path)
	if err != nil {
		return target.File{}, err
	}
	return target.File{
		Path:         path,
		LocationID:   locationID,
		RelativePath: relativePath,
		Size:         info.Size(),
		Modified:     info.ModTime(),
	}, nil
}

func hasMeta(path string) bool {
	return strings.ContainsAny(path, "*?[")
}

func globBase(pattern string) string {
	index := strings.IndexAny(pattern, "*?[")
	if index < 0 {
		return filepath.Dir(pattern)
	}
	prefix := pattern[:index]
	separator := strings.LastIndexAny(prefix, `/\`)
	if separator < 0 {
		return "."
	}
	if separator == 0 {
		return string(filepath.Separator)
	}
	return filepath.Clean(prefix[:separator])
}
