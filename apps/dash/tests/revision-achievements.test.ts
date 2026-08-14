import { describe, expect, it } from 'vitest';
import { achievementsByRevision } from '../src/features/omnisave/revision-achievements.js';
import type { Achievement } from '../src/lib/omnisave-api.js';

function achievement(id: string, minute: number, revision: string | null): Achievement {
  return {
    id,
    name: id,
    unlocked_at: `2026-03-07T19:${String(minute).padStart(2, '0')}:00Z`,
    revision_id: revision,
  };
}

describe('achievementsByRevision', () => {
  it('groups each revision’s marks in unlock order', () => {
    const marks = achievementsByRevision([
      achievement('VG_DAY76', 20, 'r2'),
      achievement('VG_DAY1', 10, 'r1'),
      achievement('VG_BEE', 15, 'r2'),
    ]);

    expect([...marks.keys()]).toEqual(['r1', 'r2']);
    expect(marks.get('r2')?.map((mark) => mark.id)).toEqual(['VG_BEE', 'VG_DAY76']);
  });

  it('leaves out unlocks that no revision carries yet', () => {
    const marks = achievementsByRevision([
      achievement('VG_DAY1', 10, 'r1'),
      achievement('VG_DAY2', 30, null),
    ]);

    expect([...marks.keys()]).toEqual(['r1']);
  });
});
