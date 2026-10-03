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

// Deletion pairs a registry entry with the native path the placement removed.
// Keep both spellings: Steam names can differ in case from local paths.
type Deletion struct {
	Name string
	Path string
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
	// Deletes are registry entries to remove: entries the placement carries
	// no file for whose local file the placement itself removed. The removal
	// is the evidence — the placing flow only removes content a committed
	// revision holds, so the entry's bytes stay recoverable — and without
	// the delete the store resurrects the file at the game's next launch,
	// where the game may act on it (FDR-005, decision 13).
	Deletes []Deletion
	// Extras are registry entries the placement carries no file for and no
	// removal vouches for. They are left alone and reported so their effect
	// can be seen.
	Extras []string
}

// PlanReconciliation maps placed local files into the store's registry.
// removed are local paths the placement removed from the save folder;
// a registry entry anchored at one of them is planned for deletion.
//
// The anchor is never guessed: every registry name that matches exactly one
// placed file by path suffix must strip to the same local directory. A
// registry that matches nothing, or matches inconsistently, proves no
// anchor, and the plan is empty — a wrong anchor would register files under
// names the game has never used, which is worse than reporting that the
// registry could not be reconciled. Deletions hang from the same anchor,
// so nothing is ever deleted on a guess either.
func PlanReconciliation(registry []RegistryFile, placed, removed []string) (Plan, bool) {
	anchor, anchored := deriveAnchor(registry, placed)
	if !anchored {
		return Plan{}, false
	}
	listed := make(map[string]RegistryFile, len(registry))
	precedent := make(map[string]bool, len(registry))
	for _, entry := range registry {
		key := strings.ToLower(entry.Name)
		listed[key] = entry
		precedent[precedentKey(entry.Name)] = true
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
		// keeps files exactly like it: the same directory carrying the same
		// extension. The two are one piece of evidence, not two — an
		// extension seen only elsewhere authorizes nothing here, or a
		// registry with .dat replays in one folder would admit a .dat the
		// game keeps local in another. The registry root is likewise never
		// enough on its own: a game's root mixes registered files with
		// deliberately local ones (Slay the Spire 2 registers profile.save
		// but never settings.save).
		if path.Dir(name) != "." && precedent[precedentKey(name)] {
			plan.Writes = append(plan.Writes, Write{Name: name, Path: file})
			continue
		}
		plan.Ineligible = append(plan.Ineligible, name)
	}
	removedUnder := make(map[string]string, len(removed))
	for _, file := range removed {
		slashed := strings.ToLower(toSlash(file))
		if strings.HasPrefix(slashed, prefix) {
			removedUnder[slashed[len(prefix):]] = file
		}
	}
	for _, entry := range registry {
		if carried[strings.ToLower(entry.Name)] {
			continue
		}
		if nativePath, exists := removedUnder[strings.ToLower(entry.Name)]; exists {
			plan.Deletes = append(plan.Deletes, Deletion{Name: entry.Name, Path: nativePath})
			continue
		}
		plan.Extras = append(plan.Extras, entry.Name)
	}
	sortPlan(&plan)
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

// precedentKey is where-and-what evidence for one registry name: its
// directory and extension together, compared case-insensitively.
func precedentKey(name string) string {
	return strings.ToLower(path.Dir(name)) + "\x00" + strings.ToLower(path.Ext(name))
}

func toSlash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

func sortPlan(plan *Plan) {
	sort.Slice(plan.Writes, func(left, right int) bool {
		return plan.Writes[left].Name < plan.Writes[right].Name
	})
	sort.Strings(plan.Ineligible)
	sort.Slice(plan.Deletes, func(left, right int) bool {
		return plan.Deletes[left].Name < plan.Deletes[right].Name
	})
	sort.Strings(plan.Extras)
}
