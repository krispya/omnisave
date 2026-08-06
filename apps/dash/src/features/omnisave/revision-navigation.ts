import type { Revision } from '../../lib/omnisave-api.js';

export type RevisionMove = 'rewind' | 'fast-forward' | 'jump';

/** Describes a pointer move by ancestry rather than by timestamps. */
export function revisionMove(
  revisions: Revision[],
  currentRevisionID: string | null,
  targetRevisionID: string
): RevisionMove {
  if (!currentRevisionID) return 'jump';
  const byID = new Map(revisions.map((revision) => [revision.id, revision]));

  function isAncestor(candidateID: string, descendantID: string) {
    let revision = byID.get(descendantID);
    const visited = new Set<string>();
    while (revision?.parent_id && !visited.has(revision.id)) {
      if (revision.parent_id === candidateID) return true;
      visited.add(revision.id);
      revision = byID.get(revision.parent_id);
    }
    return false;
  }

  if (isAncestor(targetRevisionID, currentRevisionID)) return 'rewind';
  if (isAncestor(currentRevisionID, targetRevisionID)) return 'fast-forward';
  return 'jump';
}

export function revisionMoveLabel(move: RevisionMove) {
  if (move === 'rewind') return 'Rewind';
  if (move === 'fast-forward') return 'Fast-forward';
  return 'Jump';
}
