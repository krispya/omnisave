import { describe, expect, it } from 'vitest';
import type { GameMedia, GameProvenance } from '../src/lib/omnisave-api.js';
import {
  backdrop,
  playingOn,
  releaseYear,
  type GameSummary,
} from '../src/features/game/game-summary.js';

function game(overrides: Partial<GameSummary> = {}): GameSummary {
  return {
    id: 'game-1',
    label: 'Chrono Trigger',
    sortKey: 'Chrono Trigger',
    identifiers: [],
    fingerprints: [],
    media: [],
    provenance: [],
    saves: [],
    inLibrary: true,
    ...overrides,
  };
}

function media(overrides: Partial<GameMedia>): GameMedia {
  return {
    id: 'media-1',
    kind: 'cover',
    position: 0,
    format: 'image/jpeg',
    size: 1024,
    url: '/api/v1/games/game-1/media/media-1',
    ...overrides,
  };
}

function device(overrides: Partial<GameProvenance> = {}): GameProvenance {
  return {
    device_id: 'device-1',
    device_name: 'Steam Deck',
    installed: true,
    first_tracked_at: '2026-01-01T00:00:00Z',
    last_seen_at: '2026-08-08T11:00:00Z',
    ...overrides,
  };
}

describe('what the library reads off a game', () => {
  it('shows the release year whichever shape the provider sent it in', () => {
    // IGDB sends a string; nothing stops another provider sending a number,
    // and a caption should not turn into "[object Object]" over it.
    expect(releaseYear(game({ metadata: { release_year: '1995' } }))).toBe('1995');
    expect(releaseYear(game({ metadata: { release_year: 1995 } }))).toBe('1995');
    expect(releaseYear(game({ metadata: { tags: ['rpg'] } }))).toBeUndefined();
    expect(releaseYear(game())).toBeUndefined();
  });

  it('prefers key art for a landscape card, then a screenshot, then the cover', () => {
    const cover = media({ id: 'cover', kind: 'cover' });
    const shot = media({ id: 'shot', kind: 'screenshot', position: 0 });
    const art = media({ id: 'art', kind: 'artwork', position: 0 });
    const laterArt = media({ id: 'later-art', kind: 'artwork', position: 1 });

    // Artwork wins even when a screenshot came first in the list, and the
    // provider's own ordering decides which artwork.
    expect(backdrop(game({ media: [cover, shot, laterArt, art] }))?.id).toBe('art');
    expect(backdrop(game({ media: [cover, shot] }))?.id).toBe('shot');
    // The cover is a compromise, not a match — but a cropped cover beats a
    // card with nothing on it, which is what most games would get.
    expect(backdrop(game({ media: [cover] }))?.id).toBe('cover');
    expect(backdrop(game())).toBeUndefined();
  });

  it('counts a device as playing only while the server still speaks for it', () => {
    const playing = device({ playing: true });
    const idle = device({ device_id: 'device-2', device_name: 'Desktop' });
    // An untracked device's last report describes a moment that has passed,
    // so it is not evidence that anything is being played now (ADR-013).
    const abandoned = device({
      device_id: 'device-3',
      device_name: 'Old Laptop',
      playing: true,
      untracked_at: '2026-08-07T00:00:00Z',
    });

    expect(playingOn(game({ provenance: [playing, idle, abandoned] }))).toEqual([playing]);
    expect(playingOn(game({ provenance: [idle, abandoned] }))).toEqual([]);
  });
});
