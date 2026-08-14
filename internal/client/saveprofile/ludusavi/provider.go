// Package ludusavi adapts the Ludusavi manifest into save profiles.
package ludusavi

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/krisbaumgartner/omnisave/internal/client/saveprofile"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

type Provider struct {
	bySteamID map[string]saveprofile.Profile
}

// New parses the supported subset of a Ludusavi manifest.
func New(data []byte) (*Provider, error) {
	var manifest map[string]manifestEntry
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse Ludusavi manifest: %w", err)
	}
	provider := &Provider{bySteamID: make(map[string]saveprofile.Profile)}
	titles := make([]string, 0, len(manifest))
	for title := range manifest {
		titles = append(titles, title)
	}
	sort.Strings(titles)
	for _, title := range titles {
		entry := manifest[title]
		steamID, ok := externalID(entry.Steam.ID)
		if !ok {
			continue
		}
		// The community manifest reuses a Steam id when wiki pages split or a
		// game is renamed. Titles arrive sorted, so the first entry wins
		// deterministically, and a base title outranks its longer variants.
		if _, exists := provider.bySteamID[steamID]; exists {
			continue
		}
		provider.bySteamID[steamID] = saveprofile.Profile{
			Provider:   "ludusavi",
			ProviderID: steamID,
			Title:      title,
			Rules:      rules(entry.Files),
		}
	}
	return provider, nil
}

func (p *Provider) Find(ctx context.Context, identity target.GameIdentity) (*saveprofile.Profile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	steamID, ok := identity.Identifier("steam.app")
	if !ok || steamID == "" {
		return nil, saveprofile.ErrNotFound
	}
	profile, ok := p.bySteamID[steamID]
	if !ok {
		return nil, saveprofile.ErrNotFound
	}
	profile.Rules = append([]saveprofile.Rule(nil), profile.Rules...)
	return &profile, nil
}

type manifestEntry struct {
	Files map[string]*manifestPath `yaml:"files"`
	Steam manifestSteam            `yaml:"steam"`
}

type manifestSteam struct {
	ID any `yaml:"id"`
}

type manifestPath struct {
	Tags []string       `yaml:"tags"`
	When []manifestWhen `yaml:"when"`
}

type manifestWhen struct {
	OS    string `yaml:"os"`
	Store string `yaml:"store"`
}

func rules(paths map[string]*manifestPath) []saveprofile.Rule {
	templates := make([]string, 0, len(paths))
	for path := range paths {
		templates = append(templates, path)
	}
	sort.Strings(templates)
	var result []saveprofile.Rule
	for _, template := range templates {
		path := paths[template]
		if path != nil && !isSave(path.Tags) {
			continue
		}
		// Steam Cloud's userdata directories already belong to the steam
		// adapter, which attributes them to an account. A profile rule over
		// the same files would report every Cloud save a second time.
		if strings.HasPrefix(template, "<root>/userdata") {
			continue
		}
		constraints := []manifestWhen{{}}
		if path != nil && len(path.When) > 0 {
			constraints = path.When
		}
		for _, constraint := range constraints {
			result = append(result, saveprofile.Rule{
				ID:    strconv.Itoa(len(result) + 1),
				Path:  template,
				OS:    normalizeOS(constraint.OS),
				Store: strings.ToLower(constraint.Store),
				Kind:  "save",
			})
		}
	}
	return result
}

func isSave(tags []string) bool {
	if len(tags) == 0 {
		return true
	}
	for _, tag := range tags {
		if strings.EqualFold(tag, "save") {
			return true
		}
	}
	return false
}

func normalizeOS(value string) string {
	switch strings.ToLower(value) {
	case "mac", "macos", "darwin":
		return saveprofile.OSMacOS
	case "windows":
		return saveprofile.OSWindows
	case "linux":
		return saveprofile.OSLinux
	default:
		return strings.ToLower(value)
	}
}

func externalID(value any) (string, bool) {
	switch id := value.(type) {
	case int:
		if id > 0 {
			return strconv.Itoa(id), true
		}
	case int64:
		if id > 0 {
			return strconv.FormatInt(id, 10), true
		}
	case uint64:
		if id > 0 {
			return strconv.FormatUint(id, 10), true
		}
	case string:
		if parsed, err := strconv.ParseUint(id, 10, 64); err == nil && parsed > 0 {
			return strconv.FormatUint(parsed, 10), true
		}
	}
	return "", false
}

var _ saveprofile.Provider = (*Provider)(nil)
