import { describe, expect, it } from 'vitest';
import { buildRevisionRail } from '../src/features/omnisave/revision-rail.js';
import type { Omnisave, Revision } from '../src/lib/omnisave-api.js';

function save(id: string, current: string | null): Omnisave {
  return {
    id,
    game_id: 'game-1',
    display_name: id,
    current_revision_id: current,
    created_at: '2026-08-01T00:00:00Z',
    current_revision_created_at: '2026-08-01T00:00:00Z',
  } as Omnisave;
}

function revision(id: string, saveID: string, parent: string | null, minute: number): Revision {
  return {
    id,
    omnisave_id: saveID,
    display_name: '',
    parent_id: parent,
    created_at: `2026-08-01T00:${String(minute).padStart(2, '0')}:00Z`,
    files: [],
  } as unknown as Revision;
}

describe('buildRevisionRail', () => {
  it('lays a linear history in one lane with a continuous line', () => {
    const rail = buildRevisionRail(save('s1', 'r3'), [
      revision('r1', 's1', null, 1),
      revision('r2', 's1', 'r1', 2),
      revision('r3', 's1', 'r2', 3),
    ]);

    expect(rail.laneCount).toBe(1);
    expect(rail.rows.map((row) => row.node.revision.id)).toEqual(['r3', 'r2', 'r1']);
    expect(rail.rows.map((row) => row.node.lane)).toEqual([0, 0, 0]);
    expect(rail.rows.map((row) => [row.lineUp, row.lineDown])).toEqual([
      [false, true],
      [true, true],
      [true, false],
    ]);
    expect(rail.rows.every((row) => row.through.length === 0)).toBe(true);
    expect(rail.rows.every((row) => row.curvesIn.length === 0)).toBe(true);
  });

  it('gives a rewind branch its own lane, curving into the shared parent', () => {
    // r1 ← r2 (left behind) and r1 ← r3 (committed after a rewind, newest).
    const rail = buildRevisionRail(save('s1', 'r3'), [
      revision('r1', 's1', null, 1),
      revision('r2', 's1', 'r1', 2),
      revision('r3', 's1', 'r1', 3),
    ]);

    expect(rail.laneCount).toBe(2);
    const [top, middle, bottom] = rail.rows;
    expect(top.node.revision.id).toBe('r3');
    expect(middle.node.revision.id).toBe('r2');
    expect(bottom.node.revision.id).toBe('r1');
    // The newest child continues the trunk; the older sibling branches.
    expect(top.node.lane).toBe(0);
    expect(middle.node.lane).toBe(1);
    expect(bottom.node.lane).toBe(0);
    // Both siblings' lanes run down to r1: lane 0 as a straight line, lane 1
    // over the gap and into a curve at r1's row.
    expect(top.lineDown).toBe(true);
    expect(middle.lineDown).toBe(true);
    expect(bottom.lineUp).toBe(true);
    expect(bottom.curvesIn).toEqual([1]);
  });

  it('routes a branch lane over interleaved trunk rows', () => {
    // Trunk r1←r2←r4 with a branch r2←r3 whose timestamp interleaves under r4.
    const rail = buildRevisionRail(save('s1', 'r4'), [
      revision('r1', 's1', null, 1),
      revision('r2', 's1', 'r1', 2),
      revision('r3', 's1', 'r2', 3),
      revision('r4', 's1', 'r2', 4),
    ]);

    expect(rail.rows.map((row) => row.node.revision.id)).toEqual(['r4', 'r3', 'r2', 'r1']);
    const [, r3Row, r2Row] = rail.rows;
    expect(r3Row.node.lane).toBe(1);
    expect(r2Row.curvesIn).toEqual([1]);
    // The trunk line from r4 to r2 passes over r3's row.
    expect(r3Row.through).toContain(0);
  });

  it('never reports lanes for a single revision', () => {
    const rail = buildRevisionRail(save('s1', 'r1'), [revision('r1', 's1', null, 1)]);
    expect(rail.laneCount).toBe(1);
    expect(rail.rows).toHaveLength(1);
    const [only] = rail.rows;
    expect(only.lineUp).toBe(false);
    expect(only.lineDown).toBe(false);
    expect(only.through).toEqual([]);
  });
});
