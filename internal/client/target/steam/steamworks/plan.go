// Package steamworks talks to Steam Cloud for one game the way the game
// itself does: through the Steamworks library the game ships. Games that
// keep their cloud saves behind the store's API trust the store's file
// registry — not any folder — for whether live state exists, so a restore
// is complete only when the registry matches the placed files (FDR-005).
package steamworks

import (
	"path"
	"sort"
	"strings"
)

// RegistryFile is one entry in the store's cloud file registry.
type RegistryFile struct {
	Name string
	Size int64
}

// Write is one registry entry the plan wants written from a local file.
type Write struct {
	// Name is the registry spelling: an existing entry keeps the case the
	// registry already uses, a new entry takes its case from the file path.
	Name string
	// Path is the local file holding the bytes to register.
	Path string
	// Listed reports the registry already carries this name, so the write
	// refreshes content rather than registering a new file.
	Listed bool
}

// Plan is what a placement asks the registry to become. It is derived
// entirely from evidence: the registry's own names prove where the placed
// folder anchors in the store's namespace, and files are only registered
// where files like them already live.
type Plan struct {
	// Anchor is the local directory the registry's names are relative to.
	Anchor string
	// Writes are the files to register, in registry-name order.
	Writes []Write
	// Ineligible are placed files under the anchor that match nothing the
	// registry has ever carried — no entry, and no registered neighbor in
	// the same directory with the same extension. The game never put files
	// like these in its cloud, so the plan does not either.
	Ineligible []string
	// Outside are placed files that do not lie under the anchor.
	Outside []string
	// Extras are registry entries the placement carries no file for. They
	// are left alone — whether a restore must also remove them is an open
	// measurement (FDR-005) — and reported so their effect can be seen.
	Extras []string
}

// PlanReconciliation maps placed local files into the store's registry.
//
// The anchor is never guessed: every registry name that matches exactly one
// placed file by path suffix must strip to the same local directory. A
// registry that matches nothing, or matches inconsistently, proves no
// anchor, and the plan is empty — a wrong anchor would register files under
// names the game has never used, which is worse than reporting that the
// registry could not be reconciled.
func PlanReconciliation(registry []RegistryFile, placed []string) (Plan, bool) {
	anchor, anchored := deriveAnchor(registry, placed)
	if !anchored {
		return Plan{}, false
	}
	listed := make(map[string]RegistryFile, len(registry))
	directories := make(map[string]bool, len(registry))
	extensions := make(map[string]bool, len(registry))
	for _, entry := range registry {
		key := strings.ToLower(entry.Name)
		listed[key] = entry
		directories[strings.ToLower(path.Dir(entry.Name))] = true
		extensions[strings.ToLower(path.Ext(entry.Name))] = true
	}

	plan := Plan{Anchor: anchor}
	carried := make(map[string]bool, len(placed))
	prefix := strings.ToLower(toSlash(anchor)) + "/"
	for _, file := range placed {
		slashed := toSlash(file)
		if !strings.HasPrefix(strings.ToLower(slashed), prefix) {
			plan.Outside = append(plan.Outside, file)
			continue
		}
		name := slashed[len(prefix):]
		key := strings.ToLower(name)
		if entry, exists := listed[key]; exists {
			carried[key] = true
			plan.Writes = append(plan.Writes, Write{Name: entry.Name, Path: file, Listed: true})
			continue
		}
		// A new name is registered only where the registry shows the game
		// keeps files like it: a directory and an extension the registry has
		// carried. The registry root is deliberately not enough on its own —
		// a game's root mixes registered files with deliberately local ones
		// (Slay the Spire 2 registers profile.save but never settings.save),
		// and a directory the game created for cloud files is the stronger
		// precedent.
		if path.Dir(name) != "." && directories[strings.ToLower(path.Dir(name))] &&
			extensions[strings.ToLower(path.Ext(name))] {
			plan.Writes = append(plan.Writes, Write{Name: name, Path: file})
			continue
		}
		plan.Ineligible = append(plan.Ineligible, name)
	}
	for _, entry := range registry {
		if !carried[strings.ToLower(entry.Name)] {
			plan.Extras = append(plan.Extras, entry.Name)
		}
	}
	sort.Slice(plan.Writes, func(left, right int) bool {
		return plan.Writes[left].Name < plan.Writes[right].Name
	})
	sort.Strings(plan.Ineligible)
	sort.Strings(plan.Extras)
	return plan, true
}

// deriveAnchor finds the one local directory the registry's names hang from.
// Names that match several placed files prove nothing and are passed over;
// names that match exactly one file each nominate an anchor, and every
// nomination must agree.
func deriveAnchor(registry []RegistryFile, placed []string) (string, bool) {
	anchor := ""
	found := false
	for _, entry := range registry {
		suffix := "/" + strings.ToLower(toSlash(entry.Name))
		candidate := ""
		matches := 0
		for _, file := range placed {
			slashed := toSlash(file)
			if strings.HasSuffix(strings.ToLower(slashed), suffix) {
				candidate = file[:len(slashed)-len(suffix)]
				matches++
			}
		}
		if matches != 1 {
			continue
		}
		if found && !strings.EqualFold(candidate, anchor) {
			return "", false
		}
		anchor = candidate
		found = true
	}
	return anchor, found
}

func toSlash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}
