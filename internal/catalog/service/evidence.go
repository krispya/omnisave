package service

import (
	"cmp"
	"context"
	"encoding/hex"
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/krisbaumgartner/omnisave/internal/catalog"
)

// find returns the one Game that holds any of the evidence, nil when none
// does, and *IdentityConflict when it points at more than one.
func (s *service) find(ctx context.Context, evidence catalog.Evidence) (*catalog.Game, error) {
	found := make(map[string]*catalog.Game)
	for _, identifier := range evidence.Identifiers {
		game, err := s.repository.FindGameByIdentifier(ctx, identifier)
		if err != nil && !errors.Is(err, catalog.ErrNotFound) {
			return nil, err
		}
		if game != nil {
			found[game.ID] = game
		}
	}
	for _, fingerprint := range evidence.Fingerprints {
		game, err := s.repository.FindGameByFingerprint(ctx, fingerprint)
		if err != nil && !errors.Is(err, catalog.ErrNotFound) {
			return nil, err
		}
		if game != nil {
			found[game.ID] = game
		}
	}
	if len(found) > 1 {
		return nil, &catalog.IdentityConflict{GameIDs: slices.Sorted(maps.Keys(found))}
	}
	for _, game := range found {
		return game, nil
	}
	return nil, nil
}

// mergeEvidence adds everything a Game already holds to evidence.
func mergeEvidence(evidence catalog.Evidence, game *catalog.Game) catalog.Evidence {
	evidence.Identifiers = append(slices.Clone(evidence.Identifiers), game.Identifiers...)
	evidence.Fingerprints = append(slices.Clone(evidence.Fingerprints), game.Fingerprints...)
	merged, _ := normalizeEvidence(evidence)
	return merged
}

// normalizeEvidence trims, validates, de-duplicates, and orders evidence, so
// the same evidence always compares and stores the same way. False when any
// part is malformed or there is no identifier or fingerprint at all.
func normalizeEvidence(input catalog.Evidence) (catalog.Evidence, bool) {
	input.TitleHint = strings.TrimSpace(input.TitleHint)
	input.PlatformHint = strings.TrimSpace(input.PlatformHint)
	if len(input.TitleHint) > 200 || len(input.PlatformHint) > 100 {
		return catalog.Evidence{}, false
	}
	identifiers, valid := normalizeAll(input.Identifiers, normalizeIdentifier)
	if !valid {
		return catalog.Evidence{}, false
	}
	fingerprints, valid := normalizeAll(input.Fingerprints, normalizeFingerprint)
	if !valid || len(identifiers) == 0 && len(fingerprints) == 0 {
		return catalog.Evidence{}, false
	}
	slices.SortFunc(identifiers, func(left, right catalog.GameIdentifier) int {
		return cmp.Or(cmp.Compare(left.Namespace, right.Namespace), cmp.Compare(left.Value, right.Value))
	})
	slices.SortFunc(fingerprints, func(left, right catalog.GameFingerprint) int {
		return cmp.Or(cmp.Compare(left.Platform, right.Platform),
			cmp.Compare(left.Algorithm, right.Algorithm), cmp.Compare(left.Value, right.Value))
	})
	input.Identifiers = identifiers
	input.Fingerprints = fingerprints
	return input, true
}

// normalizeAll normalizes each item and drops duplicates; false when any is
// malformed.
func normalizeAll[T comparable](items []T, normalize func(T) (T, bool)) ([]T, bool) {
	result := make([]T, 0, len(items))
	seen := make(map[T]bool, len(items))
	for _, item := range items {
		normalized, valid := normalize(item)
		if !valid {
			return nil, false
		}
		if !seen[normalized] {
			seen[normalized] = true
			result = append(result, normalized)
		}
	}
	return result, true
}

// normalizeIdentifier lowercases the namespace, which must be a dotted
// lowercase name, and rejects control characters in the value.
func normalizeIdentifier(input catalog.GameIdentifier) (catalog.GameIdentifier, bool) {
	input.Namespace = strings.ToLower(strings.TrimSpace(input.Namespace))
	input.Value = strings.TrimSpace(input.Value)
	if input.Namespace == "" || len(input.Namespace) > 64 || input.Value == "" || len(input.Value) > 512 {
		return catalog.GameIdentifier{}, false
	}
	for index, character := range input.Namespace {
		valid := character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
		if index > 0 && (character == '.' || character == '_' || character == '-') {
			valid = true
		}
		if !valid {
			return catalog.GameIdentifier{}, false
		}
	}
	for _, character := range input.Value {
		if character < 0x20 || character == 0x7f {
			return catalog.GameIdentifier{}, false
		}
	}
	return input, true
}

// fingerprintSizes are the hash algorithms a fingerprint may use, by hex length.
var fingerprintSizes = map[string]int{"crc32": 8, "md5": 32, "sha1": 40, "sha256": 64}

// normalizeFingerprint lowercases a fingerprint and requires a known
// algorithm with a hex value of its length.
func normalizeFingerprint(input catalog.GameFingerprint) (catalog.GameFingerprint, bool) {
	input.Platform = strings.ToLower(strings.TrimSpace(input.Platform))
	input.Algorithm = strings.ToLower(strings.TrimSpace(input.Algorithm))
	input.Value = strings.ToLower(strings.TrimSpace(input.Value))
	size, known := fingerprintSizes[input.Algorithm]
	if input.Platform == "" || len(input.Platform) > 100 || !known || len(input.Value) != size {
		return catalog.GameFingerprint{}, false
	}
	_, err := hex.DecodeString(input.Value)
	return input, err == nil
}
