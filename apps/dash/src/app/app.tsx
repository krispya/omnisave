import {
  Suspense,
  use,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  useTransition,
} from 'react';
import {
  deleteGame,
  deleteOmnisave,
  downloadOmnisaveArchive,
  downloadRevisionArchive,
  claimServer,
  exchangeOwnerToken,
  listGames,
  approvePairingRequest,
  denyPairingRequest,
  listOmnisaves,
  listPairingRequests,
  serverAccess,
  UnauthorizedError,
  signIn,
  HeadConflictError,
  updateOmnisaveDisplayName,
  type CatalogGame,
  type IssuedCredential,
  type PairingRequest,
  type Omnisave,
  type Revision,
} from '../lib/omnisave-api.js';
import { ConnectForm } from '../features/connection/connect-form.js';
import { PairingDialog } from '../features/connection/pairing-dialog.js';
import { ServerSettings } from '../features/connection/server-settings.js';
import {
  createRandomTestOmnisave,
  createTestRevision,
  createTestSave,
  forkTestSave,
} from '../features/debug/debug-actions.js';
import { DebugMenu } from '../features/debug/debug-menu.js';
import { DeleteGameDialog, DeleteGameSavesDialog } from '../features/game/delete-game-dialog.js';
import { preloadGameArtwork } from '../features/game/game-artwork.js';
import { GameDetail } from '../features/game/game-detail.js';
import type { GameSummary } from '../features/game/game-summary.js';
import { buildLibrary } from '../features/library/build-library.js';
import { FixMatchDialog } from '../features/library/fix-match-dialog.js';
import { DeleteSaveDialog } from '../features/omnisave/delete-save-dialog.js';
import { GameLibrary, GameLibraryLoading } from '../features/library/game-library.js';
import { navigate, useRoute } from '../lib/route.js';
import { useServerEvents, type ServerEventStatus } from '../lib/use-server-events.js';
import { RouteLink } from '../components/route-link.js';

/**
 * The Dash holds a credential of its own, traded for the owner token once and
 * revocable like any device's (ADR-007). The owner's secret is never stored
 * here — only what this browser was issued in exchange for it.
 */
const credentialStorageKey = 'omnisave.credential';

type StoredCredential = { id: string; token: string };

function storedCredential(): StoredCredential {
  try {
    const stored = localStorage.getItem(credentialStorageKey);
    if (!stored) return { id: '', token: '' };
    const parsed = JSON.parse(stored) as Partial<StoredCredential>;
    return { id: parsed.id ?? '', token: parsed.token ?? '' };
  } catch {
    return { id: '', token: '' };
  }
}

/** A name the owner will recognize in the list of what holds a credential. */
function browserLabel() {
  const platform = navigator.userAgent.match(/\(([^;)]+)/)?.[1];
  return platform ? `Dash on ${platform.trim()}` : 'Dash';
}

type DeleteTarget =
  | { type: 'game'; game: GameSummary }
  | { type: 'game-saves'; game: GameSummary }
  | { type: 'save'; game: GameSummary; save: Omnisave; name: string };

function saveArchiveToDisk(archive: Blob, filename: string) {
  const url = URL.createObjectURL(archive);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  anchor.click();
  URL.revokeObjectURL(url);
}

// Mirrors the server's Content-Disposition stamp so browser and curl
// downloads of one revision land under the same name.
function archiveStamp(createdAt: string) {
  const date = new Date(createdAt);
  const pad = (value: number) => String(value).padStart(2, '0');
  return (
    `${date.getUTCFullYear()}-${pad(date.getUTCMonth() + 1)}-${pad(date.getUTCDate())} ` +
    `${pad(date.getUTCHours())}${pad(date.getUTCMinutes())}${pad(date.getUTCSeconds())}`
  );
}

function upsertCatalogGame(catalog: CatalogGame[], game: CatalogGame) {
  return catalog.some((candidate) => candidate.id === game.id)
    ? catalog.map((candidate) => (candidate.id === game.id ? game : candidate))
    : [...catalog, game];
}

type LibrarySnapshot = {
  catalog: CatalogGame[] | null;
  saves: Omnisave[];
  error: string;
  // Set when the server refused this browser's credential. It is not an error
  // to show — it means this browser has to sign in again.
  unauthorized?: boolean;
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
    if (loadError instanceof UnauthorizedError) {
      return { catalog: null, saves: [], error: '', unauthorized: true };
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
  onCloseGame: () => void;
  onReload: () => Promise<LibrarySnapshot>;
  onReplace: (snapshot: LibrarySnapshot) => void;
  onSnapshot: (snapshot: LibrarySnapshot) => void;
};

function LibraryDashboard({
  token,
  resource,
  libraryPending,
  selectedGameID,
  onCloseGame,
  onReload,
  onReplace,
  onSnapshot,
}: LibraryDashboardProps) {
  const snapshot = use(resource.promise);
  const { catalog, saves } = snapshot;
  // The open save belongs to the game being read, not to the address bar.
  const [selectedSaveID, setSelectedSaveID] = useState('');
  const [error, setError] = useState('');
  const [revisionError, setRevisionError] = useState('');
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

  // The save's history follows its head, so reloading the library is enough to refresh it.
  useEffect(() => setRevisionError(''), [selectedSaveID]);

  // A link can name a game that is gone, and a save can be deleted while it is open.
  useEffect(() => {
    if (selectedGameID && !games.some((game) => game.id === selectedGameID)) onCloseGame();
    else if (selectedSaveID && !saves.some((save) => save.id === selectedSaveID)) {
      setSelectedSaveID('');
    }
  }, [games, onCloseGame, saves, selectedGameID, selectedSaveID]);

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
      await onReload();
    } catch (createError) {
      if (createError instanceof HeadConflictError) await onReload();
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
      setSelectedSaveID(result.omnisave.id);
    } catch (createError) {
      setRevisionError(
        createError instanceof Error ? createError.message : 'Could not fork this save.'
      );
    } finally {
      setDebugAction(null);
    }
  }

  async function downloadSave(save: Omnisave, name: string) {
    if (!token) return;
    setError('');
    try {
      saveArchiveToDisk(await downloadOmnisaveArchive(token, save.id), `${name}.zip`);
    } catch (downloadError) {
      setError(
        downloadError instanceof Error ? downloadError.message : 'Could not download this save.'
      );
    }
  }

  async function downloadRevision(save: Omnisave, name: string, revision: Revision) {
    if (!token) return;
    setRevisionError('');
    try {
      saveArchiveToDisk(
        await downloadRevisionArchive(token, save.id, revision.id),
        `${name} ${revision.display_name?.trim() || archiveStamp(revision.created_at)}.zip`
      );
    } catch (downloadError) {
      setRevisionError(
        downloadError instanceof Error ? downloadError.message : 'Could not download this revision.'
      );
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
          setSelectedSaveID(nextSave?.id ?? '');
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
          <RouteLink
            to={{ name: 'library' }}
            className="text-sm font-medium text-slate-400 transition hover:text-white"
          >
            ← All games
          </RouteLink>
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
          revisionError={revisionError}
          onSelectSave={(save) => setSelectedSaveID(save?.id ?? '')}
          onDownloadSave={(save, name) => void downloadSave(save, name)}
          onDownloadRevision={(save, name, revision) => void downloadRevision(save, name, revision)}
          onRequestDelete={requestDeleteSave}
          onRenameSave={renameSave}
        />
      ) : (
        <section className="mt-8" aria-label="Game library" aria-busy={libraryPending}>
          <GameLibrary
            games={games}
            token={token}
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
  const [credential, setCredential] = useState<StoredCredential>(storedCredential);
  const token = credential.token;
  const [connecting, setConnecting] = useState(false);
  const [connectError, setConnectError] = useState('');
  const [access, setAccess] = useState({ claimable: false, pinSet: false });
  const [pending, setPending] = useState<PairingRequest[]>([]);
  const [answering, setAnswering] = useState('');
  const [pairingError, setPairingError] = useState('');
  const [dismissed, setDismissed] = useState<string[]>([]);
  const [resource, setResource] = useState<LibraryResource | null>(() =>
    token ? initialLibraryResource(token) : null
  );
  const route = useRoute();
  const selectedGameID = route.name === 'game' ? route.gameID : '';
  const activeResource = useRef(resource);
  const latestSnapshot = useRef<LibrarySnapshot | undefined>(undefined);
  const [libraryPending, startLibraryTransition] = useTransition();
  const [eventStatus, setEventStatus] = useState<ServerEventStatus>('connecting');

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

  // A credential the server no longer accepts is not an error to display. Drop
  // it and go back to the way in, which is where someone can do something.
  const forgetCredential = useCallback(() => {
    activeResource.current?.abort();
    activeResource.current = null;
    latestSnapshot.current = undefined;
    localStorage.removeItem(credentialStorageKey);
    setCredential({ id: '', token: '' });
    setResource(null);
  }, []);

  const rememberSnapshot = useCallback(
    (snapshot: LibrarySnapshot) => {
      // The library load answers before the event stream does, so this is
      // usually what notices a revoked or stale credential first.
      if (snapshot.unauthorized) {
        forgetCredential();
        return;
      }
      latestSnapshot.current = snapshot;
    },
    [forgetCredential]
  );

  useEffect(() => {
    if (eventStatus === 'unauthorized') forgetCredential();
  }, [eventStatus, forgetCredential]);

  const refreshPending = useCallback(async () => {
    if (!token) return;
    try {
      setPending(await listPairingRequests(token));
    } catch {
      // A request nobody can read is not worth interrupting anyone about; the
      // stream will bring the next one.
    }
  }, [token]);

  // One stream for the whole shell. The Library refreshes on its own changes,
  // and a device asking to connect reaches the owner wherever they are —
  // pending requests expire in minutes, so waiting for a visit to the server
  // settings would mean missing most of them.
  const refreshAll = useCallback(async () => {
    await Promise.all([resource ? reloadLibrary() : Promise.resolve(), refreshPending()]);
  }, [refreshPending, reloadLibrary, resource]);

  useServerEvents({
    token,
    eventTypes: ['library.changed', 'access.changed'],
    onRefresh: refreshAll,
    onStatusChange: setEventStatus,
  });

  useEffect(() => {
    void refreshPending();
  }, [refreshPending]);

  async function answerPairing(request: PairingRequest, approve: boolean) {
    setAnswering(request.id);
    setPairingError('');
    try {
      await (approve
        ? approvePairingRequest(token, request.id)
        : denyPairingRequest(token, request.id));
      await refreshPending();
    } catch (answerError) {
      setPairingError(answerError instanceof Error ? answerError.message : 'That did not work.');
      await refreshPending();
    } finally {
      setAnswering('');
    }
  }

  useEffect(() => {
    if (token) return;
    const controller = new AbortController();
    serverAccess(controller.signal)
      .then(setAccess)
      .catch(() => setAccess({ claimable: false, pinSet: false }));
    return () => controller.abort();
  }, [token]);

  // Only corrections close a game on the reader's behalf — a stale link, a disconnect —
  // so this replaces rather than pushes. The visible way back is a link.
  const closeGame = useCallback(() => navigate({ name: 'library' }, { replace: true }), []);

  // Both ways in end the same way: this browser holds a credential of its own,
  // and whatever proved it was entitled to one is not kept.
  async function establish(issue: () => Promise<IssuedCredential>) {
    if (connecting) return;
    setConnecting(true);
    setConnectError('');
    try {
      const issued = await issue();
      const next = { id: issued.credential.id, token: issued.token };
      localStorage.setItem(credentialStorageKey, JSON.stringify(next));
      setEventStatus('connecting');
      setCredential(next);
      latestSnapshot.current = undefined;
      void installResource(createLibraryResource(next.token), false);
    } catch (issueError) {
      setConnectError(issueError instanceof Error ? issueError.message : 'Could not connect.');
      // A server claimed out from under this browser should stop offering to
      // claim it and start asking for the PIN.
      serverAccess()
        .then(setAccess)
        .catch(() => undefined);
    } finally {
      setConnecting(false);
    }
  }

  function claim(pin: string) {
    void establish(() => claimServer(browserLabel(), pin));
  }

  function enterPIN(pin: string) {
    void establish(() => signIn(browserLabel(), pin));
  }

  function enterOwnerToken(ownerToken: string) {
    if (ownerToken) void establish(() => exchangeOwnerToken(ownerToken, browserLabel()));
  }

  function disconnect() {
    // The credential stays valid on the server; forgetting it here is not
    // revoking it, which is a deliberate act in the server settings.
    forgetCredential();
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

  // Dismissing sets this request aside without answering it; the next one to
  // arrive still interrupts.
  const unanswered = pending.filter((request) => !dismissed.includes(request.id));

  return (
    <div className="min-h-screen bg-[#111111] text-[#e5e5e5]">
      <header className="border-b border-white/5 bg-[#181818]">
        <div className="flex items-center justify-between px-5 py-3 sm:px-8">
          <RouteLink to={{ name: 'library' }} className="flex items-center gap-3 text-left">
            <span className="grid size-8 place-items-center rounded-md bg-[#e5a00d] text-sm font-black text-black">
              O
            </span>
            <span className="text-sm font-semibold text-white">Omnisave</span>
          </RouteLink>

          <div className="flex items-center gap-3">
            <span className="hidden items-center gap-2 text-xs text-slate-400 sm:flex">
              <span className={`size-1.5 rounded-full ${connectionColor}`} aria-hidden="true" />
              {connectionLabel}
            </span>
            {token ? (
              <>
                <RouteLink
                  to={{ name: 'settings' }}
                  className={`rounded-md px-3 py-2 text-xs font-medium transition hover:bg-white/5 hover:text-white ${
                    route.name === 'settings' ? 'text-white' : 'text-neutral-400'
                  }`}
                >
                  Server
                </RouteLink>
                <button
                  type="button"
                  onClick={disconnect}
                  className="rounded-md px-3 py-2 text-xs font-medium text-neutral-400 transition hover:bg-white/5 hover:text-white"
                >
                  Disconnect
                </button>
              </>
            ) : null}
          </div>
        </div>
      </header>

      <main className="px-5 py-8 sm:px-8 lg:px-10">
        {!token ? (
          <ConnectForm
            claimable={access.claimable}
            pinSet={access.pinSet}
            pending={connecting}
            error={connectError}
            onClaim={claim}
            onSignIn={enterPIN}
            onOwnerToken={enterOwnerToken}
          />
        ) : route.name === 'settings' ? (
          <ServerSettings
            token={token}
            credentialID={credential.id}
            requests={pending}
            onAnswered={refreshPending}
          />
        ) : resource ? (
          <Suspense fallback={<GameLibraryLoading />}>
            <LibraryDashboard
              token={token}
              resource={resource}
              libraryPending={libraryPending}
              selectedGameID={selectedGameID}
              onCloseGame={closeGame}
              onReload={reloadLibrary}
              onReplace={replaceLibrary}
              onSnapshot={rememberSnapshot}
            />
          </Suspense>
        ) : null}
      </main>

      {token && route.name !== 'settings' && unanswered.length > 0 ? (
        <PairingDialog
          requests={unanswered}
          busyID={answering}
          error={pairingError}
          onApprove={(request) => void answerPairing(request, true)}
          onDeny={(request) => void answerPairing(request, false)}
          onDismiss={() => setDismissed(pending.map((request) => request.id))}
        />
      ) : null}
    </div>
  );
}
