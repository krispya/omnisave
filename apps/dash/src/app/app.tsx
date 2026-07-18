import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react';
import {
  deleteOmniSave,
  listGames,
  listOmniSaves,
  listRevisions,
  HeadConflictError,
  updateOmniSaveDisplayName,
  type CatalogGame,
  type OmniSave,
  type Revision,
} from '../lib/omnisave-api.js';
import { ConnectForm } from '../features/connection/connect-form.js';
import {
  createRandomTestOmniSave,
  createTestRevision,
  createTestSave,
  forkTestSave,
} from '../features/debug/debug-actions.js';
import { DebugMenu } from '../features/debug/debug-menu.js';
import { DeleteGameSavesDialog, DeleteSaveDialog } from '../features/games/delete-dialog.js';
import { FixMatchDialog } from '../features/games/fix-match-dialog.js';
import { GameDetail } from '../features/games/game-detail.js';
import {
  GameLibrary,
  GameLibrarySkeleton,
  buildLibrary,
  type GameSummary,
} from '../features/games/game-library.js';

const tokenStorageKey = 'omnisave.api-token';

type DeleteTarget =
  | { type: 'game-saves'; game: GameSummary }
  | { type: 'save'; game: GameSummary; save: OmniSave; name: string };

function upsertCatalogGame(catalog: CatalogGame[], game: CatalogGame) {
  return catalog.some((candidate) => candidate.id === game.id)
    ? catalog.map((candidate) => (candidate.id === game.id ? game : candidate))
    : [...catalog, game];
}

export function App() {
  const [token, setToken] = useState(() => sessionStorage.getItem(tokenStorageKey) ?? '');
  const [tokenInput, setTokenInput] = useState(token);
  const [catalog, setCatalog] = useState<CatalogGame[] | null>(null);
  const [saves, setSaves] = useState<OmniSave[]>([]);
  const [selectedGameID, setSelectedGameID] = useState('');
  const [selectedSaveID, setSelectedSaveID] = useState('');
  const [revisions, setRevisions] = useState<Revision[]>([]);
  const [error, setError] = useState('');
  const [revisionError, setRevisionError] = useState('');
  const [loading, setLoading] = useState(false);
  const [loadingRevisions, setLoadingRevisions] = useState(false);
  const [debugAction, setDebugAction] = useState<'game' | 'save' | 'revision' | 'fork' | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget>();
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState('');
  const [fixMatchTarget, setFixMatchTarget] = useState<GameSummary>();

  const games = useMemo(() => buildLibrary(catalog, saves), [catalog, saves]);
  const selectedGame = useMemo(
    () => games.find((game) => game.id === selectedGameID),
    [games, selectedGameID]
  );
  const selectedSave = useMemo(
    () => selectedGame?.saves.find((save) => save.id === selectedSaveID),
    [selectedGame, selectedSaveID]
  );

  const loadLibrary = useCallback(async (activeToken: string, signal?: AbortSignal) => {
    if (!activeToken) return;

    setLoading(true);
    setError('');
    try {
      // The catalog endpoints are optional server-side; without them the
      // library falls back to games described by their saves.
      const [nextSaves, nextCatalog] = await Promise.all([
        listOmniSaves(activeToken, signal),
        listGames(activeToken, signal).catch((catalogError: unknown) => {
          if (catalogError instanceof DOMException && catalogError.name === 'AbortError') {
            throw catalogError;
          }
          return null;
        }),
      ]);
      setSaves(nextSaves);
      setCatalog(nextCatalog);
      setSelectedGameID((currentID) =>
        (nextCatalog?.some((game) => game.id === currentID) ?? false) ||
        nextSaves.some((save) => save.game_id === currentID)
          ? currentID
          : ''
      );
      setSelectedSaveID((currentID) =>
        nextSaves.some((save) => save.id === currentID) ? currentID : ''
      );
    } catch (loadError) {
      if (loadError instanceof DOMException && loadError.name === 'AbortError') return;
      setError(loadError instanceof Error ? loadError.message : 'Could not load the library.');
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
    void loadLibrary(token, controller.signal);
    return () => controller.abort();
  }, [loadLibrary, token]);

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
    if (nextToken === token) void loadLibrary(nextToken);
  }

  function disconnect() {
    sessionStorage.removeItem(tokenStorageKey);
    setToken('');
    setTokenInput('');
    setCatalog(null);
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
    await loadLibrary(token);
    if (selectedSaveID) await loadRevisionHistory(token, selectedSaveID);
  }

  async function addRandomGame() {
    if (!token) return;

    setDebugAction('game');
    setError('');
    try {
      const created = await createRandomTestOmniSave(
        token,
        games.map((game) => game.label)
      );
      setSaves((current) => [...current, created.save]);
      setCatalog((current) => (current ? upsertCatalogGame(current, created.game) : current));
      created.catalog
        .then((matched) =>
          setCatalog((current) => (current ? upsertCatalogGame(current, matched) : current))
        )
        .catch((catalogError: unknown) => {
          setError(
            catalogError instanceof Error ? catalogError.message : 'Could not match the test game.'
          );
        });
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
      const created = await createTestSave(token, {
        id: selectedGame.id,
        label: selectedGame.label,
        platform: selectedGame.platform,
      });
      await loadLibrary(token);
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
      await createTestRevision(token, selectedSave.id, selectedSave.head_revision_id);
      await loadLibrary(token);
      await loadRevisionHistory(token, selectedSave.id);
    } catch (createError) {
      if (createError instanceof HeadConflictError) {
        await loadLibrary(token);
        await loadRevisionHistory(token, selectedSave.id);
      }
      setRevisionError(
        createError instanceof HeadConflictError
          ? 'This save changed elsewhere. History was refreshed; fork it to preserve both versions.'
          : createError instanceof Error
            ? createError.message
            : 'Could not add a revision.'
      );
    } finally {
      setDebugAction(null);
    }
  }

  async function forkSave() {
    if (!token || !selectedSave || !selectedSave.head_revision_id) return;

    setDebugAction('fork');
    setRevisionError('');
    try {
      const result = await forkTestSave(
        token,
        selectedSave.id,
        selectedSave.head_revision_id,
        selectedSave.display_name || 'Save'
      );
      await loadLibrary(token);
      setSelectedSaveID(result.omnisave.id);
    } catch (createError) {
      setRevisionError(
        createError instanceof Error ? createError.message : 'Could not fork this save.'
      );
    } finally {
      setDebugAction(null);
    }
  }

  async function renameSave(save: OmniSave, displayName: string) {
    if (!token) return;
    const updated = await updateOmniSaveDisplayName(token, save.id, displayName);
    setSaves((current) =>
      current.map((candidate) => (candidate.id === updated.id ? updated : candidate))
    );
  }

  function requestDeleteGameSaves(game: GameSummary) {
    setDeleteError('');
    setDeleteTarget({ type: 'game-saves', game });
  }

  function requestDeleteSave(save: OmniSave, name: string) {
    if (!selectedGame) return;
    setDeleteError('');
    setDeleteTarget({ type: 'save', game: selectedGame, save, name });
  }

  function cancelDelete() {
    if (deleting) return;
    setDeleteTarget(undefined);
    setDeleteError('');
  }

  async function confirmDelete() {
    if (!token || !deleteTarget) return;

    setDeleting(true);
    setDeleteError('');
    try {
      const savesToDelete =
        deleteTarget.type === 'game-saves' ? deleteTarget.game.saves : [deleteTarget.save];
      for (const save of savesToDelete) {
        await deleteOmniSave(token, save.id);
      }

      if (deleteTarget.type === 'save' && selectedSaveID === deleteTarget.save.id) {
        const nextSave = deleteTarget.game.saves.find((save) => save.id !== deleteTarget.save.id);
        setSelectedSaveID(nextSave?.id ?? '');
      }
      setDeleteTarget(undefined);
      await loadLibrary(token);
    } catch (deleteFailure) {
      setDeleteError(
        deleteFailure instanceof Error
          ? deleteFailure.message
          : deleteTarget.type === 'game-saves'
            ? 'Could not delete these saves.'
            : 'Could not delete this save.'
      );
    } finally {
      setDeleting(false);
    }
  }

  const librarySummary = catalog
    ? `${games.length} ${games.length === 1 ? 'game' : 'games'} in the catalog · ${saves.length} ${
        saves.length === 1 ? 'save' : 'saves'
      }.`
    : `${games.length} ${games.length === 1 ? 'game' : 'games'} with saved progress.`;

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
          <ConnectForm token={tokenInput} onTokenChange={setTokenInput} onConnect={connect} />
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
                    ? selectedGame.saves.length === 0
                      ? 'This game has no saves yet.'
                      : 'Choose a save to inspect its revision history.'
                    : librarySummary}
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
                  revisionHistoryAvailable={!loadingRevisions}
                  canFork={Boolean(selectedSave?.head_revision_id)}
                  onAddRandomGame={() => void addRandomGame()}
                  onAddSave={() => void addSave()}
                  onAddRevision={() => void addRevision()}
                  onForkSave={() => void forkSave()}
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
                token={token}
                selectedSave={selectedSave}
                revisions={revisions}
                loadingRevisions={loadingRevisions}
                revisionError={revisionError}
                onSelectSave={(save) => setSelectedSaveID(save.id)}
                onRequestDelete={requestDeleteSave}
                onRenameSave={renameSave}
              />
            ) : (
              <section className="mt-8" aria-label="Game library" aria-busy={loading}>
                {loading && games.length === 0 ? (
                  <GameLibrarySkeleton />
                ) : (
                  <GameLibrary
                    games={games}
                    token={token}
                    onOpenGame={openGame}
                    onRequestFixMatch={setFixMatchTarget}
                    onRequestDeleteSaves={requestDeleteGameSaves}
                  />
                )}
              </section>
            )}
          </>
        )}
      </main>
      {deleteTarget?.type === 'game-saves' ? (
        <DeleteGameSavesDialog
          game={deleteTarget.game}
          deleting={deleting}
          error={deleteError}
          onCancel={cancelDelete}
          onConfirm={() => void confirmDelete()}
        />
      ) : deleteTarget?.type === 'save' ? (
        <DeleteSaveDialog
          name={deleteTarget.name}
          deleting={deleting}
          error={deleteError}
          onCancel={cancelDelete}
          onConfirm={() => void confirmDelete()}
        />
      ) : null}
      {fixMatchTarget ? (
        <FixMatchDialog
          game={fixMatchTarget}
          token={token}
          onCancel={() => setFixMatchTarget(undefined)}
          onMatched={(game) => {
            setCatalog((current) => (current ? upsertCatalogGame(current, game) : current));
            setFixMatchTarget(undefined);
          }}
        />
      ) : null}
    </div>
  );
}
