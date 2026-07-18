// Package tracking persists local game selections and save mappings.
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

// LocalSave identifies one adapter-native save discovered on this machine.
type LocalSave struct {
	ID        string
	Adapter   string
	TargetID  string
	GameID    string
	GameTitle string
	Kind      string
	FileCount int
	Size      int64
}

// Binding maps one local save to one independently versioned Omnisave.
type Binding struct {
	Adapter              string  `json:"adapter"`
	TargetID             string  `json:"target_id"`
	LocalSaveID          string  `json:"local_save_id"`
	LocalGameID          string  `json:"local_game_id"`
	OmnisaveID           string  `json:"omnisave_id"`
	LastSyncedRevisionID *string `json:"last_synced_revision_id"`
}

// State contains this machine's tracked games and save bindings.
type State struct {
	Games    map[string]Game `json:"games"`
	Bindings []Binding       `json:"bindings"`
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
	if state.Bindings == nil {
		state.Bindings = []Binding{}
	}
	if err := state.validateBindings(); err != nil {
		return State{}, fmt.Errorf("parse tracking state: %w", err)
	}
	return state, nil
}

// Save atomically writes the current selections.
func (s *Store) Save(state State) error {
	if err := state.validateBindings(); err != nil {
		return fmt.Errorf("encode tracking state: %w", err)
	}
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
	return State{Games: make(map[string]Game), Bindings: []Binding{}}
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
	selected := make(map[string]bool, len(selectedIDs))
	for _, id := range selectedIDs {
		selected[id] = true
	}
	bindings := s.Bindings[:0]
	for _, binding := range s.Bindings {
		_, wasVisible := available[binding.LocalGameID]
		if !wasVisible || selected[binding.LocalGameID] {
			bindings = append(bindings, binding)
		}
	}
	s.Bindings = bindings
	return nil
}

// Bind maps a discovered local save to an Omnisave and clears any old sync baseline.
func (s *State) Bind(local LocalSave, omnisaveID string) error {
	if local.ID == "" || local.Adapter == "" || local.TargetID == "" || local.GameID == "" || omnisaveID == "" {
		return fmt.Errorf("binding identities must not be empty")
	}
	binding := Binding{
		Adapter:     local.Adapter,
		TargetID:    local.TargetID,
		LocalSaveID: local.ID,
		LocalGameID: local.GameID,
		OmnisaveID:  omnisaveID,
	}
	for index := range s.Bindings {
		if sameLocalSave(s.Bindings[index], binding) {
			s.Bindings[index] = binding
			return nil
		}
	}
	s.Bindings = append(s.Bindings, binding)
	return nil
}

// BindingFor returns the active mapping for a discovered local save.
func (s State) BindingFor(local LocalSave) (Binding, bool) {
	probe := Binding{Adapter: local.Adapter, TargetID: local.TargetID, LocalSaveID: local.ID}
	for _, binding := range s.Bindings {
		if sameLocalSave(binding, probe) {
			return binding, true
		}
	}
	return Binding{}, false
}

// RecordSynced advances the baseline only while the local save has the expected binding.
func (s *State) RecordSynced(local LocalSave, omnisaveID, revisionID string) error {
	if omnisaveID == "" || revisionID == "" {
		return fmt.Errorf("sync identities must not be empty")
	}
	probe := Binding{Adapter: local.Adapter, TargetID: local.TargetID, LocalSaveID: local.ID}
	for index := range s.Bindings {
		if sameLocalSave(s.Bindings[index], probe) {
			if s.Bindings[index].OmnisaveID != omnisaveID {
				return fmt.Errorf("local save binding changed")
			}
			s.Bindings[index].LastSyncedRevisionID = &revisionID
			return nil
		}
	}
	return fmt.Errorf("local save is not bound")
}

func (s State) validateBindings() error {
	seen := make(map[string]bool, len(s.Bindings))
	for _, binding := range s.Bindings {
		if binding.Adapter == "" || binding.TargetID == "" || binding.LocalSaveID == "" ||
			binding.LocalGameID == "" || binding.OmnisaveID == "" {
			return fmt.Errorf("binding identities must not be empty")
		}
		if binding.LastSyncedRevisionID != nil && *binding.LastSyncedRevisionID == "" {
			return fmt.Errorf("last synced revision must not be empty")
		}
		key := localSaveKey(binding)
		if seen[key] {
			return fmt.Errorf("duplicate binding for local save")
		}
		seen[key] = true
	}
	return nil
}

func sameLocalSave(left, right Binding) bool {
	return left.Adapter == right.Adapter && left.TargetID == right.TargetID && left.LocalSaveID == right.LocalSaveID
}

func localSaveKey(binding Binding) string {
	return fmt.Sprintf("%d:%s%d:%s%d:%s", len(binding.Adapter), binding.Adapter, len(binding.TargetID), binding.TargetID, len(binding.LocalSaveID), binding.LocalSaveID)
}

// FromScans returns selectable local identities from scanner results.
func FromScans(scans []client.TargetScan) []Game {
	var games []Game
	for _, scan := range scans {
		for _, discovered := range scan.Games {
			title := discovered.Game.Identity.DisplayTitle(discovered.Game.ID)
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

// SavesFromScans returns exact local saves from scanner results.
func SavesFromScans(scans []client.TargetScan) []LocalSave {
	var saves []LocalSave
	for _, scan := range scans {
		for _, discovered := range scan.Games {
			title := discovered.Game.Identity.DisplayTitle(discovered.Game.ID)
			for _, save := range discovered.Saves {
				local := LocalSave{
					ID:        save.ID,
					Adapter:   scan.Target.Adapter,
					TargetID:  scan.Target.ID,
					GameID:    discovered.Game.ID,
					GameTitle: title,
					Kind:      save.Kind,
					FileCount: len(save.Files),
				}
				for _, file := range save.Files {
					local.Size += file.Size
				}
				saves = append(saves, local)
			}
		}
	}
	return saves
}
