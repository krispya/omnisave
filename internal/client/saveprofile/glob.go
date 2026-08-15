package saveprofile

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// expandGlob matches an absolute pattern against the filesystem like
// filepath.Glob, with one extension the Ludusavi manifest relies on: a `**`
// segment matches zero or more directories. Like Glob, it ignores filesystem
// errors while walking. Symlinked directories are never descended into.
func expandGlob(pattern string) ([]string, error) {
	segments := strings.Split(filepath.ToSlash(pattern), "/")
	anchor := 0
	for anchor < len(segments) && !strings.ContainsAny(segments[anchor], "*?[") {
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
	var walk func(directory string, remaining []string) error
	walk = func(directory string, remaining []string) error {
		if len(remaining) == 0 {
			matches = append(matches, directory)
			return nil
		}
		head, rest := remaining[0], remaining[1:]
		if head == "**" {
			if len(rest) == 0 {
				matches = append(matches, directory)
				return nil
			}
			if err := walk(directory, rest); err != nil {
				return err
			}
			entries, err := os.ReadDir(directory)
			if err != nil {
				return nil
			}
			for _, entry := range entries {
				if entry.IsDir() {
					if err := walk(filepath.Join(directory, entry.Name()), remaining); err != nil {
						return err
					}
				}
			}
			return nil
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return nil
		}
		for _, entry := range entries {
			matched, err := filepath.Match(head, entry.Name())
			if err != nil {
				return err
			}
			if !matched {
				continue
			}
			path := filepath.Join(directory, entry.Name())
			if len(rest) == 0 {
				matches = append(matches, path)
				continue
			}
			if entry.IsDir() {
				if err := walk(path, rest); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(filepath.FromSlash(root), segments[anchor:]); err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}
