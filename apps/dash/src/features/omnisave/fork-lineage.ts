import type { Omnisave } from '../../lib/omnisave-api.js';
import { defaultSaveName, displaySaveName } from './save-name.js';

/** A save that a fork edge leads to, named the way the save list names it. */
export type ForkLink = {
  save: Omnisave;
  name: string;
};

/** Fork edges leaving one save's revision history. */
export type ForkLineage = {
  /** Forks started from this save, keyed by the revision each one branched at. */
  forks: Map<string, ForkLink[]>;
};

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

  return { forks };
}
