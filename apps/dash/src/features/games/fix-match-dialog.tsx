import { useCallback, useEffect, useState, type FormEvent } from 'react';
import {
  fixGameMatch,
  searchGameMatches,
  type CatalogGame,
  type GameMatchCandidate,
} from '../../lib/omnisave-api.js';
import type { GameSummary } from './game-library.js';

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
        setError(searchError instanceof Error ? searchError.message : 'Could not search Hasheous.');
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
        className="flex max-h-[min(48rem,90vh)] w-full max-w-2xl flex-col overflow-hidden rounded-xl border border-white/10 bg-[#181818] shadow-2xl"
      >
        <header className="border-b border-white/5 px-5 py-4">
          <div className="flex items-start justify-between gap-4">
            <div>
              <h2 id="fix-match-title" className="font-medium text-white">
                Fix Match
              </h2>
              <p className="mt-1 text-sm text-slate-500">
                Choose the catalog entry for {game.label}.
              </p>
            </div>
            <button
              type="button"
              onClick={onCancel}
              disabled={Boolean(matchingID)}
              className="rounded px-2 py-1 text-sm text-slate-400 hover:bg-white/5 hover:text-white disabled:opacity-40"
            >
              Close
            </button>
          </div>
          <form onSubmit={submit} className="mt-4 flex gap-2">
            <label className="min-w-0 flex-1 text-xs font-medium text-slate-400">
              Game title
              <input
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                className="mt-1.5 block w-full rounded-md border border-white/10 bg-black/25 px-3 py-2 text-sm text-white outline-none placeholder:text-slate-600 focus:border-[#e5a00d]"
                placeholder="Final Fantasy"
              />
            </label>
            <button
              type="submit"
              disabled={loading || !query.trim() || Boolean(matchingID)}
              className="mt-5 h-9 rounded-md bg-[#e5a00d] px-4 text-sm font-semibold text-black hover:bg-[#f2b51d] disabled:cursor-not-allowed disabled:opacity-40"
            >
              Search
            </button>
          </form>
        </header>

        <div className="overflow-y-auto p-3">
          {error ? (
            <p role="alert" className="m-2 rounded-md bg-red-400/10 px-3 py-2 text-sm text-red-200">
              {error}
            </p>
          ) : null}
          {loading ? (
            <p className="px-3 py-10 text-center text-sm text-slate-500">Searching catalog…</p>
          ) : candidates.length === 0 ? (
            <p className="px-3 py-10 text-center text-sm text-slate-500">No matching games found.</p>
          ) : (
            <ul className="space-y-1">
              {candidates.map((candidate) => {
                return (
                  <li key={`${candidate.provider}:${candidate.provider_id}`}>
                    <button
                      type="button"
                      onClick={() => void select(candidate)}
                      disabled={Boolean(matchingID)}
                      className="w-full rounded-lg px-3 py-3 text-left transition hover:bg-white/5 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      <span className="block min-w-0">
                        <span className="block text-sm font-medium text-white">
                          {candidate.title}
                        </span>
                        {candidate.edition ? (
                          <span className="mt-0.5 block truncate text-xs text-slate-400">
                            {candidate.edition}
                          </span>
                        ) : null}
                        <span className="mt-1 block text-xs text-slate-600">
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
