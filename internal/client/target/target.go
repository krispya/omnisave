// Package target defines installed applications, games, and native save data.
package target

import (
	"context"
	"strings"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
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
	Identifiers  []catalog.GameIdentifier
	Fingerprints []catalog.GameFingerprint
	Title        string
	Platform     string
	ContentPath  string
}

// Identifier returns an external ID in the requested namespace.
func (identity GameIdentity) Identifier(namespace string) (string, bool) {
	for _, identifier := range identity.Identifiers {
		if strings.EqualFold(identifier.Namespace, namespace) {
			return identifier.Value, true
		}
	}
	return "", false
}

// StoreIdentifier returns the first store application identity.
func (identity GameIdentity) StoreIdentifier() (store, value string, ok bool) {
	for _, identifier := range identity.Identifiers {
		if strings.HasSuffix(identifier.Namespace, ".app") {
			return strings.TrimSuffix(identifier.Namespace, ".app"), identifier.Value, true
		}
	}
	return "", "", false
}

// DisplayTitle returns the discovered title or a local fallback.
func (identity GameIdentity) DisplayTitle(fallback string) string {
	if strings.TrimSpace(identity.Title) != "" {
		return identity.Title
	}
	return fallback
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
