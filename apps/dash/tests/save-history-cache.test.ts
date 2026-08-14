import { describe, expect, it } from 'vitest';
import { saveHistoryKey } from '../src/features/omnisave/use-save-revisions.js';
import type { Omnisave } from '../src/lib/omnisave-api.js';

function save(): Omnisave {
  return {
    id: 'save-1',
    game_id: 'game-1',
    display_name: 'First run',
    current_revision_id: 'revision-1',
    created_at: '2026-08-13T12:00:00Z',
    current_revision_created_at: '2026-08-13T12:00:00Z',
  };
}

describe('saveHistoryKey', () => {
  it('revalidates history for a refreshed Library snapshot without losing same-snapshot prefetches', () => {
    const firstSnapshot = save();
    const refreshedSnapshot = { ...firstSnapshot };

    expect(saveHistoryKey(firstSnapshot)).toBe(saveHistoryKey(firstSnapshot));
    expect(saveHistoryKey(refreshedSnapshot)).not.toBe(saveHistoryKey(firstSnapshot));
  });
});
