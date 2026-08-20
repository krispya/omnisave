// Package ludusavi adapts the Ludusavi manifest into save profiles.
package ludusavi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// New parses the supported subset of a Ludusavi manifest. Entries without a
// Steam id or without save rules are dropped, so Find answers only for games
// the manifest actually locates saves for.
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
		kept := rules(entry.Files)
		if len(kept) == 0 {
			continue
		}
		// The community manifest reuses a Steam id when wiki pages split or
		// a game is renamed, and either page may hold the rules a given
		// build looks for. Every rule-bearing title contributes its rules;
		// the first in sorted order names the profile deterministically, and
		// an exact rule two pages both spell lands in the profile once while
		// distinct OS and store constraints on the same template survive.
		existing, exists := provider.bySteamID[steamID]
		if !exists {
			provider.bySteamID[steamID] = saveprofile.Profile{
				Provider:   "ludusavi",
				ProviderID: steamID,
				Title:      title,
				Rules:      kept,
			}
			continue
		}
		contributed := make(map[saveprofile.Rule]bool, len(existing.Rules))
		for _, rule := range existing.Rules {
			contributed[rule] = true
		}
		for _, rule := range kept {
			if !contributed[rule] {
				existing.Rules = append(existing.Rules, rule)
				contributed[rule] = true
			}
		}
		provider.bySteamID[steamID] = existing
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
	originals := make([]string, 0, len(paths))
	for path := range paths {
		originals = append(originals, path)
	}
	sort.Strings(originals)
	normalized := make(map[string]*manifestPath, len(paths))
	templates := make([]string, 0, len(paths))
	for _, original := range originals {
		template := original
		// The manifest spells some Windows rules from the drive root as
		// C:/Users/<osUserName>/…. That directory is what <home> already
		// names on every runtime, and a Proton prefix spells users in
		// lowercase, so the literal path would never match there.
		if rest, ok := strings.CutPrefix(template, "C:/Users/<osUserName>/"); ok {
			template = "<home>/" + rest
		}
		if existing, exists := normalized[template]; exists {
			normalized[template] = mergePaths(existing, paths[original])
			continue
		}
		normalized[template] = paths[original]
		templates = append(templates, template)
	}
	sort.Strings(templates)
	var result []saveprofile.Rule
	for _, template := range templates {
		path := normalized[template]
		if path != nil && !isSave(path.Tags) {
			continue
		}
		// Steam Cloud's userdata directories already belong to the steam
		// adapter, which attributes them to an account. A profile rule over
		// the same files would report every Cloud save a second time. The
		// manifest spells those directories through <root>, <home>,
		// <xdgData>, and Application Support, in either casing.
		lowered := strings.ToLower(template)
		if strings.HasPrefix(lowered, "<root>/userdata") || strings.Contains(lowered, "steam/userdata") {
			continue
		}
		constraints := []manifestWhen{{}}
		if path != nil && len(path.When) > 0 {
			constraints = path.When
		}
		for _, constraint := range constraints {
			result = append(result, saveprofile.Rule{
				ID:    locationID(template),
				Path:  template,
				OS:    normalizeOS(constraint.OS),
				Store: strings.ToLower(constraint.Store),
				Kind:  "save",
			})
		}
	}
	return result
}

// locationID derives a rule's identity from its template alone. The id is
// recorded in revision file paths on the server, so it must keep meaning
// across manifest refreshes that add, remove, or reorder a game's other
// rules — a position could not.
func locationID(template string) string {
	sum := sha256.Sum256([]byte(template))
	return hex.EncodeToString(sum[:4])
}

// mergePaths combines two manifest spellings of the same template. Either
// side declaring the path a save keeps it one, and both sides' constraints
// survive.
func mergePaths(left, right *manifestPath) *manifestPath {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	merged := &manifestPath{}
	seenConstraints := make(map[manifestWhen]bool, len(left.When)+len(right.When))
	for _, constraint := range append(append([]manifestWhen(nil), left.When...), right.When...) {
		if !seenConstraints[constraint] {
			merged.When = append(merged.When, constraint)
			seenConstraints[constraint] = true
		}
	}
	if !isSave(left.Tags) && !isSave(right.Tags) {
		merged.Tags = left.Tags
	}
	return merged
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
