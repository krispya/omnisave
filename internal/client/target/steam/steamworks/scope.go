package steamworks

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

// PlanScopedReconciliation uses game-provided ownership and cloud eligibility,
// including an empty registry. Sibling slots and shared account files are
// outside this plan; unrelated entries inside the selected slot remain Extras.
// Names compare case-insensitively, as in PlanReconciliation: an existing entry
// keeps the registry's spelling.
func PlanScopedReconciliation(registry []RegistryFile, placed, removed []string, scope target.CloudScope) (Plan, bool) {
	if !filepath.IsAbs(scope.Root) || !safeCloudName(scope.Prefix) || scope.AccountID == "" {
		return Plan{}, false
	}
	for _, name := range scope.Files {
		if !safeCloudName(name) {
			return Plan{}, false
		}
	}
	for _, rule := range scope.Directories {
		if !safeCloudName(rule.Path) || rule.Extension == "" || strings.ContainsAny(rule.Extension, "/\\") {
			return Plan{}, false
		}
	}
	plan := Plan{Anchor: filepath.Clean(scope.Root)}
	anchorPrefix := strings.TrimSuffix(toSlash(plan.Anchor), "/") + "/"
	ownedPrefix := scope.Prefix + "/"
	// owned is the registry-relative name under the selected slot, or false
	// for a path outside it.
	owned := func(file string) (string, bool) {
		slashed := toSlash(filepath.Clean(file))
		if !hasPrefixFold(slashed, anchorPrefix) {
			return "", false
		}
		name := slashed[len(anchorPrefix):]
		return name, hasPrefixFold(name, ownedPrefix) && safeCloudName(name)
	}
	listed := map[string]RegistryFile{}
	for _, entry := range registry {
		if hasPrefixFold(entry.Name, ownedPrefix) {
			listed[strings.ToLower(entry.Name)] = entry
		}
	}
	carried := map[string]bool{}
	for _, file := range placed {
		name, ok := owned(file)
		if !ok {
			plan.Outside = append(plan.Outside, file)
			continue
		}
		if !eligible(name[len(ownedPrefix):], scope) {
			plan.Ineligible = append(plan.Ineligible, name)
			continue
		}
		key := strings.ToLower(name)
		carried[key] = true
		if entry, exists := listed[key]; exists {
			plan.Writes = append(plan.Writes, Write{Name: entry.Name, Path: file, Listed: true})
		} else {
			plan.Writes = append(plan.Writes, Write{Name: name, Path: file})
		}
	}
	removedByName := map[string]string{}
	for _, file := range removed {
		if name, ok := owned(file); ok {
			removedByName[strings.ToLower(name)] = file
		}
	}
	for key, entry := range listed {
		if carried[key] {
			continue
		}
		if native, ok := removedByName[key]; ok {
			plan.Deletes = append(plan.Deletes, Deletion{Name: entry.Name, Path: native})
		} else {
			plan.Extras = append(plan.Extras, entry.Name)
		}
	}
	sortPlan(&plan)
	return plan, true
}

// eligible reports whether the game registers a file at this slot-relative
// path: a listed file, or a direct child of a listed directory with its extension.
func eligible(relative string, scope target.CloudScope) bool {
	for _, allowed := range scope.Files {
		if strings.EqualFold(relative, allowed) {
			return true
		}
	}
	for _, rule := range scope.Directories {
		if strings.EqualFold(path.Dir(relative), rule.Path) && strings.EqualFold(path.Ext(relative), rule.Extension) {
			return true
		}
	}
	return false
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

func safeCloudName(name string) bool {
	return name != "" && name != "." && !path.IsAbs(name) && path.Clean(name) == name && !strings.Contains(name, "\\") && !strings.ContainsAny(name, "\x00:") && name != ".." && !strings.HasPrefix(name, "../")
}
