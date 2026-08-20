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
  listPresence,
  forkOmnisave,
  restoreRevision,
  serverAccess,
  UnauthorizedError,
  signIn,
  CurrentRevisionConflictError,
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
import { DeleteGameDialog, DeleteGameSavesDialog } from '../features/game/delete-game-dialog.js';
import { preloadGameArtwork } from '../features/game/game-artwork.js';
import { GameDetail } from '../features/game/game-detail.js';
import type { GameSummary } from '../features/game/game-summary.js';
import { applyPresence } from '../features/game/playing-devices.js';
import { buildLibrary } from '../features/library/build-library.js';
import { FixMatchDialog } from '../features/library/fix-match-dialog.js';
import {
  LibrarySortControl,
  sortLibrary,
  storedLibrarySort,
} from '../features/library/library-sort.js';
import { DeleteSaveDialog } from '../features/omnisave/delete-save-dialog.js';
import { GameLibrary, GameLibraryLoading } from '../features/library/game-library.js';
import { NowPlaying } from '../features/library/now-playing.js';
import { navigate, useRoute } from '../lib/route.js';
import { useServerEvents, type ServerEventStatus } from '../lib/use-server-events.js';
import { ConnectionBanner, NavigationBar, NavigationRail, TopBar } from './navigation-chrome.js';

/** The browser stores only its issued credential, never the owner token. */
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
  // A rejected credential returns the browser to sign-in.
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
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget>();
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState('');
  const [fixMatchTarget, setFixMatchTarget] = useState<GameSummary>();
  const [librarySort, setLibrarySort] = useState(storedLibrarySort);

  const games = useMemo(() => buildLibrary(catalog, saves), [catalog, saves]);
  // The sort rearranges only the grid; Playing now keeps its own order.
  const sortedGames = useMemo(() => sortLibrary(games, librarySort), [games, librarySort]);
  const selectedGame = useMemo(
    () => games.find((game) => game.id === selectedGameID),
    [games, selectedGameID]
  );
  const selectedSave = useMemo(
    () => selectedGame?.saves.find((save) => save.id === selectedSaveID),
    [selectedGame, selectedSaveID]
  );

  useEffect(() => onSnapshot(snapshot), [onSnapshot, snapshot]);

  // History and revision-action errors belong to the selected save.
  useEffect(() => setRevisionError(''), [selectedSaveID]);

  // A link can name a game that is gone, and a save can be deleted while it is open.
  useEffect(() => {
    if (selectedGameID && !games.some((game) => game.id === selectedGameID)) onCloseGame();
    else if (selectedSaveID && !saves.some((save) => save.id === selectedSaveID)) {
      setSelectedSaveID('');
    }
  }, [games, onCloseGame, saves, selectedGameID, selectedSaveID]);

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

  async function restoreSaveRevision(save: Omnisave, revision: Revision) {
    if (!token) return;
    try {
      await restoreRevision(token, save.id, revision.id, save.current_revision_id);
      await onReload();
      setSelectedSaveID(save.id);
    } catch (restoreError) {
      if (restoreError instanceof CurrentRevisionConflictError) await onReload();
      throw restoreError;
    }
  }

  async function forkSaveAtRevision(save: Omnisave, revision: Revision, displayName: string) {
    if (!token) return;
    const result = await forkOmnisave(token, save.id, {
      revisionID: revision.id,
      displayName,
    });
    await onReload();
    setSelectedSaveID(result.omnisave.id);
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

  const visibleError = error || snapshot.error;

  return (
    <>
      {/* The top bar names this section and the rail leads back out, so
          neither the Library nor an open game draws a header or a back link
          of its own. */}
      {visibleError ? (
        <div
          role="alert"
          className="mt-5 rounded-md border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger"
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
          onRestoreRevision={restoreSaveRevision}
          onForkRevision={forkSaveAtRevision}
        />
      ) : (
        <div aria-busy={libraryPending}>
          <NowPlaying games={games} token={token} />

          <section aria-label="Game library">
            {games.length > 0 ? (
              <div className="mb-5 flex items-center gap-4">
                <LibrarySortControl sort={librarySort} onSortChange={setLibrarySort} />
                <span
                  className="rounded-md bg-text/8 px-2.5 py-1 text-sm font-medium text-muted"
                  aria-label={`${games.length} ${games.length === 1 ? 'game' : 'games'}`}
                >
                  {games.length}
                </span>
              </div>
            ) : null}
            <GameLibrary
              games={sortedGames}
              token={token}
              onRequestFixMatch={setFixMatchTarget}
              onRequestDeleteSaves={requestDeleteGameSaves}
              onRequestDeleteGame={requestDeleteGame}
            />
          </section>
        </div>
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
  // Manual opening may show an empty request list.
  const [requestsOpen, setRequestsOpen] = useState(false);
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

  // Discard rejected credentials and return to sign-in.
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
      // Library requests may detect a rejected credential before the event stream.
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
      // Ignore unreadable requests and wait for the next event.
    }
  }, [token]);

  // One shell stream refreshes the Library and surfaces expiring pairing requests.
  const refreshAll = useCallback(async () => {
    await Promise.all([resource ? reloadLibrary() : Promise.resolve(), refreshPending()]);
  }, [refreshPending, reloadLibrary, resource]);

  // Presence events update playing flags; failures fall back to a full reload.
  const refreshPresence = useCallback(async () => {
    if (!token || !latestSnapshot.current?.catalog) {
      await refreshAll();
      return;
    }
    try {
      const { devices } = await listPresence(token);
      const current = latestSnapshot.current;
      if (!current?.catalog) return;
      replaceLibrary({ ...current, catalog: applyPresence(current.catalog, devices) });
    } catch {
      await reloadLibrary();
    }
  }, [token, refreshAll, reloadLibrary, replaceLibrary]);

  useServerEvents({
    token,
    eventTypes: ['library.changed', 'access.changed', 'devices.changed'],
    onRefresh: (events) =>
      events.length > 0 && events.every((event) => event === 'devices.changed')
        ? refreshPresence()
        : refreshAll(),
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

  // Correct stale or inaccessible game routes without adding browser history.
  const closeGame = useCallback(() => navigate({ name: 'library' }, { replace: true }), []);

  // Store the issued browser credential and discard the proof used to obtain it.
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
      // Switch to PIN sign-in if another browser claimed the server first.
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
    // Signing out forgets the local credential without revoking it.
    forgetCredential();
    closeGame();
  }

  // Dismissal leaves the request pending; a later request may interrupt again.
  const unanswered = pending.filter((request) => !dismissed.includes(request.id));

  // Only transient connection failures need a status message.
  const connectionLost = Boolean(token) && eventStatus === 'retrying';

  return (
    <div className="flex min-h-screen flex-col bg-bg text-text">
      {/* Above the menu as well as the content: what it reports is true of the
          whole app, not of the section anyone happens to be reading. */}
      <ConnectionBanner lost={connectionLost} />

      <div className="flex flex-1">
        {token ? <NavigationRail route={route} /> : null}

        <div className="flex min-w-0 flex-1 flex-col">
          {/* Every page starts below this bar: it names the section being
              read and holds the app-wide controls, so no page draws a title
              of its own. */}
          {token ? (
            <TopBar
              title={route.name === 'settings' ? 'Server' : 'Games Library'}
              back={route.name === 'game' ? { name: 'library' } : undefined}
              pendingCount={pending.length}
              onOpenRequests={() => setRequestsOpen(true)}
            />
          ) : null}

          <main className="flex-1 px-5 pt-4 pb-8 sm:px-8 lg:px-10">
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
              <ServerSettings token={token} credentialID={credential.id} onDisconnect={disconnect} />
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

          {token ? <NavigationBar route={route} /> : null}
        </div>
      </div>

      {/* One dialog for both ways in: a new request opens it over whatever the
          owner was doing, and the top bar's control opens it deliberately —
          showing every live request, dismissed ones included. */}
      {token && (requestsOpen || unanswered.length > 0) ? (
        <PairingDialog
          requests={requestsOpen ? pending : unanswered}
          busyID={answering}
          error={pairingError}
          onApprove={(request) => void answerPairing(request, true)}
          onDeny={(request) => void answerPairing(request, false)}
          onDismiss={() => {
            setRequestsOpen(false);
            setDismissed(pending.map((request) => request.id));
          }}
        />
      ) : null}
    </div>
  );
}
