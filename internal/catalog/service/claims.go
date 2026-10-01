package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
)

// claim asks each resolution provider in turn what the evidence is. Each one
// sees the evidence and hints the claims before it added, so a hash catalog's
// igdb.game identifier lets a title catalog answer. A provider with nothing
// to add is skipped; a claim that repeats none of the evidence it was asked
// about is refused. skip leaves out the provider whose claim is being
// enriched. The result is nil when no provider knows the evidence.
func (s *service) claim(ctx context.Context, evidence catalog.Evidence, skip string) (*catalog.Claim, error) {
	var combined *catalog.Claim
	for _, provider := range s.resolvers {
		if provider.Name() == skip {
			continue
		}
		claim, err := provider.Resolve(ctx, evidence)
		if errors.Is(err, catalog.ErrNotFound) || errors.Is(err, catalog.ErrUnavailable) {
			continue
		}
		if err != nil {
			return nil, err
		}
		claimed, valid := claimedEvidence(claim)
		if !valid || !corroborates(claimed, evidence) {
			return nil, catalog.ErrUnavailable
		}
		stampMedia(claim, provider.Name())
		evidence.TitleHint, evidence.PlatformHint = claim.Title, claim.Platform
		if evidence, err = withClaimed(evidence, claim); err != nil {
			return nil, err
		}
		combined = mergeClaims(combined, claim)
	}
	return combined, nil
}

// withClaimed adds a claim's identifiers and fingerprints to evidence.
func withClaimed(evidence catalog.Evidence, claim *catalog.Claim) (catalog.Evidence, error) {
	claimed, valid := claimedEvidence(claim)
	if !valid {
		return evidence, catalog.ErrUnavailable
	}
	evidence.Identifiers = append(slices.Clone(evidence.Identifiers), claimed.Identifiers...)
	evidence.Fingerprints = append(slices.Clone(evidence.Fingerprints), claimed.Fingerprints...)
	if evidence, valid = normalizeEvidence(evidence); !valid {
		return evidence, catalog.ErrUnavailable
	}
	return evidence, nil
}

// claimedEvidence is the evidence a claim makes, with its title and platform
// as hints. False when the claim lacks a source or title, or its evidence is
// malformed.
func claimedEvidence(claim *catalog.Claim) (catalog.Evidence, bool) {
	if claim == nil || strings.TrimSpace(claim.Source) == "" || strings.TrimSpace(claim.Title) == "" {
		return catalog.Evidence{}, false
	}
	return normalizeEvidence(catalog.Evidence{
		Identifiers:  slices.Clone(claim.Identifiers),
		Fingerprints: slices.Clone(claim.Fingerprints),
		TitleHint:    claim.Title,
		PlatformHint: claim.Platform,
	})
}

// corroborates reports whether a claim repeats any identifier or fingerprint
// it was asked about, which is what ties the claim to this game.
func corroborates(claimed, requested catalog.Evidence) bool {
	for _, identifier := range requested.Identifiers {
		if slices.Contains(claimed.Identifiers, identifier) {
			return true
		}
	}
	for _, fingerprint := range requested.Fingerprints {
		if slices.ContainsFunc(claimed.Fingerprints, func(candidate catalog.GameFingerprint) bool {
			return fingerprint.Algorithm == candidate.Algorithm && fingerprint.Value == candidate.Value
		}) {
			return true
		}
	}
	return false
}

// mergeClaims lays next over base. Every claim's evidence and media are kept;
// where both describe the game, the later provider's description wins.
func mergeClaims(base, next *catalog.Claim) *catalog.Claim {
	if base == nil {
		return next
	}
	if next == nil {
		return base
	}
	base.Identifiers = append(base.Identifiers, next.Identifiers...)
	base.Fingerprints = append(base.Fingerprints, next.Fingerprints...)
	base.Media = append(base.Media, next.Media...)
	override := func(field *string, value string) {
		if value != "" {
			*field = value
		}
	}
	override(&base.Source, next.Source)
	override(&base.Title, next.Title)
	override(&base.SortTitle, next.SortTitle)
	override(&base.Publisher, next.Publisher)
	override(&base.Description, next.Description)
	// A platform and its company describe one thing, so they move together.
	if next.Platform != "" {
		base.Platform, base.PlatformCompany = next.Platform, next.PlatformCompany
	}
	if base.Metadata == nil {
		base.Metadata = make(map[string]any)
	}
	maps.Copy(base.Metadata, next.Metadata)
	return base
}

// stampMedia names the provider on its claim's media, which is who stored
// media is fetched from.
func stampMedia(claim *catalog.Claim, provider string) {
	if claim == nil {
		return
	}
	for index := range claim.Media {
		claim.Media[index].Provider = provider
	}
}

// A selection token wraps a provider's own token with the provider's name,
// so Match knows whom to ask.
type providerSelection struct {
	Provider string `json:"provider"`
	Token    string `json:"token"`
}

func encodeSelection(provider, token string) (string, error) {
	payload, err := json.Marshal(providerSelection{Provider: provider, Token: token})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// selection unwraps a selection token into its provider and the provider's
// own token; ErrInvalid for anything this service did not issue.
func (s *service) selection(encoded string) (catalog.Provider, string, error) {
	if encoded == "" || len(encoded) > 8192 {
		return nil, "", catalog.ErrInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, "", catalog.ErrInvalid
	}
	var selection providerSelection
	if err := json.Unmarshal(payload, &selection); err != nil {
		return nil, "", catalog.ErrInvalid
	}
	name := strings.TrimSpace(selection.Provider)
	token := strings.TrimSpace(selection.Token)
	provider := s.providers[name]
	if provider == nil || token == "" || len(token) > 4096 {
		return nil, "", catalog.ErrInvalid
	}
	return provider, token, nil
}
