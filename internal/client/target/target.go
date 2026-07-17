// Package target defines installed applications, games, and native save data.
package target

import (
	"context"
	"time"
)

// Target is one application installation resolved on this machine.
type Target struct {
	ID       string
	Adapter  string
	Source   string
	Root     string
	Location string
}

// GameIdentity carries adapter evidence for identifying an installed game.
type GameIdentity struct {
	Source      string
	ID          string
	Title       string
	Platform    string
	ContentPath string
	CRC32       string
}

// Environment describes where an installed game runs on this machine.
type Environment struct {
	HostOS     string
	Runtime    string
	Home       string
	StoreRoot  string
	PrefixRoot string
	Variables  map[string]string
}

// InstalledGame is one game exposed through a local target.
type InstalledGame struct {
	ID          string
	TargetID    string
	Identity    GameIdentity
	InstallRoot string
	Environment Environment
	Metadata    map[string]string
}

// File is one native file belonging to a discovered save.
type File struct {
	Path         string
	LocationID   string
	RelativePath string
	Size         int64
	Modified     time.Time
}

// Save is one adapter-defined native save set, which may contain multiple files.
type Save struct {
	ID       string
	TargetID string
	GameID   string
	Kind     string
	Files    []File
	Metadata map[string]string
}

// Adapter discovers application targets, their games, and adapter-native saves.
type Adapter interface {
	Name() string
	DiscoverTargets(context.Context) ([]Target, error)
	DiscoverGames(context.Context, Target) ([]InstalledGame, error)
	DiscoverSaves(context.Context, Target, InstalledGame) ([]Save, error)
}
