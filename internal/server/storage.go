package server

import (
	"fmt"
	"os"
	"path/filepath"
)

// Storage manages versioned content-addressed save files on disk.
type Storage struct {
	Root string
}

// NewStorage creates a Storage with the given root directory.
func NewStorage(root string) (*Storage, error) {
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("storage: create root dir: %w", err)
	}
	return &Storage{Root: root}, nil
}

// Store writes data to {root}/{system}/{game}/{sha256}.srm.
// If a file with that sha256 already exists, the write is skipped (deduplication).
// Returns the path to the stored file.
func (s *Storage) Store(system, game string, data []byte, sha256 string) (string, error) {
	dir := filepath.Join(s.Root, system, game)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("storage: create game dir: %w", err)
	}
	path := filepath.Join(dir, sha256+".srm")
	if _, err := os.Stat(path); err == nil {
		// Already exists — deduplicated.
		return path, nil
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("storage: write file: %w", err)
	}
	return path, nil
}

// ActivePath returns the path to the .srm file for system/game/sha256.
func (s *Storage) ActivePath(system, game, sha256 string) string {
	return filepath.Join(s.Root, system, game, sha256+".srm")
}
