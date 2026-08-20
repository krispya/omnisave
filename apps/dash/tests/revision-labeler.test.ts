import { describe, expect, it } from 'vitest';
import { canRunRevisionLabeler } from '../src/features/omnisave/revision-log.js';

describe('revision labeler action', () => {
  it('is offered for every revision when the game has a labeler', () => {
    expect(canRunRevisionLabeler(true)).toBe(true);
    expect(canRunRevisionLabeler(false)).toBe(false);
  });
});
