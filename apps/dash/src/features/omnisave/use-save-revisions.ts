import { useEffect, useState } from 'react';
import {
  listAchievements,
  listRevisions,
  type Achievement,
  type Omnisave,
  type Revision,
} from '../../lib/omnisave-api.js';
import { createPromiseCache } from '../cache/promise-cache.js';

/**
 * A save's history as the log shows it. Achievements travel with the
 * revisions because a mark is only readable next to the row it sits on, and
 * both change on exactly the same event: a commit.
 */
type History = {
  revisions: Revision[];
  achievements: Achievement[];
};

// Keyed by save and current revision, so commits and restores revalidate history.
const histories = createPromiseCache<string, History>();
// Retains the last history while a moved current pointer revalidates.
const shown = new Map<string, History>();
const shownKeys = new Map<string, string>();

export type SaveHistory = {
  revisions: Revision[];
  achievements: Achievement[];
  /** True only when there is nothing to show yet; a silent revalidation is not loading. */
  loading: boolean;
  error: string;
  replaceRevision: (revision: Revision) => void;
  removeRevision: (revisionID: string) => void;
};

type Snapshot = {
  key: string;
  history?: History;
  error: string;
};

function historyKey(save: Omnisave) {
  return `${save.id}:${save.current_revision_id ?? ''}`;
}

function saveID(key: string) {
  return key.slice(0, key.indexOf(':'));
}

async function load(token: string, key: string): Promise<History> {
  return histories.load(key, async () => {
    const id = saveID(key);
    const [revisions, achievements] = await Promise.all([
      listRevisions(token, id),
      listAchievements(token, id),
    ]);
    return { revisions, achievements };
  });
}

function record(key: string, history: History) {
  const id = saveID(key);
  const previous = shownKeys.get(id);
  // One history per save stays cached while another save is open.
  if (previous && previous !== key) histories.delete(previous);
  shownKeys.set(id, key);
  shown.set(id, history);
}

function read(key: string): Snapshot {
  if (!key) return { key, error: '' };
  return { key, history: histories.get(key) ?? shown.get(saveID(key)), error: '' };
}

// Shared revisions must be renamed in every cached sibling history.
function replaceInCaches(revision: Revision) {
  for (const [id, key] of shownKeys) {
    const history = histories.get(key) ?? shown.get(id);
    if (!history?.revisions.some((candidate) => candidate.id === revision.id)) continue;
    histories.delete(key);
    shown.set(id, {
      ...history,
      revisions: history.revisions.map((candidate) =>
        candidate.id === revision.id ? revision : candidate
      ),
    });
  }
}

// Remove deleted shared revisions from every cached sibling history.
function dropFromCaches(revisionID: string) {
  for (const [id, key] of shownKeys) {
    const history = histories.get(key) ?? shown.get(id);
    if (!history?.revisions.some((candidate) => candidate.id === revisionID)) continue;
    histories.delete(key);
    shown.set(id, {
      ...history,
      revisions: history.revisions.filter((candidate) => candidate.id !== revisionID),
      // The server moves a deleted node's marks back to waiting; the log drops
      // them until the next commit claims them, matching what a reload shows.
      achievements: history.achievements.map((achievement) =>
        achievement.revision_id === revisionID ? { ...achievement, revision_id: null } : achievement
      ),
    });
  }
}

/** Warms the cache so expanding a save can paint its history with no loading step. */
export function prefetchSaveRevisions(token: string, save: Omnisave) {
  const key = historyKey(save);
  if (!token || histories.get(key)) return;
  void load(token, key).then(
    (history) => record(key, history),
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
      (history) => {
        record(key, history);
        if (!active) return;
        setSnapshot((current) =>
          current.key === key && current.history === history ? current : { key, history, error: '' }
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
  function apply(history: History | undefined) {
    if (!history) return;
    histories.delete(key);
    record(key, history);
    setSnapshot({ key, history, error: '' });
  }
  function replaceRevision(revision: Revision) {
    if (!key) return;
    replaceInCaches(revision);
    apply(
      current.history && {
        ...current.history,
        revisions: current.history.revisions.map((candidate) =>
          candidate.id === revision.id ? revision : candidate
        ),
      }
    );
  }
  function removeRevision(revisionID: string) {
    if (!key) return;
    dropFromCaches(revisionID);
    apply(
      current.history && {
        ...current.history,
        revisions: current.history.revisions.filter((candidate) => candidate.id !== revisionID),
        achievements: current.history.achievements.map((achievement) =>
          achievement.revision_id === revisionID ? { ...achievement, revision_id: null } : achievement
        ),
      }
    );
  }
  return {
    revisions: current.history?.revisions ?? [],
    achievements: current.history?.achievements ?? [],
    loading: Boolean(key) && current.history === undefined && !current.error,
    error: current.error,
    replaceRevision,
    removeRevision,
  };
}
