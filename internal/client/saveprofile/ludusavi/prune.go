package ludusavi

import (
	"fmt"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Prune rewrites a full Ludusavi manifest into the subset this provider
// consumes. It is built from New's own parse, so the pruned copy yields the
// same profiles as the full manifest by construction; the client can ship a
// small manifest without a second interpretation of the format.
func Prune(data []byte) ([]byte, error) {
	provider, err := New(data)
	if err != nil {
		return nil, err
	}
	pruned := make(map[string]prunedEntry, len(provider.bySteamID))
	for _, profile := range provider.bySteamID {
		id, err := strconv.ParseUint(profile.ProviderID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("prune Ludusavi entry %s: %w", profile.Title, err)
		}
		files := make(map[string]*prunedPath, len(profile.Rules))
		for _, rule := range profile.Rules {
			path := files[rule.Path]
			if path == nil {
				path = &prunedPath{}
				files[rule.Path] = path
			}
			path.When = append(path.When, prunedWhen{OS: rule.OS, Store: rule.Store})
		}
		pruned[profile.Title] = prunedEntry{Files: files, Steam: prunedSteam{ID: id}}
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
