// Package labeler derives revision display names with sandboxed Starlark scripts.
package labeler

import (
	"context"
	"io"
	"log"
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
	"github.com/krisbaumgartner/omnisave/internal/omnisave"
)

// maxNameLength mirrors the service's display-name cap so a long answer is
// truncated into a usable name rather than rejected whole.
const maxNameLength = 100

// GameDirectory answers which game a revision belongs to; the server's
// repository is one.
type GameDirectory interface {
	GetGame(ctx context.Context, id string) (*catalog.Game, error)
}

// ArtifactOpener opens a revision file's content by hash; the server's
// repository is one.
type ArtifactOpener interface {
	OpenArtifact(ctx context.Context, sha256 string) (io.ReadCloser, error)
}

// Labeler names revisions by running the game's labeler script, when one
// exists, against the revision's file set.
type Labeler struct {
	games     GameDirectory
	artifacts ArtifactOpener
	scripts   map[string]*script
}

// New loads built-in labelers backed by game metadata and stored artifacts.
func New(games GameDirectory, artifacts ArtifactOpener) (*Labeler, error) {
	scripts, err := loadBuiltins()
	if err != nil {
		return nil, err
	}
	return &Labeler{games: games, artifacts: artifacts, scripts: scripts}, nil
}

// HasLabeler reports whether the game currently matches a registered script.
func (l *Labeler) HasLabeler(ctx context.Context, gameID string) bool {
	if l == nil {
		return false
	}
	game, err := l.games.GetGame(ctx, gameID)
	return err == nil && l.scriptFor(game.Identifiers) != nil
}

// NameRevision derives a display name, returning "" when labeling is unavailable or fails.
func (l *Labeler) NameRevision(ctx context.Context, gameID string, files []omnisave.RevisionFile) string {
	if l == nil {
		return ""
	}
	game, err := l.games.GetGame(ctx, gameID)
	if err != nil {
		log.Printf("labeler: game %s: %v", gameID, err)
		return ""
	}
	found := l.scriptFor(game.Identifiers)
	if found == nil {
		return ""
	}
	name, err := found.run(ctx, newSnapshot(ctx, files, l.artifacts))
	if err != nil {
		log.Printf("labeler: %s on game %s: %v", found.name, gameID, err)
		return ""
	}
	return cleanName(name)
}

// scriptFor returns the first script registered under any of the game's
// identifiers, so one game known by several namespaces still labels.
func (l *Labeler) scriptFor(identifiers []catalog.GameIdentifier) *script {
	for _, identifier := range identifiers {
		if found := l.scripts[scriptKey(identifier.Namespace, identifier.Value)]; found != nil {
			return found
		}
	}
	return nil
}

// scriptKey is the registry form of one game identifier: "namespace:value",
// case-folded because identifier namespaces compare case-insensitively.
func scriptKey(namespace, value string) string {
	return strings.ToLower(namespace) + ":" + strings.ToLower(value)
}

// cleanName flattens a script's answer into one displayable line and bounds
// its length.
func cleanName(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	runes := []rune(name)
	if len(runes) > maxNameLength {
		return strings.TrimSpace(string(runes[:maxNameLength]))
	}
	return name
}
