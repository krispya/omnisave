package client

import (
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const debounceDuration = 500 * time.Millisecond

// SyncEvent is sent to the TUI when a sync completes or fails.
type SyncEvent struct {
	System   string
	Game     string
	FilePath string
	Pushed   bool
	Conflict bool
	Err      error
}

// Watcher watches the Retroarch saves directory and triggers syncs.
type Watcher struct {
	cfg    *Config
	state  *State
	syncer *Syncer
	events chan<- SyncEvent

	mu      sync.Mutex
	timers  map[string]*time.Timer
	watcher *fsnotify.Watcher
}

// NewWatcher creates a Watcher. events is the channel SyncEvents are sent to.
func NewWatcher(cfg *Config, state *State, syncer *Syncer, events chan<- SyncEvent) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{
		cfg:     cfg,
		state:   state,
		syncer:  syncer,
		events:  events,
		timers:  make(map[string]*time.Timer),
		watcher: fw,
	}, nil
}

// Start begins watching the saves directory. Blocks until done is closed.
func (w *Watcher) Start(done <-chan struct{}) error {
	if err := w.watcher.Add(w.cfg.RetroarchSavesDir); err != nil {
		return err
	}
	// Also watch subdirectories (systems).
	matches, _ := filepath.Glob(filepath.Join(w.cfg.RetroarchSavesDir, "*"))
	for _, m := range matches {
		w.watcher.Add(m) // ignore errors for non-directories
	}

	go func() {
		for {
			select {
			case <-done:
				w.watcher.Close()
				return
			case event, ok := <-w.watcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					if strings.ToLower(filepath.Ext(event.Name)) == ".srm" {
						w.scheduleSync(event.Name)
					}
				}
			case err, ok := <-w.watcher.Errors:
				if !ok {
					return
				}
				log.Printf("watcher error: %v", err)
			}
		}
	}()
	return nil
}

// scheduleSync debounces sync for the given path (500ms quiet period).
func (w *Watcher) scheduleSync(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if t, ok := w.timers[path]; ok {
		t.Reset(debounceDuration)
		return
	}
	w.timers[path] = time.AfterFunc(debounceDuration, func() {
		w.mu.Lock()
		delete(w.timers, path)
		w.mu.Unlock()
		w.doSync(path)
	})
}

// doSync parses system/game from the path and calls SyncGame if enabled.
func (w *Watcher) doSync(path string) {
	system, game, ok := parseSavePath(w.cfg.RetroarchSavesDir, path)
	if !ok {
		return
	}
	if !w.state.IsEnabled(system, game) {
		return
	}
	pushed, conflict, err := w.syncer.SyncGame(system, game, path)
	w.events <- SyncEvent{
		System:   system,
		Game:     game,
		FilePath: path,
		Pushed:   pushed,
		Conflict: conflict,
		Err:      err,
	}
	if err == nil && pushed {
		w.state.Save()
	}
}

// parseSavePath extracts (system, game) from an .srm path relative to savesDir.
// Retroarch saves as {savesDir}/{system}/{game}.srm.
func parseSavePath(savesDir, path string) (system, game string, ok bool) {
	rel, err := filepath.Rel(savesDir, path)
	if err != nil {
		return "", "", false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 2 {
		return "", "", false
	}
	system = parts[0]
	game = strings.TrimSuffix(parts[1], ".srm")
	if system == "" || game == "" {
		return "", "", false
	}
	return system, game, true
}
