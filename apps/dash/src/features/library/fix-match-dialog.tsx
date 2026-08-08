import { useCallback, useEffect, useState, type FormEvent } from 'react';
import {
  fixGameMatch,
  searchGameMatches,
  type CatalogGame,
  type GameMatchCandidate,
} from '../../lib/omnisave-api.js';
import { Button } from '../../components/button.js';
import { fieldClass } from '../../components/field.js';
import type { GameSummary } from '../game/game-summary.js';

type FixMatchDialogProps = {
  game: GameSummary;
  token: string;
  onCancel: () => void;
  onMatched: (game: CatalogGame) => void;
};

export function FixMatchDialog({ game, token, onCancel, onMatched }: FixMatchDialogProps) {
  const [query, setQuery] = useState(game.label);
  const [candidates, setCandidates] = useState<GameMatchCandidate[]>([]);
  const [loading, setLoading] = useState(true);
  const [matchingID, setMatchingID] = useState('');
  const [error, setError] = useState('');

  const search = useCallback(
    async (title: string, signal?: AbortSignal) => {
      setLoading(true);
      setError('');
      try {
        setCandidates(await searchGameMatches(token, game.id, title, undefined, signal));
      } catch (searchError) {
        if (searchError instanceof DOMException && searchError.name === 'AbortError') return;
        setError(
          searchError instanceof Error ? searchError.message : 'Could not search for matches.'
        );
      } finally {
        if (!signal?.aborted) setLoading(false);
      }
    },
    [game.id, token]
  );

  useEffect(() => {
    const controller = new AbortController();
    void search(game.label, controller.signal);
    return () => controller.abort();
  }, [game.label, search]);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (query.trim()) void search(query.trim());
  }

  async function select(candidate: GameMatchCandidate) {
    setMatchingID(candidate.provider_id);
    setError('');
    try {
      onMatched(await fixGameMatch(token, game.id, candidate.selection_token));
    } catch (matchError) {
      setError(matchError instanceof Error ? matchError.message : 'Could not update the match.');
      setMatchingID('');
    }
  }

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/75 p-5" role="presentation">
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="fix-match-title"
        className="flex max-h-[min(48rem,90vh)] w-full max-w-2xl flex-col overflow-hidden rounded-xl border border-outline bg-surface"
      >
        <header className="border-b border-outline px-5 py-4">
          <div className="flex items-start justify-between gap-4">
            <div>
              <h2 id="fix-match-title" className="text-lg font-semibold text-text">
                Fix Match
              </h2>
              <p className="mt-1 text-sm text-muted">Choose the correct match for {game.label}.</p>
            </div>
            <button
              type="button"
              onClick={onCancel}
              disabled={Boolean(matchingID)}
              className="rounded-md px-2.5 py-1.5 text-sm text-muted transition duration-120 hover:bg-text/8 hover:text-text disabled:opacity-40"
            >
              Close
            </button>
          </div>
          <form onSubmit={submit} className="mt-4 flex gap-2">
            <label className="min-w-0 flex-1 text-xs text-muted">
              Game title
              <input
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                className={`${fieldClass} mt-1.5 block w-full`}
                placeholder="Final Fantasy"
              />
            </label>
            <Button
              type="submit"
              variant="filled"
              className="mt-5 self-start"
              disabled={loading || !query.trim() || Boolean(matchingID)}
            >
              Search
            </Button>
          </form>
        </header>

        <div className="overflow-y-auto p-3">
          {error ? (
            <p
              role="alert"
              className="m-2 rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger"
            >
              {error}
            </p>
          ) : null}
          {loading ? (
            <p className="px-3 py-10 text-center text-sm text-muted">Searching…</p>
          ) : candidates.length === 0 ? (
            <p className="px-3 py-10 text-center text-sm text-muted">No matching games found.</p>
          ) : (
            <ul className="space-y-1">
              {candidates.map((candidate) => {
                return (
                  <li key={`${candidate.provider}:${candidate.provider_id}`}>
                    <button
                      type="button"
                      onClick={() => void select(candidate)}
                      disabled={Boolean(matchingID)}
                      className="w-full rounded-md px-3 py-3 text-left transition duration-120 hover:bg-text/8 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      <span className="block min-w-0">
                        <span className="block text-sm font-medium text-text">{candidate.title}</span>
                        {candidate.edition ? (
                          <span className="mt-0.5 block truncate text-xs text-muted">
                            {candidate.edition}
                          </span>
                        ) : null}
                        <span className="mt-1 block text-xs text-muted">
                          {[candidate.platform, candidate.region, candidate.language, candidate.year]
                            .filter(Boolean)
                            .join(' · ')}
                        </span>
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      </section>
    </div>
  );
}
