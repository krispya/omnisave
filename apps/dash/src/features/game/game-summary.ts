import type {
  GameFingerprint,
  GameIdentifier,
  GameMedia,
  GameProvenance,
  Omnisave,
} from '../../lib/omnisave-api.js';

export type GameSummary = {
  id: string;
  label: string;
  sortKey: string;
  platform?: string;
  platformCompany?: string;
  publisher?: string;
  description?: string;
  labelerAvailable: boolean;
  metadataSource?: string;
  identifiers: GameIdentifier[];
  fingerprints: GameFingerprint[];
  metadata?: Record<string, unknown>;
  refreshedAt?: string;
  media: GameMedia[];
  provenance: GameProvenance[];
  saves: Omnisave[];
  inLibrary: boolean;
};

/** Returns a provider release year from either its numeric or string form. */
export function releaseYear(game: GameSummary) {
  const value = game.metadata?.['release_year'];
  return typeof value === 'string' || typeof value === 'number' ? String(value) : undefined;
}

/** Returns tracked devices currently reported as playing the game. */
export function playingOn(game: GameSummary) {
  return game.provenance.filter((record) => !record.untracked_at && record.playing === true);
}

/** Returns the preferred landscape image, falling back to the portrait cover. */
export function backdrop(game: GameSummary) {
  const byPosition = (left: GameMedia, right: GameMedia) => left.position - right.position;
  const wide = game.media.filter((media) => media.kind === 'artwork').sort(byPosition);
  const shots = game.media.filter((media) => media.kind === 'screenshot').sort(byPosition);
  return wide[0] ?? shots[0] ?? game.media.find((media) => media.kind === 'cover');
}
