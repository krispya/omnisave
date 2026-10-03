package gamesave

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
)

// Extensions register themselves by GAME_KEYS, just like revision labelers.
// Each directory also carries fixtures run by the shared extension test harness.
//
//go:embed builtin/*/rules.star
var builtinScripts embed.FS

// Builtins loads shipped, sandboxed save extensions. Invalid or conflicting
// registrations fail discovery explicitly rather than choosing a random owner.
func Builtins() ([]Adapter, error) {
	return LoadExtensions(builtinScripts, "builtin/*/rules.star")
}

// LoadExtensions loads an extension bundle from a filesystem. Callers explicitly
// provide bundles; this does not search user folders or download executable rules.
func LoadExtensions(source fs.FS, pattern string) ([]Adapter, error) {
	entries, err := fs.Glob(source, pattern)
	if err != nil {
		return nil, err
	}
	var adapters []Adapter
	keys, ids := map[string]bool{}, map[string]bool{}
	for _, entry := range entries {
		content, err := fs.ReadFile(source, entry)
		if err != nil {
			return nil, fmt.Errorf("save extension source unavailable")
		}
		loaded, err := loadScript(content)
		if err != nil {
			return nil, err
		}
		if ids[loaded.id] {
			return nil, fmt.Errorf("duplicate save extension ID")
		}
		ids[loaded.id] = true
		for _, key := range loaded.keys {
			key = strings.ToLower(key)
			if keys[key] {
				return nil, fmt.Errorf("duplicate save extension game key")
			}
			keys[key] = true
		}
		adapters = append(adapters, loaded)
	}
	return adapters, nil
}
