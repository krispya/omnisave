import { useEffect, useState } from 'react';
import { listRevisions, type Omnisave, type Revision } from '../../lib/omnisave-api.js';
import { createPromiseCache } from '../cache/promise-cache.js';

// Keyed by save and head, so committing a revision invalidates the history it replaces.
const histories = createPromiseCache<string, Revision[]>();
// The last history shown for a save, so a moved head revalidates in place rather than
// dropping the log back to a loading state under the reader.
const shown = new Map<string, Revision[]>();
const shownKeys = new Map<string, string>();

export type SaveHistory = {
  revisions: Revision[];
  /** True only when there is nothing to show yet; a silent revalidation is not loading. */
  loading: boolean;
  error: string;
  replaceRevision: (revision: Revision) => void;
};

type Snapshot = {
  key: string;
  revisions?: Revision[];
  error: string;
};

function historyKey(save: Omnisave) {
  return `${save.id}:${save.head_revision_id ?? ''}`;
}

function saveID(key: string) {
  return key.slice(0, key.indexOf(':'));
}

function load(token: string, key: string) {
  return histories.load(key, () => listRevisions(token, saveID(key)));
}

function record(key: string, revisions: Revision[]) {
  const id = saveID(key);
  const previous = shownKeys.get(id);
  // One history per save stays cached; the head it replaced is no longer reachable.
  if (previous && previous !== key) histories.delete(previous);
  shownKeys.set(id, key);
  shown.set(id, revisions);
}

function read(key: string): Snapshot {
  if (!key) return { key, error: '' };
  return { key, revisions: histories.get(key) ?? shown.get(saveID(key)), error: '' };
}

/** Warms the cache so expanding a save can paint its history with no loading step. */
export function prefetchSaveRevisions(token: string, save: Omnisave) {
  const key = historyKey(save);
  if (!token || histories.get(key)) return;
  void load(token, key).then(
    (revisions) => record(key, revisions),
    () => undefined
  );
}

/**
 * The revision history of one save. A history that is already cached is in state before
 * the first paint, so opening a save never shows another save's rows, an empty state, or
 * a loading step on the way to its own.
 */
export function useSaveRevisions(token: string, save?: Omnisave): SaveHistory {
  const key = save ? historyKey(save) : '';
  const [snapshot, setSnapshot] = useState(() => read(key));

  // Adjusting here rather than in an effect re-runs this render before anything paints,
  // so the save that was open a moment ago never flashes inside the one just opened.
  if (snapshot.key !== key) setSnapshot(read(key));

  useEffect(() => {
    if (!token || !key) return;

    let active = true;
    load(token, key).then(
      (revisions) => {
        record(key, revisions);
        if (!active) return;
        setSnapshot((current) =>
          current.key === key && current.revisions === revisions
            ? current
            : { key, revisions, error: '' }
        );
      },
      (loadError: unknown) => {
        if (!active) return;
        setSnapshot({
          key,
          error: loadError instanceof Error ? loadError.message : 'Could not load revisions.',
        });
      }
    );
    return () => {
      active = false;
    };
  }, [key, token]);

  const current = snapshot.key === key ? snapshot : read(key);
  function replaceRevision(revision: Revision) {
    if (!key) return;
    const revisions = current.revisions?.map((candidate) =>
      candidate.id === revision.id ? revision : candidate
    );
    if (!revisions) return;
    histories.delete(key);
    record(key, revisions);
    setSnapshot({ key, revisions, error: '' });
  }
  return {
    revisions: current.revisions ?? [],
    loading: Boolean(key) && current.revisions === undefined && !current.error,
    error: current.error,
    replaceRevision,
  };
}
