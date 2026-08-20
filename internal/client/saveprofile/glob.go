package saveprofile

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// expandGlob matches an absolute pattern against the filesystem like
// filepath.Glob, with the extensions the Ludusavi manifest relies on: a `**`
// segment matches zero or more directories, and matching is case-insensitive
// because community-authored casing is unreliable. Filesystem errors and
// unmatchable pattern segments make a rule find nothing rather than fail. The
// first filesystem error is returned so a trace can distinguish unreadable
// paths from absent ones.
// Symlinked directories are followed for named segments, like Glob, but
// never during `**` recursion, which unbounded traversal could loop.
func expandGlob(pattern string) ([]string, error) {
	segments := splitPattern(pattern)
	anchor := 0
	for anchor < len(segments) && !hasMeta(segments[anchor]) {
		anchor++
	}
	if anchor == len(segments) {
		return []string{pattern}, nil
	}
	root := strings.Join(segments[:anchor], "/")
	if root == "" || strings.HasSuffix(root, ":") {
		root += "/"
	}
	var matches []string
	var firstErr error
	var walk func(directory string, remaining []string)
	walk = func(directory string, remaining []string) {
		head, rest := remaining[0], remaining[1:]
		if head == "**" && len(rest) == 0 {
			matches = append(matches, directory)
			return
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			if !os.IsNotExist(err) && firstErr == nil {
				firstErr = err
			}
			return
		}
		if head == "**" {
			// Zero directories consumed: match the rest right here, then
			// keep `**` alive one level down. splitPattern collapsed runs
			// of `**`, so rest never begins with another one.
			matchEntries(directory, entries, rest, &matches, walk)
			for _, entry := range entries {
				if entry.IsDir() {
					walk(filepath.Join(directory, entry.Name()), remaining)
				}
			}
			return
		}
		matchEntries(directory, entries, remaining, &matches, walk)
	}
	walk(filepath.FromSlash(root), segments[anchor:])
	sort.Strings(matches)
	return matches, firstErr
}

// expandLiteral finds the on-disk spellings of an absolute literal path,
// comparing segments case-insensitively the way expandGlob does, because
// community-authored casing is unreliable for literal rules too. The exact
// spelling wins without any directory scan; only a miss falls back to
// walking segments, and only unfound segments scan their parent. Unlike
// glob segments, literal segments never treat metacharacters as pattern
// syntax: values expanded into a rule stay literal.
func expandLiteral(path string) ([]string, error) {
	if _, err := os.Lstat(path); err == nil {
		return []string{path}, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	segments := strings.Split(filepath.ToSlash(path), "/")
	root := segments[0]
	if root == "" || strings.HasSuffix(root, ":") {
		root += "/"
	}
	directories := []string{filepath.FromSlash(root)}
	var firstErr error
	for _, segment := range segments[1:] {
		if segment == "" {
			continue
		}
		var next []string
		for _, directory := range directories {
			exact := filepath.Join(directory, segment)
			if _, err := os.Lstat(exact); err == nil {
				next = append(next, exact)
				continue
			} else if !os.IsNotExist(err) {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			entries, err := os.ReadDir(directory)
			if err != nil {
				if !os.IsNotExist(err) && firstErr == nil {
					firstErr = err
				}
				continue
			}
			for _, entry := range entries {
				if strings.EqualFold(entry.Name(), segment) {
					next = append(next, filepath.Join(directory, entry.Name()))
				}
			}
		}
		if len(next) == 0 {
			return nil, firstErr
		}
		directories = next
	}
	sort.Strings(directories)
	return directories, firstErr
}

// matchEntries matches one directory's entries against the head segment,
// collecting final matches and descending for the rest.
func matchEntries(directory string, entries []os.DirEntry, remaining []string, matches *[]string, walk func(string, []string)) {
	head, rest := remaining[0], remaining[1:]
	for _, entry := range entries {
		if !matchSegment(head, entry.Name()) {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		if len(rest) == 0 {
			*matches = append(*matches, path)
			continue
		}
		if descendable(path, entry) {
			walk(path, rest)
		}
	}
}

// matchSegment compares case-insensitively and treats a malformed pattern
// segment as matching nothing.
func matchSegment(pattern, name string) bool {
	matched, err := filepath.Match(strings.ToLower(pattern), strings.ToLower(name))
	return err == nil && matched
}

// descendable reports whether a matched entry is a directory to walk into,
// following symlinks the way filepath.Glob does.
func descendable(path string, entry os.DirEntry) bool {
	if entry.IsDir() {
		return true
	}
	if entry.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// splitPattern breaks a pattern into slash-separated segments, collapsing
// runs of `**`: zero-or-more directories twice is still zero-or-more, and
// the collapse keeps the walk from revisiting whole subtrees.
func splitPattern(pattern string) []string {
	segments := strings.Split(filepath.ToSlash(pattern), "/")
	collapsed := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment == "**" && len(collapsed) > 0 && collapsed[len(collapsed)-1] == "**" {
			continue
		}
		collapsed = append(collapsed, segment)
	}
	return collapsed
}
