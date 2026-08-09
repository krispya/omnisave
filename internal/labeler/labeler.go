// Package labeler derives revision display names from save content.
//
// A labeler is a small Starlark script bound to a game. At commit time it
// receives the revision's file set and returns a name — "Necrobinder A5,
// Underdocks floor 12, 11/66 HP" — or nothing. Scripts are pure functions
// over the snapshot: they cannot reach the filesystem, the network, or the
// clock, and a script that fails, stalls, or answers nonsense costs only the
// name, never the commit.
//
// Built-in labelers are Starlark rather than Go so that user-provided
// labelers can later run on the exact runtime the built-ins have already
// proven, differing only in where the script is stored.
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

// New loads the built-in labeler scripts and returns a Labeler that reads
// game identity from games and file content from artifacts. A built-in that
// fails to load is a programming error and fails construction.
func New(games GameDirectory, artifacts ArtifactOpener) (*Labeler, error) {
	scripts, err := loadBuiltins()
	if err != nil {
		return nil, err
	}
	return &Labeler{games: games, artifacts: artifacts, scripts: scripts}, nil
}

// NameRevision derives a display name for a revision of gameID holding files,
// or "" when the game has no labeler or its script declines or fails. It is
// best-effort by contract: callers commit unnamed rather than propagate an
// error.
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
