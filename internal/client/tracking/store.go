// Package tracking persists which discovered games this machine should sync.
package tracking

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/krisbaumgartner/omnisave/internal/client"
)

// Game is the local identity retained when a discovered game is tracked.
type Game struct {
	ID       string `json:"id"`
	Adapter  string `json:"adapter"`
	TargetID string `json:"target_id"`
	Title    string `json:"title"`
}

// State contains this machine's tracked games.
type State struct {
	Games map[string]Game `json:"games"`
}

// Store persists tracking state in one local JSON file.
type Store struct {
	path string
}

// NewStore creates a tracking store at an explicit path.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// DefaultStore uses the host's conventional per-user configuration directory.
func DefaultStore() (*Store, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return NewStore(filepath.Join(root, "omnisave", "client.json")), nil
}

// Load reads the current selections, returning an empty state on first use.
func (s *Store) Load() (State, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return NewState(), nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read tracking state: %w", err)
	}
	state := NewState()
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("parse tracking state: %w", err)
	}
	if state.Games == nil {
		state.Games = make(map[string]Game)
	}
	return state, nil
}

// Save atomically writes the current selections.
func (s *Store) Save(state State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode tracking state: %w", err)
	}
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return fmt.Errorf("create tracking directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".client-*.json")
	if err != nil {
		return fmt.Errorf("create tracking state: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write tracking state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close tracking state: %w", err)
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("replace tracking state: %w", err)
	}
	return nil
}

// NewState creates an initialized empty tracking state.
func NewState() State {
	return State{Games: make(map[string]Game)}
}

// TrackedIDs returns the identities used to preselect discovered games.
func (s State) TrackedIDs() map[string]bool {
	tracked := make(map[string]bool, len(s.Games))
	for id := range s.Games {
		tracked[id] = true
	}
	return tracked
}

// ApplyVisible updates games shown by the latest scan and preserves unavailable ones.
func (s *State) ApplyVisible(visible []Game, selectedIDs []string) error {
	if s.Games == nil {
		s.Games = make(map[string]Game)
	}
	available := make(map[string]Game, len(visible))
	for _, game := range visible {
		available[game.ID] = game
		delete(s.Games, game.ID)
	}
	for _, id := range selectedIDs {
		game, ok := available[id]
		if !ok {
			return fmt.Errorf("selected game was not discovered: %s", id)
		}
		s.Games[id] = game
	}
	return nil
}

// FromScans returns selectable local identities from scanner results.
func FromScans(scans []client.TargetScan) []Game {
	var games []Game
	for _, scan := range scans {
		for _, discovered := range scan.Games {
			title := discovered.Game.Identity.Title
			if title == "" {
				title = discovered.Game.Identity.Source + " " + discovered.Game.Identity.ID
			}
			games = append(games, Game{
				ID:       discovered.Game.ID,
				Adapter:  scan.Target.Adapter,
				TargetID: scan.Target.ID,
				Title:    title,
			})
		}
	}
	return games
}
