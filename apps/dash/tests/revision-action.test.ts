import { describe, expect, it } from 'vitest';
import type { Omnisave, Revision } from '../src/lib/omnisave-api.js';
import {
  refreshRevisionAction,
  resolveFreshSave,
  type RevisionAction,
} from '../src/features/omnisave/revision-action.js';

function save(id: string, currentRevisionID: string | null): Omnisave {
  return {
    id,
    game_id: 'game-1',
    display_name: id,
    current_revision_id: currentRevisionID,
    current_revision_created_at: '2026-01-01T00:00:00Z',
    created_at: '2026-01-01T00:00:00Z',
  };
}

function revision(id: string, parentID: string | null): Revision {
  return {
    id,
    omnisave_id: 'save-1',
    display_name: '',
    parent_id: parentID,
    created_at: '2026-01-01T00:00:00Z',
    files: [],
  };
}

// root -> a -> b, the shape a rewind-then-retry moves through.
const revisions = [revision('root', null), revision('a', 'root'), revision('b', 'a')];

describe('resolveFreshSave', () => {
  it('prefers the reloaded save with the same id', () => {
    const captured = save('save-1', 'b');
    const reloaded = save('save-1', 'root');
    expect(resolveFreshSave([save('other', null), reloaded], captured)).toBe(reloaded);
  });

  it('falls back to the captured save when it vanished from the library', () => {
    const captured = save('save-1', 'b');
    expect(resolveFreshSave([save('other', null)], captured)).toBe(captured);
  });
});

describe('refreshRevisionAction', () => {
  it('retries a conflicted restore with the reloaded current pointer and verb', () => {
    // Captured before the conflict: current was b, so restoring a was a rewind.
    const action: RevisionAction = {
      kind: 'restore',
      save: save('save-1', 'b'),
      revision: revisions[1] as Revision,
      move: 'rewind',
    };
    // The conflict reload finds the pointer already rewound to root.
    const refreshed = refreshRevisionAction(action, [save('save-1', 'root')], revisions);

    expect(refreshed.save.current_revision_id).toBe('root');
    expect(refreshed.kind === 'restore' && refreshed.move).toBe('fast-forward');
  });

  it('keeps the captured save and move when the save is gone from the library', () => {
    const action: RevisionAction = {
      kind: 'restore',
      save: save('save-1', 'b'),
      revision: revisions[0] as Revision,
      move: 'rewind',
    };
    const refreshed = refreshRevisionAction(action, [], revisions);

    expect(refreshed.save).toBe(action.save);
    expect(refreshed.kind === 'restore' && refreshed.move).toBe('rewind');
  });

  it('swaps a fork action onto the reloaded save and otherwise stays identical', () => {
    const captured = save('save-1', 'a');
    const action: RevisionAction = {
      kind: 'fork',
      save: captured,
      revision: revisions[2] as Revision,
    };

    expect(refreshRevisionAction(action, [captured], revisions)).toBe(action);

    const reloaded = save('save-1', 'b');
    const refreshed = refreshRevisionAction(action, [reloaded], revisions);
    expect(refreshed.save).toBe(reloaded);
    expect(refreshed.revision).toBe(action.revision);
  });
});
