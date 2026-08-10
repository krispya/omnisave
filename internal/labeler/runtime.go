package labeler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// Execution limits. A labeler reads a couple of files and formats a line; a
// script that needs more than this is broken, hostile, or fighting a save
// format it no longer understands, and in every case the answer is "no name".
const (
	maxExecutionSteps = 5_000_000
	maxExecutionTime  = 2 * time.Second
)

// script is one loaded labeler: a frozen module whose label function can be
// called concurrently from fresh threads.
type script struct {
	name  string
	keys  []string
	label starlark.Callable
}

// fileOptions is the language dialect scripts run under. The zero value is
// deliberate: no while loops, no recursion, no set literals — labelers
// terminate by construction, with the step limit as the backstop.
var fileOptions = &syntax.FileOptions{}

// loadScript executes a labeler's module once and captures its contract: a
// GAME_KEYS list naming the games it labels and a label function. The module's
// globals are frozen by execution, which is what makes the returned script
// safe to share.
func loadScript(name string, source []byte) (*script, error) {
	thread := &starlark.Thread{Name: "labeler load " + name}
	thread.SetMaxExecutionSteps(maxExecutionSteps)
	globals, err := starlark.ExecFileOptions(fileOptions, thread, name, source, nil)
	if err != nil {
		return nil, fmt.Errorf("labeler %s: %w", name, err)
	}

	label, ok := globals["label"].(starlark.Callable)
	if !ok {
		return nil, fmt.Errorf("labeler %s: must define a label(snapshot) function", name)
	}
	keysValue, ok := globals["GAME_KEYS"].(*starlark.List)
	if !ok || keysValue.Len() == 0 {
		return nil, fmt.Errorf("labeler %s: must declare GAME_KEYS, a list of \"namespace:value\" strings", name)
	}
	keys := make([]string, 0, keysValue.Len())
	for i := 0; i < keysValue.Len(); i++ {
		key, ok := starlark.AsString(keysValue.Index(i))
		if !ok || !validScriptKey(key) {
			return nil, fmt.Errorf("labeler %s: GAME_KEYS entry %v is not a \"namespace:value\" string", name, keysValue.Index(i))
		}
		keys = append(keys, key)
	}
	return &script{name: name, keys: keys, label: label}, nil
}

func validScriptKey(key string) bool {
	namespace, value, found := cutKey(key)
	return found && namespace != "" && value != ""
}

func cutKey(key string) (namespace, value string, found bool) {
	for i := 0; i < len(key); i++ {
		if key[i] == ':' {
			return key[:i], key[i+1:], true
		}
	}
	return "", "", false
}

// run calls the script's label function against snap and returns its answer.
// "" means the script declined (returned None or an empty string); an error
// means it misbehaved. Either way the caller commits, at worst unnamed.
func (s *script) run(ctx context.Context, snap *snapshot) (name string, err error) {
	// A panic inside the interpreter or a host conversion must stay a missing
	// name, never a failed commit.
	defer func() {
		if recovered := recover(); recovered != nil {
			name, err = "", fmt.Errorf("labeler %s: panic: %v", s.name, recovered)
		}
	}()

	thread := &starlark.Thread{Name: "labeler " + s.name}
	thread.SetMaxExecutionSteps(maxExecutionSteps)
	timer := time.AfterFunc(maxExecutionTime, func() { thread.Cancel("labeler deadline exceeded") })
	defer timer.Stop()
	stop := context.AfterFunc(ctx, func() { thread.Cancel(context.Cause(ctx).Error()) })
	defer stop()

	value, err := starlark.Call(thread, s.label, starlark.Tuple{snap}, nil)
	if err != nil {
		return "", errors.New(starlarkErrorMessage(err))
	}
	if value == starlark.None {
		return "", nil
	}
	answer, ok := starlark.AsString(value)
	if !ok {
		return "", fmt.Errorf("label returned %s, want a string or None", value.Type())
	}
	return answer, nil
}

// starlarkErrorMessage keeps the script backtrace, which is the part a labeler
// author needs to see in the server log.
func starlarkErrorMessage(err error) string {
	var evalErr *starlark.EvalError
	if errors.As(err, &evalErr) {
		return evalErr.Backtrace()
	}
	return err.Error()
}
