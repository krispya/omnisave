// Package tracking persists local game selections and save mappings.
package tracking

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/target"
)

// Game is the local identity retained when a discovered game is tracked.
// ServerGameID is the Library identity resolved at track time; it stays empty
// while the server is unreachable and is filled in on a later track run.
type Game struct {
	ID           string `json:"id"`
	Adapter      string `json:"adapter"`
	TargetID     string `json:"target_id"`
	Title        string `json:"title"`
	ServerGameID string `json:"server_game_id,omitempty"`
	// TrackedAs and TrackedAt are what this Device last told the server about
	// tracking this game, and when. Telling it the same thing again changes
	// nothing, so a pass repeats the claim only when it has changed or gone
	// stale — see TrackingIsCurrent.
	TrackedAs string     `json:"tracked_as,omitempty"`
	TrackedAt *time.Time `json:"tracked_at,omitempty"`
}

// trackingRefresh is how long a Device may go without restating what it
// tracks. Nothing on the server forgets, so this is only a floor under how
// long an unnoticed loss — a restored backup, a record deleted by hand —
// can leave the server believing this Device stopped protecting a game.
const trackingRefresh = time.Hour

// TrackingIsCurrent reports whether the server already knows this exactly,
// recently enough that repeating it would tell it nothing.
func (s *State) TrackingIsCurrent(id, claim string, now time.Time) bool {
	game, known := s.Games[id]
	if !known || game.ServerGameID == "" || game.TrackedAs == "" || game.TrackedAs != claim {
		return false
	}
	return game.TrackedAt != nil && now.Sub(*game.TrackedAt) < trackingRefresh
}

// RecordTracking remembers a claim the server accepted.
func (s *State) RecordTracking(id, claim string, now time.Time) {
	if game, known := s.Games[id]; known {
		game.TrackedAs = claim
		stamped := now.UTC()
		game.TrackedAt = &stamped
		s.Games[id] = game
	}
}

// Device is this installation's self-minted identity (see FDR-002).
type Device struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Server is the persisted connection created by `omnisave connect`.
type Server struct {
	URL   string `json:"url,omitempty"`
	Token string `json:"token,omitempty"`
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
	Adapter              string     `json:"adapter"`
	TargetID             string     `json:"target_id"`
	LocalSaveID          string     `json:"local_save_id"`
	LocalGameID          string     `json:"local_game_id"`
	OmnisaveID           string     `json:"omnisave_id"`
	LastSyncedRevisionID *string    `json:"last_synced_revision_id"`
	LastSyncedAt         *time.Time `json:"last_synced_at,omitempty"`
	// LocalSignature summarizes the local save's files as they stood when a
	// pass last proved them equal to LastSyncedRevisionID. It is a hint that
	// lets a later pass skip reading a save nothing has touched; empty means
	// nothing is claimed, which is what every binding starts and ends at.
	LocalSignature string `json:"local_signature,omitempty"`
	// Achievements is what this Device has already accounted for of the
	// game's unlocked achievements. Its absence means no pass has looked yet.
	Achievements *AchievementWatch `json:"achievements,omitempty"`
}

// AchievementWatch is a binding's place in its game's unlock history. Through
// is the newest unlock time this Device has accounted for, and IDs records
// every achievement accounted for at that exact second. The IDs are needed
// because stores publish whole-second times and several unlocks may tie.
//
// The first look only records where the history already stood, because
// Omnisave can honestly mark a revision only for an unlock it was there for —
// everything earned before it started watching belongs to revisions that
// were never committed.
type AchievementWatch struct {
	Through time.Time `json:"through"`
	IDs     []string  `json:"ids,omitempty"`
}

// State contains this machine's tracked games and save bindings.
type State struct {
	Device   Device          `json:"device"`
	Server   Server          `json:"server"`
	Games    map[string]Game `json:"games"`
	Bindings []Binding       `json:"bindings"`
}

// EnsureDevice mints the device identity on first use and defaults its name.
func (s *State) EnsureDevice(defaultName string) Device {
	if s.Device.ID == "" {
		s.Device.ID = uuid.NewString()
	}
	if s.Device.Name == "" {
		s.Device.Name = defaultName
	}
	return s.Device
}

// SetServerGameID records the Library identity resolved for a tracked game.
func (s *State) SetServerGameID(localID, serverID string) {
	if game, ok := s.Games[localID]; ok {
		game.ServerGameID = serverID
		s.Games[localID] = game
	}
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
	// The state carries sync baselines already committed on the server; a
	// power loss that truncates it would make every binding look diverged,
	// so the bytes are flushed before the rename makes them the state.
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("flush tracking state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close tracking state: %w", err)
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("replace tracking state: %w", err)
	}
	syncDirectory(directory)
	return nil
}

// syncDirectory makes a rename in a directory durable. Best-effort: directory
// fsync is not supported everywhere (notably Windows), and the rename is
// atomic with or without it.
func syncDirectory(path string) {
	if directory, err := os.Open(path); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
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

// ApplyVisible updates games shown by the latest scan and preserves unavailable
// ones. Games kept selected retain their resolved Library identity. It returns
// the games this application untracked, so callers can report them.
func (s *State) ApplyVisible(visible []Game, selectedIDs []string) ([]Game, error) {
	if s.Games == nil {
		s.Games = make(map[string]Game)
	}
	previous := make(map[string]Game, len(s.Games))
	for id, game := range s.Games {
		previous[id] = game
	}
	available := make(map[string]Game, len(visible))
	for _, game := range visible {
		available[game.ID] = game
		delete(s.Games, game.ID)
	}
	for _, id := range selectedIDs {
		game, ok := available[id]
		if !ok {
			return nil, fmt.Errorf("selected game was not discovered: %s", id)
		}
		game.ServerGameID = previous[id].ServerGameID
		s.Games[id] = game
	}
	selected := make(map[string]bool, len(selectedIDs))
	for _, id := range selectedIDs {
		selected[id] = true
	}
	var removed []Game
	for id, game := range previous {
		if _, wasVisible := available[id]; wasVisible && !selected[id] {
			removed = append(removed, game)
		}
	}
	slices.SortFunc(removed, func(left, right Game) int { return strings.Compare(left.ID, right.ID) })
	bindings := s.Bindings[:0]
	for _, binding := range s.Bindings {
		_, wasVisible := available[binding.LocalGameID]
		if !wasVisible || selected[binding.LocalGameID] {
			bindings = append(bindings, binding)
		}
	}
	s.Bindings = bindings
	return removed, nil
}

// Untrack removes a game from this device's selections along with the
// bindings of its local saves. It reports whether the game was tracked.
func (s *State) Untrack(gameID string) bool {
	if _, tracked := s.Games[gameID]; !tracked {
		return false
	}
	delete(s.Games, gameID)
	bindings := s.Bindings[:0]
	for _, binding := range s.Bindings {
		if binding.LocalGameID != gameID {
			bindings = append(bindings, binding)
		}
	}
	s.Bindings = bindings
	return true
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

// Unbind removes any mapping for a discovered local save.
func (s *State) Unbind(local LocalSave) bool {
	probe := Binding{Adapter: local.Adapter, TargetID: local.TargetID, LocalSaveID: local.ID}
	for index := range s.Bindings {
		if sameLocalSave(s.Bindings[index], probe) {
			s.Bindings = slices.Delete(s.Bindings, index, index+1)
			return true
		}
	}
	return false
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
			now := time.Now().UTC()
			s.Bindings[index].LastSyncedRevisionID = &revisionID
			s.Bindings[index].LastSyncedAt = &now
			// A sync just moved what is on disk, or what current means, and
			// the old summary described neither. Dropping it costs the next
			// pass one read of this save and keeps a stale summary from ever
			// standing in for one.
			s.Bindings[index].LocalSignature = ""
			return nil
		}
	}
	return fmt.Errorf("local save is not bound")
}

// RecordVerified remembers how the local save's files stood when a pass
// proved them equal to the revision the binding is synced to. It reports
// nothing: the summary is an optimization a later pass may use, so a
// binding that has moved on since simply keeps none and that pass reads the
// save exactly as it always did.
func (s *State) RecordVerified(local LocalSave, signature string) {
	probe := Binding{Adapter: local.Adapter, TargetID: local.TargetID, LocalSaveID: local.ID}
	for index := range s.Bindings {
		if sameLocalSave(s.Bindings[index], probe) {
			if s.Bindings[index].LastSyncedRevisionID == nil {
				// Nothing was proved equal to anything, so there is nothing
				// this summary could later stand for.
				return
			}
			s.Bindings[index].LocalSignature = signature
			return
		}
	}
}

// AchievementsSeen reports how far this binding has already accounted for the
// game's unlock history, and whether any pass has looked at all.
func (s State) AchievementsSeen(local LocalSave) (AchievementWatch, bool) {
	bound, isBound := s.BindingFor(local)
	if !isBound || bound.Achievements == nil {
		return AchievementWatch{}, false
	}
	watch := *bound.Achievements
	watch.Through = time.Unix(watch.Through.Unix(), 0).UTC()
	watch.IDs = slices.Clone(watch.IDs)
	return watch, true
}

// RecordAchievementsSeen advances how far a binding has accounted for its
// game's unlock history. IDs are the achievements accepted at through; when
// another pass finds more at that same second they are merged into the
// boundary. A report that failed never calls this, so the next pass retries.
func (s *State) RecordAchievementsSeen(local LocalSave, through time.Time, ids []string) {
	through = time.Unix(through.Unix(), 0).UTC()
	ids = append([]string(nil), ids...)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	probe := Binding{Adapter: local.Adapter, TargetID: local.TargetID, LocalSaveID: local.ID}
	for index := range s.Bindings {
		if !sameLocalSave(s.Bindings[index], probe) {
			continue
		}
		watch := s.Bindings[index].Achievements
		if watch != nil {
			watchedThrough := time.Unix(watch.Through.Unix(), 0).UTC()
			if through.Before(watchedThrough) {
				return
			}
			if through.Equal(watchedThrough) {
				ids = append(ids, watch.IDs...)
				slices.Sort(ids)
				ids = slices.Compact(ids)
			}
		}
		s.Bindings[index].Achievements = &AchievementWatch{Through: through, IDs: ids}
		return
	}
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
			for _, save := range discovered.Saves {
				saves = append(saves, LocalSaveFrom(scan, discovered, save))
			}
		}
	}
	return saves
}

// LocalSaveFrom describes one discovered native save as a bindable identity.
func LocalSaveFrom(scan client.TargetScan, discovered client.GameScan, save target.Save) LocalSave {
	local := LocalSave{
		ID:        save.ID,
		Adapter:   scan.Target.Adapter,
		TargetID:  scan.Target.ID,
		GameID:    discovered.Game.ID,
		GameTitle: discovered.Game.Identity.DisplayTitle(discovered.Game.ID),
		Kind:      save.Kind,
		FileCount: len(save.Files),
	}
	for _, file := range save.Files {
		local.Size += file.Size
	}
	return local
}
