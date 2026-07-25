import { useEffect, useState } from 'react';
import { listRevisions, type Omnisave, type Revision } from '../../lib/omnisave-api.js';

export type GameRevisions = {
  /** Revisions per save id, oldest first, as the API returns them. */
  revisions: Map<string, Revision[]>;
  loading: boolean;
  error: string;
};

const empty: GameRevisions = { revisions: new Map(), loading: false, error: '' };

/**
 * Loads every revision of a game's saves, which is what the whole-game graph needs and
 * the single-save panel does not. Only runs while `enabled`, so the cost follows the
 * view the user is actually looking at, and reloads when a save appears or its head moves.
 */
export function useGameRevisions(token: string, saves: Omnisave[], enabled: boolean) {
  const [state, setState] = useState<GameRevisions>(empty);
  // Save ids travel in the key so the effect needs nothing from the render that scheduled it.
  const key = enabled ? saves.map((save) => `${save.id}:${save.head_revision_id ?? ''}`).join() : '';

  useEffect(() => {
    if (!token || !key) return;

    const ids = key.split(',').map((entry) => entry.slice(0, entry.indexOf(':')));
    const controller = new AbortController();
    setState({ revisions: new Map(), loading: true, error: '' });

    Promise.all(
      ids.map(async (id) => [id, await listRevisions(token, id, controller.signal)] as const)
    )
      .then((entries) => setState({ revisions: new Map(entries), loading: false, error: '' }))
      .catch((loadError: unknown) => {
        if (loadError instanceof DOMException && loadError.name === 'AbortError') return;
        setState({
          revisions: new Map(),
          loading: false,
          error: loadError instanceof Error ? loadError.message : 'Could not load revisions.',
        });
      });

    return () => controller.abort();
  }, [key, token]);

  return enabled ? state : empty;
}
