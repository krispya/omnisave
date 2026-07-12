package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// GameState tracks sync state for a single game.
type GameState struct {
	LastSHA256  string `json:"last_sha256"`
	SyncEnabled bool   `json:"sync_enabled"`
}

// State manages per-game sync state persisted as JSON.
type State struct {
	mu    sync.Mutex
	path  string
	Games map[string]*GameState `json:"games"`
}

// LoadState reads state from path, creating empty state if absent.
func LoadState(path string) (*State, error) {
	s := &State{
		path:  path,
		Games: make(map[string]*GameState),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("read state: %w", err)
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	if s.Games == nil {
		s.Games = make(map[string]*GameState)
	}
	return s, nil
}

// Save persists state to disk.
func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

func (s *State) key(system, game string) string {
	return system + "/" + game
}

func (s *State) getOrCreate(system, game string) *GameState {
	k := s.key(system, game)
	if gs, ok := s.Games[k]; ok {
		return gs
	}
	gs := &GameState{SyncEnabled: true}
	s.Games[k] = gs
	return gs
}

// SetEnabled sets whether sync is enabled for a game.
func (s *State) SetEnabled(system, game string, enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getOrCreate(system, game).SyncEnabled = enabled
}

// IsEnabled returns whether sync is enabled for a game (default: true).
func (s *State) IsEnabled(system, game string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	gs, ok := s.Games[s.key(system, game)]
	if !ok {
		return true
	}
	return gs.SyncEnabled
}

// UpdateHash records the last synced SHA256 for a game.
func (s *State) UpdateHash(system, game, sha256 string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getOrCreate(system, game).LastSHA256 = sha256
}

// LastHash returns the last synced SHA256 for a game (empty string if unknown).
func (s *State) LastHash(system, game string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	gs, ok := s.Games[s.key(system, game)]
	if !ok {
		return ""
	}
	return gs.LastSHA256
}
