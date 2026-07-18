import {
  Suspense,
  use,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  useTransition,
  type FormEvent,
} from 'react';
import {
  deleteGame,
  deleteOmnisave,
  listGames,
  listOmnisaves,
  listRevisions,
  HeadConflictError,
  updateOmnisaveDisplayName,
  type CatalogGame,
  type Omnisave,
  type Revision,
} from '../lib/omnisave-api.js';
import { ConnectForm } from '../features/connection/connect-form.js';
import {
  createRandomTestOmnisave,
  createTestRevision,
  createTestSave,
  forkTestSave,
} from '../features/debug/debug-actions.js';
import { DebugMenu } from '../features/debug/debug-menu.js';
import {
  DeleteGameDialog,
  DeleteGameSavesDialog,
  DeleteSaveDialog,
} from '../features/games/delete-dialog.js';
import { FixMatchDialog } from '../features/games/fix-match-dialog.js';
import { GameDetail } from '../features/games/game-detail.js';
import {
  GameLibrary,
  GameLibraryLoading,
  buildLibrary,
  preloadGameArtwork,
  type GameSummary,
} from '../features/games/game-library.js';
import { useLibraryEvents, type LibraryEventStatus } from '../features/games/use-library-events.js';

const tokenStorageKey = 'omnisave.api-token';

type DeleteTarget =
  | { type: 'game'; game: GameSummary }
  | { type: 'game-saves'; game: GameSummary }
  | { type: 'save'; game: GameSummary; save: Omnisave; name: string };

function upsertCatalogGame(catalog: CatalogGame[], game: CatalogGame) {
  return catalog.some((candidate) => candidate.id === game.id)
    ? catalog.map((candidate) => (candidate.id === game.id ? game : candidate))
    : [...catalog, game];
}

type LibrarySnapshot = {
  catalog: CatalogGame[] | null;
  saves: Omnisave[];
  error: string;
};

type LibraryResource = {
  promise: Promise<LibrarySnapshot>;
  abort: () => void;
};

async function fetchLibrary(
  token: string,
  signal?: AbortSignal,
  fallback?: LibrarySnapshot
): Promise<LibrarySnapshot> {
  try {
    // The games endpoint is optional; saves remain a usable library fallback.
    const [saves, catalog] = await Promise.all([
      listOmnisaves(token, signal),
      listGames(token, signal).catch((catalogError: unknown) => {
        if (catalogError instanceof DOMException && catalogError.name === 'AbortError') {
          throw catalogError;
        }
        return null;
      }),
    ]);
    await preloadGameArtwork(token, catalog ?? [], signal);
    return { catalog, saves, error: '' };
  } catch (loadError) {
    if (loadError instanceof DOMException && loadError.name === 'AbortError') {
      return fallback ?? { catalog: null, saves: [], error: '' };
    }
    const error = loadError instanceof Error ? loadError.message : 'Could not load the library.';
    if (fallback) return { ...fallback, error };
    return {
      catalog: null,
      saves: [],
      error,
    };
  }
}

function createLibraryResource(token: string, fallback?: LibrarySnapshot): LibraryResource {
  const controller = new AbortController();
  return {
    promise: fetchLibrary(token, controller.signal, fallback),
    abort: () => controller.abort(),
  };
}

function createSettledLibraryResource(snapshot: LibrarySnapshot): LibraryResource {
  return { promise: Promise.resolve(snapshot), abort: () => undefined };
}

const initialLibraryResources = new Map<string, LibraryResource>();

function initialLibraryResource(token: string) {
  let resource = initialLibraryResources.get(token);
  if (!resource) {
    resource = createLibraryResource(token);
    initialLibraryResources.set(token, resource);
  }
  return resource;
}

type LibraryDashboardProps = {
  token: string;
  resource: LibraryResource;
  libraryPending: boolean;
  selectedGameID: string;
  selectedSaveID: string;
  onSelectGameID: (id: string) => void;
  onSelectSaveID: (id: string) => void;
  onCloseGame: () => void;
  onReload: () => Promise<LibrarySnapshot>;
  onReplace: (snapshot: LibrarySnapshot) => void;
  onSnapshot: (snapshot: LibrarySnapshot) => void;
  onEventStatusChange: (status: LibraryEventStatus) => void;
};

function LibraryDashboard({
  token,
  resource,
  libraryPending,
  selectedGameID,
  selectedSaveID,
  onSelectGameID,
  onSelectSaveID,
  onCloseGame,
  onReload,
  onReplace,
  onSnapshot,
  onEventStatusChange,
}: LibraryDashboardProps) {
  const snapshot = use(resource.promise);
  const { catalog, saves } = snapshot;
  const [revisions, setRevisions] = useState<Revision[]>([]);
  const [error, setError] = useState('');
  const [revisionError, setRevisionError] = useState('');
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

  useEffect(() => onSnapshot(snapshot), [onSnapshot, snapshot]);

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
    setRevisions([]);
    setRevisionError('');
    if (!selectedSaveID) return;

    const controller = new AbortController();
    void loadRevisionHistory(token, selectedSaveID, controller.signal);
    return () => controller.abort();
  }, [loadRevisionHistory, selectedSaveID, token]);

  useEffect(() => {
    if (selectedGameID && !games.some((game) => game.id === selectedGameID)) onCloseGame();
    if (selectedSaveID && !saves.some((save) => save.id === selectedSaveID)) onSelectSaveID('');
  }, [games, onCloseGame, onSelectSaveID, saves, selectedGameID, selectedSaveID]);

  function openGame(game: GameSummary) {
    onSelectGameID(game.id);
    onSelectSaveID(game.saves[0]?.id ?? '');
  }

  const refresh = useCallback(async () => {
    setError('');
    await onReload();
    if (selectedSaveID) await loadRevisionHistory(token, selectedSaveID);
  }, [loadRevisionHistory, onReload, selectedSaveID, token]);

  useLibraryEvents({ token, onRefresh: refresh, onStatusChange: onEventStatusChange });

  async function addRandomGame() {
    if (!token) return;

    setDebugAction('game');
    setError('');
    try {
      const created = await createRandomTestOmnisave(
        token,
        games.map((game) => game.label)
      );
      onReplace({
        catalog: catalog ? upsertCatalogGame(catalog, created.game) : null,
        saves: [...saves, created.save],
        error: '',
      });
      created.catalog
        .then(() => onReload())
        .catch((catalogError: unknown) => {
          setError(
            catalogError instanceof Error ? catalogError.message : 'Could not match the test game.'
          );
        });
    } catch (createError) {
      setError(createError instanceof Error ? createError.message : 'Could not create an Omnisave.');
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
      await onReload();
      onSelectSaveID(created.id);
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
      await onReload();
      await loadRevisionHistory(token, selectedSave.id);
    } catch (createError) {
      if (createError instanceof HeadConflictError) {
        await onReload();
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
      await onReload();
      onSelectSaveID(result.omnisave.id);
    } catch (createError) {
      setRevisionError(
        createError instanceof Error ? createError.message : 'Could not fork this save.'
      );
    } finally {
      setDebugAction(null);
    }
  }

  async function renameSave(save: Omnisave, displayName: string) {
    if (!token) return;
    const updated = await updateOmnisaveDisplayName(token, save.id, displayName);
    onReplace({
      catalog,
      saves: saves.map((candidate) => (candidate.id === updated.id ? updated : candidate)),
      error: '',
    });
  }

  function requestDeleteGame(game: GameSummary) {
    setDeleteError('');
    setDeleteTarget({ type: 'game', game });
  }

  function requestDeleteGameSaves(game: GameSummary) {
    setDeleteError('');
    setDeleteTarget({ type: 'game-saves', game });
  }

  function requestDeleteSave(save: Omnisave, name: string) {
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
      if (deleteTarget.type === 'game') {
        await deleteGame(token, deleteTarget.game.id);
        if (selectedGameID === deleteTarget.game.id) onCloseGame();
      } else {
        const savesToDelete =
          deleteTarget.type === 'game-saves' ? deleteTarget.game.saves : [deleteTarget.save];
        for (const save of savesToDelete) {
          await deleteOmnisave(token, save.id);
        }

        if (deleteTarget.type === 'save' && selectedSaveID === deleteTarget.save.id) {
          const nextSave = deleteTarget.game.saves.find((save) => save.id !== deleteTarget.save.id);
          onSelectSaveID(nextSave?.id ?? '');
        }
      }
      setDeleteTarget(undefined);
      await onReload();
    } catch (deleteFailure) {
      setDeleteError(
        deleteFailure instanceof Error
          ? deleteFailure.message
          : deleteTarget.type === 'game'
            ? 'Could not delete this game.'
            : deleteTarget.type === 'game-saves'
              ? 'Could not delete these saves.'
              : 'Could not delete this save.'
      );
    } finally {
      setDeleting(false);
    }
  }

  const librarySummary = catalog
    ? `${games.length} ${games.length === 1 ? 'game' : 'games'} in your library · ${saves.length} ${
        saves.length === 1 ? 'save' : 'saves'
      }.`
    : `${games.length} ${games.length === 1 ? 'game' : 'games'} with saved progress.`;
  const visibleError = error || snapshot.error;

  return (
    <>
      <section
        className={`flex justify-between gap-5 ${selectedGame ? 'items-center' : 'items-end'}`}
      >
        {selectedGame ? (
          <button
            type="button"
            onClick={onCloseGame}
            className="text-sm font-medium text-slate-400 transition hover:text-white"
          >
            ← All games
          </button>
        ) : (
          <div>
            <h1 className="text-2xl font-medium tracking-tight text-white">Games</h1>
            <p className="mt-1.5 text-sm text-neutral-500">{librarySummary}</p>
          </div>
        )}

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
      </section>

      {visibleError ? (
        <div
          role="alert"
          className="mt-5 rounded-md border border-red-400/20 bg-red-400/10 px-4 py-3 text-sm text-red-200"
        >
          {visibleError}
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
          onSelectSave={(save) => onSelectSaveID(save.id)}
          onRequestDelete={requestDeleteSave}
          onRenameSave={renameSave}
        />
      ) : (
        <section className="mt-8" aria-label="Game library" aria-busy={libraryPending}>
          <GameLibrary
            games={games}
            token={token}
            onOpenGame={openGame}
            onRequestFixMatch={setFixMatchTarget}
            onRequestDeleteSaves={requestDeleteGameSaves}
            onRequestDeleteGame={requestDeleteGame}
          />
        </section>
      )}

      {deleteTarget?.type === 'game' ? (
        <DeleteGameDialog
          game={deleteTarget.game}
          deleting={deleting}
          error={deleteError}
          onCancel={cancelDelete}
          onConfirm={() => void confirmDelete()}
        />
      ) : deleteTarget?.type === 'game-saves' ? (
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
            onReplace({
              catalog: catalog ? upsertCatalogGame(catalog, game) : null,
              saves,
              error: '',
            });
            setFixMatchTarget(undefined);
          }}
        />
      ) : null}
    </>
  );
}

export function App() {
  const [token, setToken] = useState(() => sessionStorage.getItem(tokenStorageKey) ?? '');
  const [tokenInput, setTokenInput] = useState(token);
  const [resource, setResource] = useState<LibraryResource | null>(() =>
    token ? initialLibraryResource(token) : null
  );
  const [selectedGameID, setSelectedGameID] = useState('');
  const [selectedSaveID, setSelectedSaveID] = useState('');
  const activeResource = useRef(resource);
  const latestSnapshot = useRef<LibrarySnapshot | undefined>(undefined);
  const [libraryPending, startLibraryTransition] = useTransition();
  const [eventStatus, setEventStatus] = useState<LibraryEventStatus>('connecting');

  const installResource = useCallback(
    (next: LibraryResource, transition: boolean) => {
      activeResource.current?.abort();
      activeResource.current = next;
      if (transition) startLibraryTransition(() => setResource(next));
      else setResource(next);
      return next.promise;
    },
    [startLibraryTransition]
  );

  const reloadLibrary = useCallback(
    () => installResource(createLibraryResource(token, latestSnapshot.current), true),
    [installResource, token]
  );

  const replaceLibrary = useCallback(
    (snapshot: LibrarySnapshot) => {
      latestSnapshot.current = snapshot;
      void installResource(createSettledLibraryResource(snapshot), true);
    },
    [installResource]
  );

  const rememberSnapshot = useCallback((snapshot: LibrarySnapshot) => {
    latestSnapshot.current = snapshot;
  }, []);

  function closeGame() {
    setSelectedGameID('');
    setSelectedSaveID('');
  }

  function connect(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const nextToken = tokenInput.trim();
    if (!nextToken) return;

    sessionStorage.setItem(tokenStorageKey, nextToken);
    setEventStatus('connecting');
    setToken(nextToken);
    if (nextToken === token) void reloadLibrary();
    else {
      latestSnapshot.current = undefined;
      void installResource(createLibraryResource(nextToken), false);
    }
  }

  function disconnect() {
    activeResource.current?.abort();
    activeResource.current = null;
    latestSnapshot.current = undefined;
    sessionStorage.removeItem(tokenStorageKey);
    setToken('');
    setTokenInput('');
    setResource(null);
    closeGame();
  }

  const connectionLabel = !token
    ? 'Not connected'
    : libraryPending
      ? 'Syncing…'
      : eventStatus === 'live'
        ? 'Connected'
        : eventStatus === 'unauthorized'
          ? 'Authentication failed'
          : eventStatus === 'retrying'
            ? 'Reconnecting…'
            : 'Connecting…';
  const connectionColor = !token
    ? 'bg-neutral-600'
    : eventStatus === 'unauthorized'
      ? 'bg-red-400'
      : eventStatus === 'live'
        ? 'bg-[#e5a00d]'
        : 'bg-sky-400';

  return (
    <div className="min-h-screen bg-[#111111] text-[#e5e5e5]">
      <header className="border-b border-white/5 bg-[#181818]">
        <div className="flex items-center justify-between px-5 py-3 sm:px-8">
          <button type="button" onClick={closeGame} className="flex items-center gap-3 text-left">
            <span className="grid size-8 place-items-center rounded-md bg-[#e5a00d] text-sm font-black text-black">
              O
            </span>
            <span className="text-sm font-semibold text-white">Omnisave</span>
          </button>

          <div className="flex items-center gap-3">
            <span className="hidden items-center gap-2 text-xs text-slate-400 sm:flex">
              <span className={`size-1.5 rounded-full ${connectionColor}`} aria-hidden="true" />
              {connectionLabel}
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
        ) : resource ? (
          <Suspense fallback={<GameLibraryLoading />}>
            <LibraryDashboard
              token={token}
              resource={resource}
              libraryPending={libraryPending}
              selectedGameID={selectedGameID}
              selectedSaveID={selectedSaveID}
              onSelectGameID={setSelectedGameID}
              onSelectSaveID={setSelectedSaveID}
              onCloseGame={closeGame}
              onReload={reloadLibrary}
              onReplace={replaceLibrary}
              onSnapshot={rememberSnapshot}
              onEventStatusChange={setEventStatus}
            />
          </Suspense>
        ) : null}
      </main>
    </div>
  );
}
