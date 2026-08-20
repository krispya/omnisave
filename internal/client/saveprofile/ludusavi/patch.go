package ludusavi

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ApplyPatches adds checked-in corrections to a Ludusavi manifest before it
// is pruned. Patch names identify their source files in validation errors.
func ApplyPatches(data []byte, patches map[string][]byte) ([]byte, error) {
	var manifest map[string]manifestEntry
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse Ludusavi manifest: %w", err)
	}

	names := make([]string, 0, len(patches))
	for name := range patches {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		patch, err := parsePatch(patches[name])
		if err != nil {
			return nil, fmt.Errorf("apply Ludusavi patch %s: %w", name, err)
		}
		entry, exists := manifest[patch.Title]
		if !exists {
			return nil, fmt.Errorf("apply Ludusavi patch %s: title %q is absent upstream", name, patch.Title)
		}
		upstreamID, ok := externalID(entry.Steam.ID)
		if !ok || upstreamID != patch.SteamID {
			return nil, fmt.Errorf(
				"apply Ludusavi patch %s: title %q has Steam id %q upstream, expected %q",
				name, patch.Title, upstreamID, patch.SteamID,
			)
		}
		if entry.Files == nil {
			entry.Files = make(map[string]*manifestPath)
		}
		for path, addition := range patch.AddFiles {
			entry.Files[path] = mergePaths(entry.Files[path], addition)
		}
		manifest[patch.Title] = entry
	}

	patched, err := yaml.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("marshal patched Ludusavi manifest: %w", err)
	}
	return patched, nil
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
	return patch, nil
}
