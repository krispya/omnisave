import type { Omnisave } from '../../lib/omnisave-api.js';
import { defaultSaveName, displaySaveName } from './save-name.js';

/** A save that a fork edge leads to, named the way the save list names it. */
export type ForkLink = {
  save: Omnisave;
  name: string;
};

/** The revision a save was forked from, in the save that owns that revision. */
export type ForkOrigin = {
  revisionID: string;
  /** Absent once the source save has been deleted; the revision id is all that survives. */
  save?: Omnisave;
  name: string;
};

/** Both directions of the fork edges that touch one save's revision history. */
export type ForkLineage = {
  /** Where this save came from; absent for saves that started their own lineage. */
  origin?: ForkOrigin;
  /** Forks started from this save, keyed by the revision each one branched at. */
  forks: Map<string, ForkLink[]>;
};

const unavailableSaveName = 'unavailable save';

/**
 * Reads one save's fork edges out of the saves it shares a game with. Forks record
 * their origin, so the downstream edges are found by scanning the siblings.
 */
export function forkLineage(save: Omnisave, saves: Omnisave[]): ForkLineage {
  const forks = new Map<string, ForkLink[]>();
  for (const [index, candidate] of saves.entries()) {
    const origin = candidate.forked_from;
    if (!origin || origin.omnisave_id !== save.id) continue;
    const link = { save: candidate, name: displaySaveName(candidate, defaultSaveName(index)) };
    forks.set(origin.revision_id, [...(forks.get(origin.revision_id) ?? []), link]);
  }

  const origin = save.forked_from;
  if (!origin) return { forks };

  const index = saves.findIndex((candidate) => candidate.id === origin.omnisave_id);
  const source = index >= 0 ? saves[index] : undefined;
  return {
    origin: {
      revisionID: origin.revision_id,
      save: source,
      name: source ? displaySaveName(source, defaultSaveName(index)) : unavailableSaveName,
    },
    forks,
  };
}
