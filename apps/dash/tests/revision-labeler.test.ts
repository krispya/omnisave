import { describe, expect, it } from 'vitest';
import { canRunRevisionLabeler } from '../src/features/omnisave/revision-log.js';

describe('revision labeler action', () => {
  it('is offered only when the game has a labeler and automation owns the name', () => {
    expect(canRunRevisionLabeler(true, {})).toBe(true);
    expect(canRunRevisionLabeler(true, { name_source: 'labeler' })).toBe(true);
    expect(canRunRevisionLabeler(false, {})).toBe(false);
    expect(canRunRevisionLabeler(false, { name_source: 'labeler' })).toBe(false);
    expect(canRunRevisionLabeler(true, { name_source: 'manual' })).toBe(false);
  });
});
