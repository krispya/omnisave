import { useEffect, useState } from 'react';
import { listRevisions, type Omnisave, type Revision } from '../../lib/omnisave-api.js';
import { createPromiseCache } from '../cache/promise-cache.js';

// Keyed by save and current revision, so commits and restores revalidate history.
const histories = createPromiseCache<string, Revision[]>();
// Retains the last history while a moved current pointer revalidates.
const shown = new Map<string, Revision[]>();
const shownKeys = new Map<string, string>();

export type SaveHistory = {
  revisions: Revision[];
  /** True only when there is nothing to show yet; a silent revalidation is not loading. */
  loading: boolean;
  error: string;
  replaceRevision: (revision: Revision) => void;
  removeRevision: (revisionID: string) => void;
};

type Snapshot = {
  key: string;
  revisions?: Revision[];
  error: string;
};

function historyKey(save: Omnisave) {
  return `${save.id}:${save.current_revision_id ?? ''}`;
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
  // One history per save stays cached while another save is open.
  if (previous && previous !== key) histories.delete(previous);
  shownKeys.set(id, key);
  shown.set(id, revisions);
}

function read(key: string): Snapshot {
  if (!key) return { key, error: '' };
  return { key, revisions: histories.get(key) ?? shown.get(saveID(key)), error: '' };
}

// Shared revisions must be renamed in every cached sibling history.
function replaceInCaches(revision: Revision) {
  for (const [id, key] of shownKeys) {
    const revisions = histories.get(key) ?? shown.get(id);
    if (!revisions?.some((candidate) => candidate.id === revision.id)) continue;
    histories.delete(key);
    shown.set(
      id,
      revisions.map((candidate) => (candidate.id === revision.id ? revision : candidate))
    );
  }
}

// Remove deleted shared revisions from every cached sibling history.
function dropFromCaches(revisionID: string) {
  for (const [id, key] of shownKeys) {
    const revisions = histories.get(key) ?? shown.get(id);
    if (!revisions?.some((candidate) => candidate.id === revisionID)) continue;
    histories.delete(key);
    shown.set(
      id,
      revisions.filter((candidate) => candidate.id !== revisionID)
    );
  }
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

/** Loads one save's revision history and initializes synchronously from cache. */
export function useSaveRevisions(token: string, save?: Omnisave): SaveHistory {
  const key = save ? historyKey(save) : '';
  const [snapshot, setSnapshot] = useState(() => read(key));

  // Switch cached histories before paint when the selected save changes.
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
    replaceInCaches(revision);
    const revisions = current.revisions?.map((candidate) =>
      candidate.id === revision.id ? revision : candidate
    );
    if (!revisions) return;
    histories.delete(key);
    record(key, revisions);
    setSnapshot({ key, revisions, error: '' });
  }
  function removeRevision(revisionID: string) {
    if (!key) return;
    dropFromCaches(revisionID);
    const revisions = current.revisions?.filter((candidate) => candidate.id !== revisionID);
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
    removeRevision,
  };
}
