package labeler

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
)

// Built-in labelers ship inside the binary, one script per game. Each script
// is self-registering through its GAME_KEYS declaration, so supporting a new
// game is one new .star file and no Go changes.
//
//go:embed builtin/*.star
var builtinScripts embed.FS

// loadBuiltins executes every embedded script and indexes it by its declared
// game keys. Built-ins are first-party code: any load failure or key collision
// is a bug, not a condition to tolerate.
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
