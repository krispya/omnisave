import { Suspense, useCallback, useEffect, useState } from 'react';
import {
  forgetStoredCredential,
  storeCredential,
  storedCredential,
  type StoredCredential,
} from '../features/connection/browser-credential.js';
import { ConnectForm } from '../features/connection/connect-form.js';
import { PairingDialog } from '../features/connection/pairing-dialog.js';
import { ServerSettings } from '../features/connection/server-settings.js';
import { useConnect } from '../features/connection/use-connect.js';
import { usePairingRequests } from '../features/connection/use-pairing-requests.js';
import { GameLibraryLoading } from '../features/library/game-library.js';
import { LibraryDashboard } from '../features/library/library-dashboard.js';
import { useLibrary, type LibrarySnapshot } from '../features/library/use-library.js';
import { navigate, useRoute } from '../lib/route.js';
import { useServerEvents, type ServerEventStatus } from '../lib/use-server-events.js';
import { ConnectionBanner, NavigationBar, NavigationRail, TopBar } from './navigation-chrome.js';

export function App() {
  const [credential, setCredential] = useState<StoredCredential>(storedCredential);
  const token = credential.token;
  const route = useRoute();
  const selectedGameID = route.name === 'game' ? route.gameID : '';
  const [eventStatus, setEventStatus] = useState<ServerEventStatus>('connecting');
  const {
    resource,
    libraryPending,
    reloadLibrary,
    replaceLibrary,
    rememberSnapshot,
    loadLibrary,
    clearLibrary,
    refreshPresence,
  } = useLibrary(token);
  const pairing = usePairingRequests(token);
  const connect = useConnect(token, adoptCredential);

  // Store the issued browser credential and start loading the Library it opens.
  function adoptCredential(next: StoredCredential) {
    storeCredential(next);
    setEventStatus('connecting');
    setCredential(next);
    loadLibrary(next.token);
  }

  // Discard rejected credentials and return to sign-in.
  const forgetCredential = useCallback(() => {
    clearLibrary();
    forgetStoredCredential();
    setCredential({ id: '', token: '' });
  }, [clearLibrary]);

  const receiveSnapshot = useCallback(
    (snapshot: LibrarySnapshot) => {
      // Library requests may detect a rejected credential before the event stream.
      if (snapshot.unauthorized) {
        forgetCredential();
        return;
      }
      rememberSnapshot(snapshot);
    },
    [forgetCredential, rememberSnapshot]
  );

  useEffect(() => {
    if (eventStatus === 'unauthorized') forgetCredential();
  }, [eventStatus, forgetCredential]);

  // One shell stream refreshes the Library and surfaces expiring pairing requests.
  async function refreshAll() {
    await Promise.all([resource ? reloadLibrary() : Promise.resolve(), pairing.refresh()]);
  }

  useServerEvents({
    token,
    eventTypes: ['library.changed', 'access.changed', 'devices.changed'],
    onRefresh: (events) =>
      events.length > 0 && events.every((event) => event === 'devices.changed')
        ? refreshPresence(refreshAll)
        : refreshAll(),
    onStatusChange: setEventStatus,
  });

  // Correct stale or inaccessible game routes without adding browser history.
  const closeGame = useCallback(() => navigate({ name: 'library' }, { replace: true }), []);

  function disconnect() {
    // Signing out forgets the local credential without revoking it.
    forgetCredential();
    closeGame();
  }

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
              pendingCount={pairing.pending.length}
              onOpenRequests={pairing.openRequests}
            />
          ) : null}

          <main className="flex-1 px-5 pt-4 pb-8 sm:px-8 lg:px-10">
            {!token ? (
              <ConnectForm
                claimable={connect.access.claimable}
                pinSet={connect.access.pinSet}
                pending={connect.connecting}
                error={connect.error}
                onClaim={connect.claim}
                onSignIn={connect.enterPIN}
                onOwnerToken={connect.enterOwnerToken}
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
                  onSnapshot={receiveSnapshot}
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
      {token && pairing.open ? (
        <PairingDialog
          requests={pairing.requests}
          busyID={pairing.answering}
          error={pairing.error}
          onApprove={(request) => void pairing.answer(request, true)}
          onDeny={(request) => void pairing.answer(request, false)}
          onDismiss={pairing.dismiss}
        />
      ) : null}
    </div>
  );
}
