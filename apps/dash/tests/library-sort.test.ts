import { describe, expect, it } from 'vitest';
import type { GameProvenance, Omnisave } from '../src/lib/omnisave-api.js';
import type { GameSummary } from '../src/features/game/game-summary.js';
import { sortLibrary } from '../src/features/library/library-sort.js';

function game(id: string, overrides: Partial<GameSummary> = {}): GameSummary {
  return {
    id,
    label: id,
    sortKey: id,
    labelerAvailable: false,
    identifiers: [],
    fingerprints: [],
    media: [],
    provenance: [],
    saves: [],
    inLibrary: true,
    ...overrides,
  };
}

function tracked(first_tracked_at: string): GameProvenance {
  return {
    device_id: 'device-1',
    device_name: 'Steam Deck',
    installed: true,
    first_tracked_at,
    last_seen_at: first_tracked_at,
  };
}

function save(overrides: Partial<Omnisave>): Omnisave {
  return {
    id: 'save-1',
    game_id: 'game-1',
    display_name: 'Slot 1',
    current_revision_id: 'rev-1',
    created_at: '2026-01-01T00:00:00Z',
    current_revision_created_at: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

describe('sorting the library grid', () => {
  it('leaves the title order alone', () => {
    const games = [game('Baldur'), game('Celeste'), game('Hades')];
    expect(sortLibrary(games, 'title')).toBe(games);
  });

  it('puts the game with the newest upload first', () => {
    const stale = game('Baldur', {
      saves: [save({ current_revision_created_at: '2026-03-01T00:00:00Z' })],
    });
    const fresh = game('Hades', {
      saves: [
        save({ current_revision_created_at: '2026-02-01T00:00:00Z' }),
        save({ id: 'save-2', current_revision_created_at: '2026-08-01T00:00:00Z' }),
      ],
    });

    expect(sortLibrary([stale, fresh], 'updated').map((entry) => entry.id)).toEqual([
      'Hades',
      'Baldur',
    ]);
  });

  it('counts a fresh fork of an old revision as activity', () => {
    // A fork's current revision keeps its original creation time, so the
    // omnisave's own creation is the moment the library saw something happen.
    const forked = game('Celeste', {
      saves: [
        save({
          created_at: '2026-08-10T00:00:00Z',
          current_revision_created_at: '2026-01-01T00:00:00Z',
        }),
      ],
    });
    const plain = game('Hades', {
      saves: [save({ current_revision_created_at: '2026-05-01T00:00:00Z' })],
    });

    expect(sortLibrary([forked, plain], 'updated').map((entry) => entry.id)).toEqual([
      'Celeste',
      'Hades',
    ]);
  });

  it('keeps games without saves at the end, still in title order', () => {
    const games = [
      game('Baldur'),
      game('Celeste', {
        saves: [save({ current_revision_created_at: '2026-05-01T00:00:00Z' })],
      }),
      game('Hades'),
    ];

    expect(sortLibrary(games, 'updated').map((entry) => entry.id)).toEqual([
      'Celeste',
      'Baldur',
      'Hades',
    ]);
  });

  it('orders by release year, newest first, unknown years last', () => {
    const games = [
      game('Baldur', { metadata: { release_year: '1998' } }),
      game('Celeste', { metadata: { release_year: 2018 } }),
      game('Hades'),
    ];

    expect(sortLibrary(games, 'year').map((entry) => entry.id)).toEqual([
      'Celeste',
      'Baldur',
      'Hades',
    ]);
  });

  it('dates a game as added when the server first saw it, however that happened', () => {
    // Tracked long ago on a device, even one whose saves came later.
    const veteran = game('Baldur', {
      provenance: [tracked('2026-01-01T00:00:00Z')],
      saves: [save({ created_at: '2026-08-01T00:00:00Z' })],
    });
    // Never tracked; its first save is its arrival.
    const uploadOnly = game('Celeste', {
      saves: [save({ created_at: '2026-04-01T00:00:00Z' })],
    });
    const newcomer = game('Hades', { provenance: [tracked('2026-08-15T00:00:00Z')] });

    expect(sortLibrary([veteran, uploadOnly, newcomer], 'added').map((entry) => entry.id)).toEqual([
      'Hades',
      'Celeste',
      'Baldur',
    ]);
  });
});
