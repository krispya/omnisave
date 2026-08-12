package labeler

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
)

// Built-in labelers are embedded and register themselves through GAME_KEYS.
//
//go:embed builtin/*.star
var builtinScripts embed.FS

// loadBuiltins executes embedded scripts and indexes them by declared game key.
func loadBuiltins() (map[string]*script, error) {
	entries, err := fs.Glob(builtinScripts, "builtin/*.star")
	if err != nil {
		return nil, err
	}
	scripts := make(map[string]*script)
	for _, entry := range entries {
		source, err := builtinScripts.ReadFile(entry)
		if err != nil {
			return nil, err
		}
		loaded, err := loadScript(strings.TrimPrefix(entry, "builtin/"), source)
		if err != nil {
			return nil, err
		}
		for _, key := range loaded.keys {
			key = strings.ToLower(key)
			if holder, taken := scripts[key]; taken {
				return nil, fmt.Errorf("labeler %s: game key %q already registered by %s", loaded.name, key, holder.name)
			}
			scripts[key] = loaded
		}
	}
	return scripts, nil
}
