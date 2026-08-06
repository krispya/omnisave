import { describe, expect, it } from 'vitest';
import type { Revision } from '../src/lib/omnisave-api.js';
import { revisionMove } from '../src/features/omnisave/revision-navigation.js';

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

describe('revision navigation', () => {
  const root = revision('root', null);
  const oldTip = revision('old-tip', 'root');
  const branch = revision('branch', 'root');
  const revisions = [root, oldTip, branch];

  // root -> a -> b -> c with a side branch root -> x -> x1.
  const chain = [
    root,
    revision('a', 'root'),
    revision('b', 'a'),
    revision('c', 'b'),
    revision('x', 'root'),
    revision('x1', 'x'),
  ];

  it('calls an ancestor move a rewind', () => {
    expect(revisionMove(revisions, oldTip.id, root.id)).toBe('rewind');
  });

  it('calls a descendant move a fast-forward', () => {
    expect(revisionMove(revisions, root.id, branch.id)).toBe('fast-forward');
  });

  it('calls a sibling branch move a jump', () => {
    expect(revisionMove(revisions, oldTip.id, branch.id)).toBe('jump');
  });

  it('follows ancestry across several hops when rewinding', () => {
    expect(revisionMove(chain, 'c', 'root')).toBe('rewind');
    expect(revisionMove(chain, 'c', 'a')).toBe('rewind');
  });

  it('follows ancestry across several hops when fast-forwarding', () => {
    expect(revisionMove(chain, 'root', 'c')).toBe('fast-forward');
    expect(revisionMove(chain, 'a', 'c')).toBe('fast-forward');
  });

  it('calls a move between cousins on different branches a jump', () => {
    expect(revisionMove(chain, 'c', 'x1')).toBe('jump');
    expect(revisionMove(chain, 'x1', 'b')).toBe('jump');
  });

  it('calls any move from an empty current pointer a jump', () => {
    expect(revisionMove(chain, null, 'c')).toBe('jump');
  });

  it('calls a move onto the current revision itself a jump', () => {
    expect(revisionMove(chain, 'b', 'b')).toBe('jump');
  });

  it('calls the move a jump when the current revision is missing from the list', () => {
    // A stale cache can name a current revision the history does not contain;
    // the walk must end cleanly rather than crash or loop.
    expect(revisionMove(chain, 'not-in-history', 'b')).toBe('jump');
    expect(revisionMove(chain, 'not-in-history', 'root')).toBe('jump');
  });
});
