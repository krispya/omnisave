package labeler

import (
	"context"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	starlarkjson "go.starlark.net/lib/json"
	"go.starlark.net/starlark"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// Content reads are bounded per file and per labeling run.
const (
	maxFileBytes  = 8 << 20
	maxTotalBytes = 32 << 20
)

// snapshot exposes a revision's canonical paths and content to a labeler.
//
// Free (manifest only):    paths([pattern]), exists(path), size(path)
// Reads the file:          json(path), text(path), bytes(path)
type snapshot struct {
	ctx       context.Context
	byPath    map[string]omnisave.RevisionFile
	sorted    []string
	artifacts ArtifactOpener
	budget    int64
	jsonCache map[string]starlark.Value
}

func newSnapshot(ctx context.Context, files []omnisave.RevisionFile, artifacts ArtifactOpener) *snapshot {
	byPath := make(map[string]omnisave.RevisionFile, len(files))
	sorted := make([]string, 0, len(files))
	for _, file := range files {
		byPath[file.Path] = file
		sorted = append(sorted, file.Path)
	}
	sort.Strings(sorted)
	return &snapshot{
		ctx:       ctx,
		byPath:    byPath,
		sorted:    sorted,
		artifacts: artifacts,
		budget:    maxTotalBytes,
		jsonCache: make(map[string]starlark.Value),
	}
}

var _ starlark.HasAttrs = (*snapshot)(nil)

func (s *snapshot) String() string        { return fmt.Sprintf("<snapshot of %d files>", len(s.sorted)) }
func (s *snapshot) Type() string          { return "snapshot" }
func (s *snapshot) Freeze()               {}
func (s *snapshot) Truth() starlark.Bool  { return starlark.True }
func (s *snapshot) Hash() (uint32, error) { return 0, fmt.Errorf("unhashable type: snapshot") }

var snapshotAttrs = []string{"bytes", "exists", "json", "paths", "size", "text"}

func (s *snapshot) AttrNames() []string { return snapshotAttrs }

func (s *snapshot) Attr(name string) (starlark.Value, error) {
	switch name {
	case "paths":
		return s.method(name, s.pathsMethod), nil
	case "exists":
		return s.method(name, s.existsMethod), nil
	case "size":
		return s.method(name, s.sizeMethod), nil
	case "json":
		return s.method(name, s.jsonMethod), nil
	case "text":
		return s.method(name, s.textMethod), nil
	case "bytes":
		return s.method(name, s.bytesMethod), nil
	}
	return nil, nil
}

type methodImpl func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error)

func (s *snapshot) method(name string, impl methodImpl) *starlark.Builtin {
	return starlark.NewBuiltin(name, impl)
}

// pathsMethod returns every canonical path, sorted, optionally filtered by a
// pattern where "*" matches within one path segment and "**" spans segments:
// paths("**/history/*.run").
func (s *snapshot) pathsMethod(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	pattern := ""
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 0, &pattern); err != nil {
		return nil, err
	}
	matched := make([]starlark.Value, 0, len(s.sorted))
	for _, candidate := range s.sorted {
		if pattern == "" || matchPath(pattern, candidate) {
			matched = append(matched, starlark.String(candidate))
		}
	}
	return starlark.NewList(matched), nil
}

func (s *snapshot) existsMethod(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &name); err != nil {
		return nil, err
	}
	_, ok := s.byPath[name]
	return starlark.Bool(ok), nil
}

func (s *snapshot) sizeMethod(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &name); err != nil {
		return nil, err
	}
	file, ok := s.byPath[name]
	if !ok {
		return starlark.None, nil
	}
	return starlark.MakeInt64(file.Artifact.Size), nil
}

func (s *snapshot) textMethod(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &name); err != nil {
		return nil, err
	}
	content, ok := s.read(name)
	if !ok {
		return starlark.None, nil
	}
	return starlark.String(content), nil
}

func (s *snapshot) bytesMethod(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &name); err != nil {
		return nil, err
	}
	content, ok := s.read(name)
	if !ok {
		return starlark.None, nil
	}
	return starlark.Bytes(content), nil
}

// jsonMethod parses and caches JSON, returning None when the file is unreadable or invalid.
func (s *snapshot) jsonMethod(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackPositionalArgs(fn.Name(), args, kwargs, 1, &name); err != nil {
		return nil, err
	}
	if cached, ok := s.jsonCache[name]; ok {
		return cached, nil
	}
	content, ok := s.read(name)
	if !ok {
		return starlark.None, nil
	}
	decode := starlarkjson.Module.Members["decode"]
	decoded, err := starlark.Call(thread, decode, starlark.Tuple{starlark.String(content)}, nil)
	if err != nil {
		decoded = starlark.None
	}
	s.jsonCache[name] = decoded
	return decoded, nil
}

// read opens an artifact within the byte limits and treats all failures as unreadable.
func (s *snapshot) read(name string) (string, bool) {
	file, ok := s.byPath[name]
	if !ok || file.Artifact.Size > maxFileBytes || file.Artifact.Size > s.budget {
		return "", false
	}
	payload, err := s.artifacts.OpenArtifact(s.ctx, file.Artifact.SHA256)
	if err != nil {
		return "", false
	}
	defer payload.Close()
	content, err := io.ReadAll(io.LimitReader(payload, maxFileBytes))
	if err != nil {
		return "", false
	}
	s.budget -= int64(len(content))
	return string(content), true
}

// matchPath reports whether a slash-separated pattern matches a canonical
// path. Within a segment, "*" and "?" match as in path.Match; a "**" segment
// matches any run of segments, including none.
func matchPath(pattern, name string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchSegments(pattern, segments []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			if matchSegments(pattern[1:], segments) {
				return true
			}
			if len(segments) == 0 {
				return false
			}
			segments = segments[1:]
			continue
		}
		if len(segments) == 0 {
			return false
		}
		if ok, err := path.Match(pattern[0], segments[0]); err != nil || !ok {
			return false
		}
		pattern, segments = pattern[1:], segments[1:]
	}
	return len(segments) == 0
}
