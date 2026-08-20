package saveprofile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

// Resolve finds the existing files described by a save profile. Filesystem
// trouble along one rule's path — permissions, files where directories were
// expected — makes that rule find less, never fails the pass: discovery only
// ever reports what it could actually read.
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
		for _, file := range collect(pattern, rule.ID, templateGlobs(rule.Path)) {
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
		LocationAliases: locationAliases(profile),
	}}, nil
}

// locationAliases is every identity the profile's rules give this game's save
// locations, across every OS — the same logical place spelled per platform.
// Sharing them on the resolved save is what lets a revision minted under
// another OS's spelling be recognized here (FDR-003, decision 11).
func locationAliases(profile Profile) []string {
	seen := make(map[string]bool, len(profile.Rules))
	aliases := make([]string, 0, len(profile.Rules))
	for _, rule := range profile.Rules {
		if rule.ID == "" || seen[rule.ID] {
			continue
		}
		seen[rule.ID] = true
		aliases = append(aliases, rule.ID)
	}
	sort.Strings(aliases)
	return aliases
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
		if templateGlobs(rule.Path) && hasMeta(expanded) {
			location.Path = globBase(expanded)
			location.Kind = target.SaveLocationDirectory
		} else if resolved, info, err := lstatLiteral(expanded); err == nil {
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
			location.Path = resolved
		} else if !os.IsNotExist(err) {
			continue
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
		LocationAliases: locationAliases(profile),
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

// templateGlobs reports whether a rule's template asks for glob matching:
// its own metacharacters, or the any-account placeholder. Metacharacters
// that arrive through expanded environment values (a library named with
// brackets, say) stay literal.
func templateGlobs(template string) bool {
	return hasMeta(template) || strings.Contains(template, "<storeUserId>")
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
		// Games write to the shell's idea of Documents and Saved Games,
		// which OneDrive's folder move can relocate; on Windows the
		// environment reports the known-folder paths.
		"winDocuments":    environment.Variables["DOCUMENTS"],
		"winSavedGames":   environment.Variables["SAVED_GAMES"],
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
	if home != "" {
		// A rule may name the OS account owning the save. Native
		// environments derive it from the home directory; Proton prefixes
		// always call the account steamuser.
		values["osUserName"] = filepath.Base(home)
	}
	if values["winAppData"] == "" {
		values["winAppData"] = filepath.Join(home, "AppData", "Roaming")
	}
	if values["winLocalAppData"] == "" {
		values["winLocalAppData"] = filepath.Join(home, "AppData", "Local")
	}
	if values["winDocuments"] == "" {
		values["winDocuments"] = filepath.Join(home, "Documents")
	}
	if values["winSavedGames"] == "" {
		values["winSavedGames"] = filepath.Join(home, "Saved Games")
	}
	values["winLocalAppDataLow"] = filepath.Join(home, "AppData", "LocalLow")
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
	cleaned := filepath.Clean(filepath.FromSlash(expanded))
	// The manifest carries the occasional prose or relative entry; only an
	// absolute path can honestly be searched or offered as a destination.
	if !filepath.IsAbs(cleaned) {
		return ""
	}
	return cleaned
}

// collect gathers the regular files a resolved pattern names. Unreadable
// entries are skipped rather than reported: a rule can only speak for what
// discovery can see. Literal patterns match case-insensitively like globbed
// ones: the manifest's casing is community-authored, and the same rule must
// find the same save on a case-sensitive filesystem.
func collect(pattern, locationID string, globby bool) []target.File {
	matches := expandLiteral(pattern)
	if globby {
		matches = expandGlob(pattern)
	} else if len(matches) > 1 {
		// Case-sensitive filesystems can carry several spellings that fold to
		// the same literal rule. None is authoritative when no exact spelling
		// exists, so discovery must not merge them into one save location.
		return nil
	}

	var files []target.File
	for _, match := range matches {
		info, err := os.Lstat(match)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if info.IsDir() {
			base := match
			if globby {
				base = globBase(pattern)
			}
			filepath.WalkDir(match, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					if entry != nil && entry.IsDir() {
						return filepath.SkipDir
					}
					return nil
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
					return nil
				}
				if !entryInfo.Mode().IsRegular() {
					return nil
				}
				files = append(files, nativeFile(path, base, locationID, entryInfo))
				return nil
			})
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		relativeBase := filepath.Dir(match)
		if globby {
			relativeBase = globBase(pattern)
		}
		files = append(files, nativeFile(match, relativeBase, locationID, info))
	}
	return files
}

var errAmbiguousLiteral = errors.New("literal path has ambiguous case-insensitive spellings")

// lstatLiteral stats a literal rule path, falling back to its on-disk
// case-insensitive spelling when the exact one is absent, so destinations
// point at the directory a game actually writes rather than minting a
// second casing of it. A path absent under every spelling reports the
// original not-exist error: it is still a legitimate prospective destination.
// Multiple spellings are ambiguous and must not become a restore target.
func lstatLiteral(path string) (string, fs.FileInfo, error) {
	info, err := os.Lstat(path)
	if err == nil || !os.IsNotExist(err) {
		return path, info, err
	}
	matches := expandLiteral(path)
	if len(matches) == 0 {
		return path, nil, err
	}
	if len(matches) > 1 {
		return path, nil, errAmbiguousLiteral
	}
	resolved := matches[0]
	info, resolvedErr := os.Lstat(resolved)
	if resolvedErr != nil {
		return path, nil, err
	}
	return resolved, info, nil
}

func nativeFile(path, base, locationID string, info fs.FileInfo) target.File {
	relativePath, err := filepath.Rel(base, path)
	if err != nil {
		relativePath = filepath.Base(path)
	}
	return target.File{
		Path:         path,
		LocationID:   locationID,
		RelativePath: relativePath,
		Size:         info.Size(),
		Modified:     info.ModTime(),
	}
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
