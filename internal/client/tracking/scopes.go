package tracking

import (
	"fmt"

	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// SaveSelection persists a game's native ownership boundary. An empty Adapter
// is the whole save; otherwise every save slot that adapter finds is tracked,
// including slots it finds later. Save slots are the default where offered,
// recorded the first time they apply; the whole save is an explicit choice.
type SaveSelection struct {
	Adapter string `json:"adapter,omitempty"`
}

func (s SaveSelection) validate() error {
	if s.Adapter != "" && !s.scope().Valid() {
		return fmt.Errorf("unsupported save slot adapter")
	}
	return nil
}

func (s SaveSelection) scope() omnisave.SaveScope {
	if s.Adapter == "" {
		return omnisave.SaveScope{}
	}
	return omnisave.SaveScope{Kind: omnisave.ScopeSlot, Adapter: s.Adapter}
}

// SelectSaves changes scope explicitly. Existing histories remain on the server;
// old mappings are retired rather than reinterpreted. Pending restores must finish
// first because their journal is the recovery contract for already placed bytes.
func (s *State) SelectSaves(gameID string, selection SaveSelection) error {
	if _, ok := s.Games[gameID]; !ok {
		return fmt.Errorf("game is not tracked")
	}
	if err := selection.validate(); err != nil {
		return err
	}
	if old, ok := s.SaveSelections[gameID]; ok && old == selection {
		return nil
	}
	for _, p := range s.PendingPlacements {
		if p.Save.GameID == gameID {
			return fmt.Errorf("finish the pending restore first")
		}
	}
	bindings := s.Bindings[:0]
	for _, b := range s.Bindings {
		if b.LocalGameID == gameID && b.Scope != selection.scope() {
			continue
		}
		bindings = append(bindings, b)
	}
	s.Bindings = bindings
	if s.SaveSelections == nil {
		s.SaveSelections = map[string]SaveSelection{}
	}
	s.SaveSelections[gameID] = selection
	return nil
}

// DisplayTitle gives each independent local save its own report and prompt row.
func (s LocalSave) DisplayTitle() string {
	if s.Slot != "" {
		return s.GameTitle + " · " + s.Slot
	}
	return s.GameTitle
}
