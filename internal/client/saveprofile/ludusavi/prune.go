package ludusavi

import (
	"fmt"
	"sort"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Prune rewrites a full Ludusavi manifest into the subset this provider
// consumes: entries with a Steam id, keeping only the rules they contribute.
// Pruned output is built from the same rule interpretation New uses, so it
// parses back into exactly the profiles the full manifest would have produced.
func Prune(data []byte) ([]byte, error) {
	var manifest map[string]manifestEntry
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse Ludusavi manifest: %w", err)
	}
	titles := make([]string, 0, len(manifest))
	for title := range manifest {
		titles = append(titles, title)
	}
	sort.Strings(titles)
	pruned := make(map[string]prunedEntry)
	seen := make(map[string]bool)
	for _, title := range titles {
		entry := manifest[title]
		steamID, ok := externalID(entry.Steam.ID)
		if !ok || seen[steamID] {
			continue
		}
		// Claim the id before checking for rules, exactly as New does: an
		// entry without save rules still shadows later duplicates of its id.
		seen[steamID] = true
		kept := rules(entry.Files)
		if len(kept) == 0 {
			continue
		}
		id, err := strconv.ParseUint(steamID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("prune Ludusavi entry %s: %w", title, err)
		}
		files := make(map[string]*prunedPath, len(kept))
		for _, rule := range kept {
			path := files[rule.Path]
			if path == nil {
				path = &prunedPath{}
				files[rule.Path] = path
			}
			path.When = append(path.When, prunedWhen{OS: rule.OS, Store: rule.Store})
		}
		pruned[title] = prunedEntry{Files: files, Steam: prunedSteam{ID: id}}
	}
	return yaml.Marshal(pruned)
}

// The pruned shapes mirror the parsed manifest shapes, with empty fields
// omitted. Tags are dropped entirely: untagged paths already count as saves.
type prunedEntry struct {
	Files map[string]*prunedPath `yaml:"files"`
	Steam prunedSteam            `yaml:"steam"`
}

type prunedSteam struct {
	ID uint64 `yaml:"id"`
}

type prunedPath struct {
	When []prunedWhen `yaml:"when,omitempty"`
}

type prunedWhen struct {
	OS    string `yaml:"os,omitempty"`
	Store string `yaml:"store,omitempty"`
}
