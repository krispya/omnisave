package gamesave

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/client/target"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

const (
	maxExecutionSteps = 1_000_000
	maxExecutionTime  = 2 * time.Second
	maxUnits          = 128
)

// script is frozen at load; each discovery gets an isolated, bounded thread.
type script struct {
	id       string
	keys     []string
	discover starlark.Callable
}

func extensionThread(ctx context.Context) (*starlark.Thread, func()) {
	t := &starlark.Thread{Name: "save extension", Print: func(*starlark.Thread, string) {}}
	t.SetMaxExecutionSteps(maxExecutionSteps)
	timer := time.AfterFunc(maxExecutionTime, func() { t.Cancel("deadline") })
	stop := context.AfterFunc(ctx, func() { t.Cancel("cancelled") })
	return t, func() { timer.Stop(); stop() }
}

func loadScript(source []byte) (*script, error) {
	t, stop := extensionThread(context.Background())
	defer stop()
	globals, err := starlark.ExecFileOptions(&syntax.FileOptions{}, t, "rules.star", source, nil)
	if err != nil {
		return nil, fmt.Errorf("save extension could not load")
	}
	id, ok := starlark.AsString(globals["ADAPTER_ID"])
	if !ok || !(omnisave.SaveScope{Kind: omnisave.ScopeSlot, Adapter: id}).Valid() {
		return nil, fmt.Errorf("save extension needs a valid ADAPTER_ID")
	}
	keys, err := stringList(globals["GAME_KEYS"])
	if err != nil || len(keys) == 0 {
		return nil, fmt.Errorf("save extension needs GAME_KEYS")
	}
	for _, key := range keys {
		namespace, value, found := strings.Cut(key, ":")
		if !found || namespace == "" || value == "" {
			return nil, fmt.Errorf("save extension game key must be namespace:value")
		}
	}
	fn, ok := globals["discover"].(starlark.Callable)
	if !ok {
		return nil, fmt.Errorf("save extension needs discover(snapshot)")
	}
	globals.Freeze()
	return &script{id: id, keys: keys, discover: fn}, nil
}

func (s *script) ID() string { return s.id }
func (s *script) Supports(identity target.GameIdentity) bool {
	for _, identifier := range identity.Identifiers {
		for _, key := range s.keys {
			if strings.EqualFold(key, identifier.Namespace+":"+identifier.Value) {
				return true
			}
		}
	}
	return false
}

func (s *script) Discover(ctx context.Context, game target.InstalledGame, whole []target.SaveDestination) (slots []Slot, err error) {
	// Script errors can interpolate native names. Never propagate their text,
	// print output, or panic payloads to logs or user-visible reports.
	defer func() {
		if recover() != nil {
			slots, err = nil, fmt.Errorf("save extension failed")
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("save extension discovery cancelled")
	}
	snap := newDirectorySnapshot(ctx, game, whole)
	t, stop := extensionThread(ctx)
	defer stop()
	value, callErr := starlark.Call(t, s.discover, starlark.Tuple{snap.value()}, nil)
	if callErr != nil {
		return nil, fmt.Errorf("save extension discovery unavailable")
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("save extension discovery cancelled")
	}
	list, ok := value.(*starlark.List)
	if !ok || list.Len() > maxUnits {
		return nil, fmt.Errorf("save extension must return a bounded list of save slots")
	}
	for i := 0; i < list.Len(); i++ {
		slot, err := snap.materialize(s.id, list.Index(i))
		if err != nil {
			return nil, err
		}
		for _, prior := range slots {
			if prior.Save.ID == slot.Save.ID || overlapping(prior.Destination.Locations[0].Path, slot.Destination.Locations[0].Path) {
				return nil, fmt.Errorf("save extension slots overlap")
			}
		}
		slots = append(slots, slot)
	}
	return slots, nil
}

func stringList(value starlark.Value) ([]string, error) {
	list, ok := value.(*starlark.List)
	if !ok {
		return nil, fmt.Errorf("expected a string list")
	}
	strings := make([]string, 0, list.Len())
	for i := 0; i < list.Len(); i++ {
		s, ok := starlark.AsString(list.Index(i))
		if !ok {
			return nil, fmt.Errorf("expected a string list")
		}
		strings = append(strings, s)
	}
	return strings, nil
}

var _ Adapter = (*script)(nil)
