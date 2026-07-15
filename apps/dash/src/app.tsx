import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react';
import {
  createRandomTestOmniSave,
  createTestRevision,
  createTestSave,
  listOmniSaves,
  listRevisions,
  type OmniSave,
  type Revision,
} from './api.js';
import { DebugMenu } from './debug-menu.js';
import { GameDetail } from './game-detail.js';
import { GameLibrary, groupOmniSavesByGame, type GameSummary } from './game-library.js';

const tokenStorageKey = 'omnisave.api-token';

export function App() {
  const [token, setToken] = useState(() => sessionStorage.getItem(tokenStorageKey) ?? '');
  const [tokenInput, setTokenInput] = useState(token);
  const [saves, setSaves] = useState<OmniSave[]>([]);
  const [selectedGameID, setSelectedGameID] = useState('');
  const [selectedSaveID, setSelectedSaveID] = useState('');
  const [revisions, setRevisions] = useState<Revision[]>([]);
  const [error, setError] = useState('');
  const [revisionError, setRevisionError] = useState('');
  const [loading, setLoading] = useState(false);
  const [loadingRevisions, setLoadingRevisions] = useState(false);
  const [debugAction, setDebugAction] = useState<'game' | 'save' | 'revision' | null>(null);

  const games = useMemo(() => groupOmniSavesByGame(saves), [saves]);
  const selectedGame = useMemo(
    () => games.find((game) => game.id === selectedGameID),
    [games, selectedGameID]
  );
  const selectedSave = useMemo(
    () => selectedGame?.saves.find((save) => save.id === selectedSaveID),
    [selectedGame, selectedSaveID]
  );

  const loadSaves = useCallback(async (activeToken: string, signal?: AbortSignal) => {
    if (!activeToken) return;

    setLoading(true);
    setError('');
    try {
      const nextSaves = await listOmniSaves(activeToken, signal);
      setSaves(nextSaves);
      setSelectedGameID((currentID) =>
        nextSaves.some((save) => save.game_id === currentID) ? currentID : ''
      );
      setSelectedSaveID((currentID) =>
        nextSaves.some((save) => save.id === currentID) ? currentID : ''
      );
    } catch (loadError) {
      if (loadError instanceof DOMException && loadError.name === 'AbortError') return;
      setError(loadError instanceof Error ? loadError.message : 'Could not load OmniSaves.');
    } finally {
      setLoading(false);
    }
  }, []);

  const loadRevisionHistory = useCallback(
    async (activeToken: string, saveID: string, signal?: AbortSignal) => {
      if (!activeToken || !saveID) return;

      setLoadingRevisions(true);
      setRevisionError('');
      try {
        setRevisions(await listRevisions(activeToken, saveID, signal));
      } catch (loadError) {
        if (loadError instanceof DOMException && loadError.name === 'AbortError') return;
        setRevisionError(
          loadError instanceof Error ? loadError.message : 'Could not load revisions.'
        );
      } finally {
        setLoadingRevisions(false);
      }
    },
    []
  );

  useEffect(() => {
    const controller = new AbortController();
    void loadSaves(token, controller.signal);
    return () => controller.abort();
  }, [loadSaves, token]);

  useEffect(() => {
    setRevisions([]);
    setRevisionError('');
    if (!selectedSaveID) return;

    const controller = new AbortController();
    void loadRevisionHistory(token, selectedSaveID, controller.signal);
    return () => controller.abort();
  }, [loadRevisionHistory, selectedSaveID, token]);

  function connect(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const nextToken = tokenInput.trim();
    sessionStorage.setItem(tokenStorageKey, nextToken);
    setToken(nextToken);
    if (nextToken === token) void loadSaves(nextToken);
  }

  function disconnect() {
    sessionStorage.removeItem(tokenStorageKey);
    setToken('');
    setTokenInput('');
    setSaves([]);
    closeGame();
    setError('');
  }

  function openGame(game: GameSummary) {
    setSelectedGameID(game.id);
    setSelectedSaveID(game.saves[0]?.id ?? '');
  }

  function closeGame() {
    setSelectedGameID('');
    setSelectedSaveID('');
    setRevisions([]);
    setRevisionError('');
  }

  async function refresh() {
    await loadSaves(token);
    if (selectedSaveID) await loadRevisionHistory(token, selectedSaveID);
  }

  async function addRandomGame() {
    if (!token) return;

    setDebugAction('game');
    setError('');
    try {
      await createRandomTestOmniSave(
        token,
        games.map((game) => game.label)
      );
      await loadSaves(token);
    } catch (createError) {
      setError(createError instanceof Error ? createError.message : 'Could not create an OmniSave.');
    } finally {
      setDebugAction(null);
    }
  }

  async function addSave() {
    if (!token || !selectedGame) return;

    setDebugAction('save');
    setError('');
    try {
      const created = await createTestSave(
        token,
        {
          id: selectedGame.id,
          label: selectedGame.label,
          platform: selectedGame.platform,
        },
        `slot-${selectedGame.saves.length + 1}`
      );
      await loadSaves(token);
      setSelectedSaveID(created.id);
    } catch (createError) {
      setError(createError instanceof Error ? createError.message : 'Could not create a save.');
    } finally {
      setDebugAction(null);
    }
  }

  async function addRevision() {
    if (!token || !selectedSave) return;

    setDebugAction('revision');
    setRevisionError('');
    try {
      await createTestRevision(token, selectedSave.id, revisions.at(-1)?.id);
      await loadRevisionHistory(token, selectedSave.id);
    } catch (createError) {
      setRevisionError(
        createError instanceof Error ? createError.message : 'Could not add a revision.'
      );
    } finally {
      setDebugAction(null);
    }
  }

  return (
    <div className="min-h-screen bg-[#111111] text-[#e5e5e5]">
      <header className="border-b border-white/5 bg-[#181818]">
        <div className="flex items-center justify-between px-5 py-3 sm:px-8">
          <button type="button" onClick={closeGame} className="flex items-center gap-3 text-left">
            <span className="grid size-8 place-items-center rounded-md bg-[#e5a00d] text-sm font-black text-black">
              O
            </span>
            <span className="text-sm font-semibold text-white">OmniSave</span>
          </button>

          <div className="flex items-center gap-3">
            <span className="hidden items-center gap-2 text-xs text-slate-400 sm:flex">
              <span
                className={`size-1.5 rounded-full ${token ? 'bg-[#e5a00d]' : 'bg-neutral-600'}`}
                aria-hidden="true"
              />
              {token ? 'Connected' : 'Not connected'}
            </span>
            {token ? (
              <button
                type="button"
                onClick={disconnect}
                className="rounded-md px-3 py-2 text-xs font-medium text-neutral-400 transition hover:bg-white/5 hover:text-white"
              >
                Disconnect
              </button>
            ) : null}
          </div>
        </div>
      </header>

      <main className="px-5 py-8 sm:px-8 lg:px-10">
        {!token ? (
          <Connect tokenInput={tokenInput} onTokenChange={setTokenInput} onConnect={connect} />
        ) : (
          <>
            <section className="flex items-end justify-between gap-5">
              <div>
                {selectedGame ? (
                  <button
                    type="button"
                    onClick={closeGame}
                    className="mb-4 text-sm font-medium text-slate-400 transition hover:text-white"
                  >
                    ← All games
                  </button>
                ) : null}
                <h1 className="text-2xl font-medium tracking-tight text-white">
                  {selectedGame ? 'Game saves' : 'Games'}
                </h1>
                <p className="mt-1.5 text-sm text-neutral-500">
                  {selectedGame
                    ? 'Choose a save to inspect its revision history.'
                    : `${games.length} ${games.length === 1 ? 'game' : 'games'} with saved progress.`}
                </p>
              </div>

              <div className="flex shrink-0 gap-3">
                <button
                  type="button"
                  onClick={() => void refresh()}
                  disabled={loading}
                  className="rounded-md bg-white/5 px-3.5 py-2 text-sm font-medium text-neutral-300 transition hover:bg-white/10 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  {loading ? 'Refreshing…' : 'Refresh'}
                </button>
                <DebugMenu
                  game={selectedGame}
                  selectedSave={selectedSave}
                  action={debugAction}
                  revisionHistoryAvailable={!loadingRevisions && !revisionError}
                  onAddRandomGame={() => void addRandomGame()}
                  onAddSave={() => void addSave()}
                  onAddRevision={() => void addRevision()}
                />
              </div>
            </section>

            {error ? (
              <div
                role="alert"
                className="mt-5 rounded-md border border-red-400/20 bg-red-400/10 px-4 py-3 text-sm text-red-200"
              >
                {error}
              </div>
            ) : null}

            {selectedGame ? (
              <GameDetail
                game={selectedGame}
                selectedSave={selectedSave}
                revisions={revisions}
                loadingRevisions={loadingRevisions}
                revisionError={revisionError}
                onSelectSave={(save) => setSelectedSaveID(save.id)}
              />
            ) : (
              <section className="mt-8" aria-label="Games with saves" aria-busy={loading}>
                {loading && games.length === 0 ? (
                  <GameLibrarySkeleton />
                ) : (
                  <GameLibrary games={games} onOpenGame={openGame} />
                )}
              </section>
            )}
          </>
        )}
      </main>
    </div>
  );
}

function Connect({
  tokenInput,
  onTokenChange,
  onConnect,
}: {
  tokenInput: string;
  onTokenChange: (token: string) => void;
  onConnect: (event: FormEvent<HTMLFormElement>) => void;
}) {
  return (
    <section className="max-w-lg rounded-lg border border-white/5 bg-[#1a1a1a] p-6">
      <h1 className="font-semibold text-white">Connect to the server</h1>
      <p className="mt-2 text-sm leading-6 text-slate-400">
        Enter the bearer token from your server configuration. It is kept only for this browser
        session.
      </p>
      <form onSubmit={onConnect} className="mt-5 flex flex-col gap-3 sm:flex-row">
        <label className="sr-only" htmlFor="api-token">
          API token
        </label>
        <input
          id="api-token"
          type="password"
          value={tokenInput}
          onChange={(event) => onTokenChange(event.target.value)}
          autoComplete="current-password"
          placeholder="API token"
          className="min-w-0 flex-1 rounded-md border border-white/10 bg-[#111111] px-3.5 py-2.5 text-sm text-white outline-none placeholder:text-neutral-600 focus:border-[#e5a00d]"
        />
        <button
          type="submit"
          disabled={!tokenInput.trim()}
          className="rounded-md bg-[#e5a00d] px-5 py-2.5 text-sm font-semibold text-black transition hover:bg-[#f2b51d] disabled:cursor-not-allowed disabled:opacity-40"
        >
          Connect
        </button>
      </form>
    </section>
  );
}

function GameLibrarySkeleton() {
  return (
    <div className="grid grid-cols-2 gap-5 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 xl:grid-cols-8">
      {Array.from({ length: 8 }, (_, index) => (
        <div key={index} className="aspect-[3/4] animate-pulse rounded-md bg-white/5" />
      ))}
    </div>
  );
}
