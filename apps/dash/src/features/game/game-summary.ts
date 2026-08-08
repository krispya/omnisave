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

/**
 * The year the game came out, when a provider supplied one. Providers are not
 * consistent about whether it arrives as a number or a string, so both are
 * accepted and neither is trusted to be present.
 */
export function releaseYear(game: GameSummary) {
  const value = game.metadata?.['release_year'];
  return typeof value === 'string' || typeof value === 'number' ? String(value) : undefined;
}

/**
 * The devices playing this game right now.
 *
 * A device that has been untracked is excluded whatever its last report said:
 * the server stops speaking for a device it no longer tracks, so its final
 * playing flag describes a moment that has passed rather than the present
 * (ADR-013).
 */
export function playingOn(game: GameSummary) {
  return game.provenance.filter((record) => !record.untracked_at && record.playing === true);
}

/**
 * The widest image a game has, for a card drawn in landscape.
 *
 * Artwork is the key art a publisher composed to be seen this way, so it is
 * preferred; a screenshot is the same shape and the next best thing, and which
 * of the two exists depends on which provider matched the game. The
 * lowest-positioned one is that provider's own first choice.
 *
 * The cover is the last resort and a real compromise: cropping portrait box
 * art to landscape throws away most of it. It is still better than an empty
 * card, and it is what most games have.
 */
export function backdrop(game: GameSummary) {
  const byPosition = (left: GameMedia, right: GameMedia) => left.position - right.position;
  const wide = game.media.filter((media) => media.kind === 'artwork').sort(byPosition);
  const shots = game.media.filter((media) => media.kind === 'screenshot').sort(byPosition);
  return wide[0] ?? shots[0] ?? game.media.find((media) => media.kind === 'cover');
}
