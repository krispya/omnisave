import { Suspense, useCallback, useEffect, useMemo, useState, type FormEvent } from 'react';
import {
  deleteOmniSave,
  listOmniSaves,
  listRevisions,
  HeadConflictError,
  updateOmniSaveDisplayName,
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
import { DeleteGameDialog, DeleteSaveDialog } from '../features/games/delete-dialog.js';
import { FixMatchDialog } from '../features/games/fix-match-dialog.js';
import { clearCatalogCache, primeCatalogGame } from '../features/games/game-catalog-cache.js';
import { GameDetail } from '../features/games/game-detail.js';
import {
  GameDetailSkeleton,
  GameLibrary,
  GameLibrarySkeleton,
  ResolvedGame,
  groupOmniSavesByGame,
  type GameSummary,
} from '../features/games/game-library.js';

const tokenStorageKey = 'omnisave.api-token';

type DeleteTarget =
  | { type: 'game'; game: GameSummary }
  | { type: 'save'; game: GameSummary; save: OmniSave; name: string };

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
  const [debugAction, setDebugAction] = useState<'game' | 'save' | 'revision' | 'fork' | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget>();
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState('');
  const [fixMatchTarget, setFixMatchTarget] = useState<GameSummary>();

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
    clearCatalogCache(activeToken);
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
    clearCatalogCache(token);
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
      const created = await createRandomTestOmniSave(
        token,
        games.map((game) => game.label)
      );
      const catalogPromise = created.catalog.catch((catalogError: unknown) => {
        setError(
          catalogError instanceof Error ? catalogError.message : 'Could not match the test game.'
        );
        return null;
      });
      primeCatalogGame(token, created.save.game_id, catalogPromise);
      setSaves((current) => [...current, created.save]);
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
      await createTestRevision(token, selectedSave.id, selectedSave.head_revision_id);
      await loadSaves(token);
      await loadRevisionHistory(token, selectedSave.id);
    } catch (createError) {
      if (createError instanceof HeadConflictError) {
        await loadSaves(token);
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
      await loadSaves(token);
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

  function requestDeleteGame(game: GameSummary) {
    setDeleteError('');
    setDeleteTarget({ type: 'game', game });
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
        deleteTarget.type === 'game' ? deleteTarget.game.saves : [deleteTarget.save];
      for (const save of savesToDelete) {
        await deleteOmniSave(token, save.id);
      }

      if (deleteTarget.type === 'save' && selectedSaveID === deleteTarget.save.id) {
        const nextSave = deleteTarget.game.saves.find((save) => save.id !== deleteTarget.save.id);
        setSelectedSaveID(nextSave?.id ?? '');
      }
      setDeleteTarget(undefined);
      await loadSaves(token);
    } catch (deleteFailure) {
      setDeleteError(
        deleteFailure instanceof Error
          ? deleteFailure.message
          : `Could not delete this ${deleteTarget.type}.`
      );
    } finally {
      setDeleting(false);
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
              <Suspense fallback={<GameDetailSkeleton />}>
                <ResolvedGame game={selectedGame} token={token}>
                  {(resolvedGame) => (
                    <GameDetail
                      game={resolvedGame}
                      token={token}
                      selectedSave={selectedSave}
                      revisions={revisions}
                      loadingRevisions={loadingRevisions}
                      revisionError={revisionError}
                      onSelectSave={(save) => setSelectedSaveID(save.id)}
                      onRequestDelete={requestDeleteSave}
                      onRenameSave={renameSave}
                    />
                  )}
                </ResolvedGame>
              </Suspense>
            ) : (
              <section className="mt-8" aria-label="Games with saves" aria-busy={loading}>
                {loading && games.length === 0 ? (
                  <GameLibrarySkeleton />
                ) : (
                  <GameLibrary
                    games={games}
                    token={token}
                    onOpenGame={openGame}
                    onRequestFixMatch={setFixMatchTarget}
                    onRequestDelete={requestDeleteGame}
                  />
                )}
              </section>
            )}
          </>
        )}
      </main>
      {deleteTarget?.type === 'game' ? (
        <DeleteGameDialog
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
            primeCatalogGame(token, game.id, Promise.resolve(game));
            setSaves((current) => [...current]);
            setFixMatchTarget(undefined);
          }}
        />
      ) : null}
    </div>
  );
}
