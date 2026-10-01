package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/krisbaumgartner/omnisave/internal/artifact"
	"github.com/krisbaumgartner/omnisave/internal/catalog"
)

const maxMediaSize = 10 << 20

// cacheableMediaKinds limits stored media to the shapes the Dash renders.
var cacheableMediaKinds = map[string]bool{"cover": true, "artwork": true, "screenshot": true}

// cacheMedia stores a claim's media as the Game's own. It is best-effort: a
// Game without its cover is still the right Game.
func (s *service) cacheMedia(ctx context.Context, gameID string, claim *catalog.Claim) {
	if claim == nil {
		return
	}
	for _, reference := range claim.Media {
		_ = s.storeMedia(ctx, gameID, reference)
	}
}

// storeMedia fetches one image from its provider, verifies it is an image
// within the size limit, and stores it as an artifact of the Game.
func (s *service) storeMedia(ctx context.Context, gameID string, reference catalog.MediaReference) error {
	provider := s.providers[reference.Provider]
	if provider == nil || reference.ProviderID == "" || !cacheableMediaKinds[reference.Kind] {
		return catalog.ErrInvalid
	}
	format, payload, err := provider.OpenMedia(ctx, reference)
	if err != nil {
		return err
	}
	defer payload.Close()
	format = strings.ToLower(strings.TrimSpace(strings.Split(format, ";")[0]))
	if mediaType, _, parseErr := mime.ParseMediaType(format); parseErr != nil || !strings.HasPrefix(mediaType, "image/") {
		return catalog.ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(payload, maxMediaSize+1))
	if err != nil || len(data) == 0 || len(data) > maxMediaSize {
		return catalog.ErrUnavailable
	}
	// The bytes must agree with the provider's claim that they are an image.
	format = http.DetectContentType(data)
	if !strings.HasPrefix(format, "image/") {
		return catalog.ErrUnavailable
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	descriptor := artifact.Artifact{Format: format, SHA256: hash, Size: int64(len(data))}
	if err := s.artifacts.StoreArtifact(ctx, descriptor, bytes.NewReader(data)); err != nil {
		return err
	}
	return s.repository.SaveGameMedia(ctx, catalog.GameMedia{
		ID:          uuid.NewString(),
		GameID:      gameID,
		Kind:        reference.Kind,
		Position:    reference.Position,
		Format:      format,
		SHA256:      hash,
		Size:        int64(len(data)),
		Provider:    provider.Name(),
		ProviderID:  reference.ProviderID,
		Attribution: reference.Attribution,
	})
}
