package main

import (
	"log"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/krisbaumgartner/omnisave/internal/client"
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("home dir: %v", err)
	}

	cfgPath := filepath.Join(home, ".config", "omnisave", "client.yaml")
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	statePath := filepath.Join(home, ".config", "omnisave", "state.json")

	cfg, err := client.LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if cfg.ServerURL == "" {
		log.Fatal("server_url must be set in client.yaml")
	}
	if cfg.Token == "" {
		log.Fatal("token must be set in client.yaml")
	}
	if cfg.RetroarchSavesDir == "" {
		log.Fatal("retroarch_saves_dir could not be auto-detected; please set it in client.yaml")
	}

	state, err := client.LoadState(statePath)
	if err != nil {
		log.Fatalf("load state: %v", err)
	}

	syncer := client.NewSyncer(cfg, state)

	events := make(chan client.SyncEvent, 32)

	done := make(chan struct{})
	watcher, err := client.NewWatcher(cfg, state, syncer, events)
	if err != nil {
		log.Fatalf("create watcher: %v", err)
	}
	if err := watcher.Start(done); err != nil {
		log.Fatalf("start watcher: %v", err)
	}

	model := client.NewModel(cfg, state, syncer, events)

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatalf("tui: %v", err)
	}

	close(done)
	state.Save()
}
