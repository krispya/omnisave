package gamesave

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// directorySnapshot exposes opaque roots and read-only immediate directories.
// Native absolute paths stay in the host; extensions receive no write, network,
// import, or file-content capability. Capture always includes the complete slot.
type directorySnapshot struct {
	ctx   context.Context
	game  target.InstalledGame
	roots []string
}

func newDirectorySnapshot(ctx context.Context, game target.InstalledGame, whole []target.SaveDestination) *directorySnapshot {
	seen := map[string]bool{}
	for _, destination := range whole {
		for _, location := range destination.Locations {
			if location.Kind == target.SaveLocationDirectory && filepath.IsAbs(location.Path) {
				seen[filepath.Clean(location.Path)] = true
			}
		}
	}
	roots := make([]string, 0, len(seen))
	for root := range seen {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	return &directorySnapshot{ctx: ctx, game: game, roots: roots}
}

func (s *directorySnapshot) value() starlark.Value {
	var roots []starlark.Value
	for i, root := range s.roots {
		roots = append(roots, starlarkstruct.FromStringDict(starlark.String("root"), starlark.StringDict{
			"id": starlark.MakeInt(i), "name": starlark.String(filepath.Base(root)), "parent": starlark.String(filepath.Base(filepath.Dir(root))),
		}))
	}
	targetName, _, _ := strings.Cut(s.game.TargetID, ":")
	return starlarkstruct.FromStringDict(starlark.String("snapshot"), starlark.StringDict{
		"roots": starlark.NewList(roots), "target": starlark.String(targetName),
		"directories": starlark.NewBuiltin("directories", s.directories),
	})
}

func (s *directorySnapshot) directories(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var id int
	if err := starlark.UnpackArgs("directories", args, kwargs, "root", &id); err != nil {
		return nil, err
	}
	if id < 0 || id >= len(s.roots) {
		return nil, fmt.Errorf("unknown root")
	}
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	root := s.roots[id]
	if err := safeDirectory(root, ""); err != nil {
		return nil, err
	}
	// Bound enumeration before loading entries, rather than trusting the script
	// step budget to constrain filesystem allocation.
	f, err := os.Open(root)
	if os.IsNotExist(err) {
		return starlark.NewList(nil), nil
	}
	if err != nil {
		return nil, fmt.Errorf("save root unavailable")
	}
	defer f.Close()
	entries, err := f.ReadDir(4097)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("save root unavailable")
	}
	if len(entries) > 4096 {
		return nil, fmt.Errorf("save root has too many directories")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var names []starlark.Value
	for _, entry := range entries {
		if entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			names = append(names, starlark.String(entry.Name()))
		}
	}
	return starlark.NewList(names), nil
}

func (s *directorySnapshot) materialize(adapter string, value starlark.Value) (Slot, error) {
	slot := Slot{}
	d, ok := value.(*starlark.Dict)
	if !ok {
		return slot, fmt.Errorf("save extension returned an invalid save slot")
	}
	get := func(key string) starlark.Value { v, _, _ := d.Get(starlark.String(key)); return v }
	rootID, err := starlark.AsInt32(get("root"))
	if err != nil || rootID < 0 || rootID >= len(s.roots) {
		return slot, fmt.Errorf("save extension returned an unknown root")
	}
	parent, okParent := starlark.AsString(get("parent"))
	directory, okDirectory := starlark.AsString(get("directory"))
	key, okKey := starlark.AsString(get("key"))
	label, okLabel := starlark.AsString(get("label"))
	location, okLocation := starlark.AsString(get("location"))
	if !okParent || !relativePath(parent, true) || !okDirectory || !relativePath(directory, false) || !okKey || !identifier(key) || !okLabel || label == "" || len(label) > 100 || strings.IndexFunc(label, unicode.IsControl) >= 0 || !okLocation || !identifier(location) {
		return slot, fmt.Errorf("save extension returned invalid save slot metadata")
	}
	root := s.roots[rootID]
	relative := path.Join(parent, directory)
	if err := safeDirectory(root, relative); err != nil {
		return slot, err
	}
	anchor := filepath.Join(root, filepath.FromSlash(parent))
	native := filepath.Join(anchor, filepath.FromSlash(directory))
	scope := omnisave.SaveScope{Kind: omnisave.ScopeSlot, Adapter: adapter}
	// Identity follows the extension's anchor and key, never the absolute
	// path, so moving a home or library folder keeps the recorded choice. The
	// anchor is hashed because it can name a private account.
	digest := sha256.Sum256([]byte(parent))
	id := fmt.Sprintf("%s:slot:%s:%x:%s", s.game.ID, adapter, digest[:12], key)
	cloud, err := cloudMembership(get("cloud"), anchor, directory)
	if err != nil {
		return slot, err
	}
	files, err := DirectoryFiles(s.ctx, native, location)
	if err != nil {
		return slot, err
	}
	slot.Destination = target.SaveDestination{ID: id, TargetID: s.game.TargetID, GameID: s.game.ID, Kind: "local", Scope: scope, Slot: label, Cloud: cloud,
		Locations: []target.SaveLocation{{ID: location, Path: native, Kind: target.SaveLocationDirectory}}}
	slot.Save = target.Save{ID: id, TargetID: s.game.TargetID, GameID: s.game.ID, Kind: "local", Scope: scope, Slot: label, Cloud: cloud, Files: files}
	return slot, nil
}

func cloudMembership(value starlark.Value, anchor, directory string) (*target.CloudScope, error) {
	if value == nil || value == starlark.None {
		return nil, nil
	}
	d, ok := value.(*starlark.Dict)
	if !ok {
		return nil, fmt.Errorf("save extension returned invalid cloud membership")
	}
	get := func(key string) starlark.Value { v, _, _ := d.Get(starlark.String(key)); return v }
	account, ok := starlark.AsString(get("account_id"))
	files, err := stringList(get("files"))
	if !ok || account == "" || err != nil {
		return nil, fmt.Errorf("save extension returned invalid cloud membership")
	}
	for _, name := range files {
		if !relativePath(name, false) {
			return nil, fmt.Errorf("save extension cloud path escapes its save slot")
		}
	}
	directories, ok := get("directories").(*starlark.List)
	if !ok {
		return nil, fmt.Errorf("save extension returned invalid cloud directories")
	}
	cloud := &target.CloudScope{Root: anchor, Prefix: directory, AccountID: account, Files: files}
	for i := 0; i < directories.Len(); i++ {
		entry, ok := directories.Index(i).(*starlark.Dict)
		if !ok {
			return nil, fmt.Errorf("save extension returned invalid cloud directory")
		}
		p, _, _ := entry.Get(starlark.String("path"))
		e, _, _ := entry.Get(starlark.String("extension"))
		name, okName := starlark.AsString(p)
		extension, okExtension := starlark.AsString(e)
		if !okName || !relativePath(name, false) || !okExtension || !strings.HasPrefix(extension, ".") || !identifier(strings.TrimPrefix(extension, ".")) {
			return nil, fmt.Errorf("save extension returned invalid cloud directory")
		}
		cloud.Directories = append(cloud.Directories, target.CloudDirectory{Path: name, Extension: extension})
	}
	return cloud, nil
}

func relativePath(value string, empty bool) bool {
	if value == "" {
		return empty
	}
	return value != "." && !strings.HasPrefix(value, "/") && path.Clean(value) == value && value != ".." && !strings.HasPrefix(value, "../") && !strings.ContainsAny(value, "\\:\x00")
}

func identifier(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return value != "." && value != ".."
}

// safeDirectory rejects symlinks in every extension-selected component. Missing
// descendants are valid empty destinations; existing non-directories are not.
func safeDirectory(root, relative string) error {
	native := root
	components := []string{""}
	if relative != "" {
		components = append(components, strings.Split(relative, "/")...)
	}
	for _, component := range components {
		native = filepath.Join(native, component)
		info, err := os.Lstat(native)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("save extension directory unavailable")
		}
	}
	return nil
}

func overlapping(a, b string) bool {
	r, err := filepath.Rel(a, b)
	if err == nil && (r == "." || r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))) {
		return true
	}
	r, err = filepath.Rel(b, a)
	return err == nil && (r == "." || r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)))
}
