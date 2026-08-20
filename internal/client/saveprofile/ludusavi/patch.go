package ludusavi

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile"
)

// ApplyPatches adds checked-in additive corrections to a Ludusavi manifest
// before it is pruned. Patches currently add save rules only; their names
// identify source files in validation errors.
func ApplyPatches(data []byte, patches map[string][]byte) ([]byte, error) {
	manifest, err := parseManifest(data)
	if err != nil {
		return nil, err
	}

	for _, name := range patchNames(patches) {
		patch, err := parsePatch(patches[name])
		if err != nil {
			return nil, fmt.Errorf("apply Ludusavi patch %s: %w", name, err)
		}
		entry, err := patchTarget(manifest, patch)
		if err != nil {
			return nil, fmt.Errorf("apply Ludusavi patch %s: %w", name, err)
		}
		before := profileRules(manifest, patch.SteamID)
		if entry.Files == nil {
			entry.Files = make(map[string]*manifestPath)
		}
		for path, addition := range patch.AddFiles {
			entry.Files[path] = mergePaths(entry.Files[path], addition)
		}
		manifest[patch.Title] = entry
		if !addsRule(before, profileRules(manifest, patch.SteamID)) {
			return nil, fmt.Errorf(
				"apply Ludusavi patch %s: adds no effective save rule; remove it if upstream now has the correction",
				name,
			)
		}
	}

	patched, err := yaml.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("marshal patched Ludusavi manifest: %w", err)
	}
	return patched, nil
}

// validatePatchesApplied verifies that compiled manifest data contains every
// effective rule declared by the checked-in additive patches.
func validatePatchesApplied(data []byte, patches map[string][]byte) error {
	manifest, err := parseManifest(data)
	if err != nil {
		return err
	}
	for _, name := range patchNames(patches) {
		patch, err := parsePatch(patches[name])
		if err != nil {
			return fmt.Errorf("validate Ludusavi patch %s: %w", name, err)
		}
		compiled := profileRules(manifest, patch.SteamID)
		for rule := range ruleSet(rules(patch.AddFiles)) {
			if !compiled[rule] {
				return fmt.Errorf("validate Ludusavi patch %s: embedded manifest is missing %q", name, rule.Path)
			}
		}
	}
	return nil
}

type manifestPatch struct {
	SteamID  string                   `yaml:"steamId"`
	Title    string                   `yaml:"title"`
	Reason   string                   `yaml:"reason"`
	Upstream string                   `yaml:"upstream"`
	AddFiles map[string]*manifestPath `yaml:"addFiles"`
}

func parsePatch(data []byte) (manifestPatch, error) {
	var patch manifestPatch
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&patch); err != nil {
		return manifestPatch{}, fmt.Errorf("parse: %w", err)
	}
	patch.SteamID = strings.TrimSpace(patch.SteamID)
	patch.Title = strings.TrimSpace(patch.Title)
	patch.Reason = strings.TrimSpace(patch.Reason)
	patch.Upstream = strings.TrimSpace(patch.Upstream)
	normalizedSteamID, ok := externalID(patch.SteamID)
	if !ok {
		return manifestPatch{}, fmt.Errorf("steamId must be a positive integer string")
	}
	patch.SteamID = normalizedSteamID
	if patch.Title == "" {
		return manifestPatch{}, fmt.Errorf("title is required")
	}
	if patch.Reason == "" {
		return manifestPatch{}, fmt.Errorf("reason is required")
	}
	if patch.Upstream == "" {
		return manifestPatch{}, fmt.Errorf("upstream is required")
	}
	if len(patch.AddFiles) == 0 {
		return manifestPatch{}, fmt.Errorf("addFiles must contain at least one path")
	}
	for path := range patch.AddFiles {
		if strings.TrimSpace(path) == "" {
			return manifestPatch{}, fmt.Errorf("addFiles contains an empty path")
		}
	}
	addedRules := rules(patch.AddFiles)
	if len(addedRules) == 0 {
		return manifestPatch{}, fmt.Errorf("addFiles must contain at least one save rule")
	}
	for _, rule := range addedRules {
		if rule.OS != "" && rule.OS != saveprofile.OSWindows && rule.OS != saveprofile.OSLinux && rule.OS != saveprofile.OSMacOS {
			return manifestPatch{}, fmt.Errorf("addFiles rule %q has unsupported OS %q", rule.Path, rule.OS)
		}
		if rule.Store != "" && rule.Store != "steam" {
			return manifestPatch{}, fmt.Errorf("addFiles rule %q has unsupported store %q", rule.Path, rule.Store)
		}
	}
	return patch, nil
}

func parseManifest(data []byte) (map[string]manifestEntry, error) {
	var manifest map[string]manifestEntry
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse Ludusavi manifest: %w", err)
	}
	return manifest, nil
}

func patchNames(patches map[string][]byte) []string {
	names := make([]string, 0, len(patches))
	for name := range patches {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func patchTarget(manifest map[string]manifestEntry, patch manifestPatch) (manifestEntry, error) {
	entry, exists := manifest[patch.Title]
	if !exists {
		return manifestEntry{}, fmt.Errorf("title %q is absent upstream", patch.Title)
	}
	upstreamID, ok := externalID(entry.Steam.ID)
	if !ok || upstreamID != patch.SteamID {
		return manifestEntry{}, fmt.Errorf(
			"title %q has Steam id %q upstream, expected %q",
			patch.Title, upstreamID, patch.SteamID,
		)
	}
	return entry, nil
}

// profileRules mirrors New's merge by Steam id without depending on the title
// chosen for the compiled profile.
func profileRules(manifest map[string]manifestEntry, steamID string) map[saveprofile.Rule]bool {
	result := make(map[saveprofile.Rule]bool)
	for _, entry := range manifest {
		entryID, ok := externalID(entry.Steam.ID)
		if !ok || entryID != steamID {
			continue
		}
		for _, rule := range rules(entry.Files) {
			result[rule] = true
		}
	}
	return result
}

func ruleSet(items []saveprofile.Rule) map[saveprofile.Rule]bool {
	result := make(map[saveprofile.Rule]bool, len(items))
	for _, rule := range items {
		result[rule] = true
	}
	return result
}

func addsRule(before, after map[saveprofile.Rule]bool) bool {
	for rule := range after {
		if !before[rule] {
			return true
		}
	}
	return false
}
