import { useEffect, useState } from 'react';
import {
  claimServer,
  exchangeOwnerToken,
  serverAccess,
  signIn,
  type IssuedCredential,
  type ServerAccess,
} from '../../lib/omnisave-api.js';
import type { StoredCredential } from './browser-credential.js';

/** A name the owner will recognize in the list of what holds a credential. */
function browserLabel() {
  const platform = navigator.userAgent.match(/\(([^;)]+)/)?.[1];
  return platform ? `Dash on ${platform.trim()}` : 'Dash';
}

/**
 * Claims the server or signs in, handing the credential issued to this browser
 * to `onConnect`. While `token` is empty it reads whether the server can still
 * be claimed or wants its PIN; the last answer is kept through a sign-out so
 * the connect form does not flash another mode while it reads again.
 */
export function useConnect(token: string, onConnect: (credential: StoredCredential) => void) {
  const [connecting, setConnecting] = useState(false);
  const [error, setError] = useState('');
  const [access, setAccess] = useState<ServerAccess>({ claimable: false, pinSet: false });

  useEffect(() => {
    if (token) return;
    const controller = new AbortController();
    serverAccess(controller.signal)
      .then(setAccess)
      .catch(() => setAccess({ claimable: false, pinSet: false }));
    return () => controller.abort();
  }, [token]);

  // Hand over the issued browser credential; the proof used to obtain it is not kept.
  async function establish(issue: () => Promise<IssuedCredential>) {
    if (connecting) return;
    setConnecting(true);
    setError('');
    try {
      const issued = await issue();
      onConnect({ id: issued.credential.id, token: issued.token });
    } catch (issueError) {
      setError(issueError instanceof Error ? issueError.message : 'Could not connect.');
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

  return { access, connecting, error, claim, enterPIN, enterOwnerToken };
}
