import { describe, expect, it } from 'vitest';
import type { CatalogGame, GameProvenance } from '../src/lib/omnisave-api.js';
import { applyPresence } from '../src/features/game/playing-devices.js';

function record(overrides: Partial<GameProvenance>): GameProvenance {
  return {
    device_id: 'device-1',
    device_name: 'Steam Deck',
    installed: true,
    first_tracked_at: '2026-01-01T00:00:00Z',
    last_seen_at: '2026-08-06T11:00:00Z',
    ...overrides,
  };
}

describe('applyPresence', () => {
  function game(id: string, provenance: GameProvenance[]): CatalogGame {
    return {
      id,
      title: 'Chrono Trigger',
      labeler_available: false,
      metadata_source: 'hasheous',
      identifiers: [],
      fingerprints: [],
      media: [],
      provenance,
      refreshed_at: '2026-08-06T00:00:00Z',
    };
  }

  it('lights the records the picture names and nothing else', () => {
    const [chrono, mario] = applyPresence(
      [
        game('game-1', [record({}), record({ device_id: 'device-2', device_name: 'Desktop' })]),
        game('game-2', [record({})]),
      ],
      [{ device_id: 'device-1', playing_game_ids: ['game-1'], reported_at: '2026-08-06T11:59:00Z' }]
    );
    expect(chrono.provenance[0].playing).toBe(true);
    expect(chrono.provenance[0].playing_reported_at).toBe('2026-08-06T11:59:00Z');
    expect(chrono.provenance[1].playing).toBeUndefined();
    expect(mario.provenance[0].playing).toBeUndefined();
  });

  it('clears a session the picture no longer carries', () => {
    const [patched] = applyPresence(
      [game('game-1', [record({ playing: true, playing_reported_at: '2026-08-06T11:58:00Z' })])],
      []
    );
    expect(patched.provenance[0].playing).toBeUndefined();
    expect(patched.provenance[0].playing_reported_at).toBeUndefined();
  });

  it('leaves everything that is not presence alone', () => {
    const [patched] = applyPresence(
      [game('game-1', [record({})])],
      [{ device_id: 'device-1', playing_game_ids: ['game-1'], reported_at: '2026-08-06T11:59:00Z' }]
    );
    expect(patched.provenance[0].device_name).toBe('Steam Deck');
    expect(patched.provenance[0].last_seen_at).toBe('2026-08-06T11:00:00Z');
    expect(patched.title).toBe('Chrono Trigger');
  });
});
