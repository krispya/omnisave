import { useCallback, useRef, useState, useTransition } from 'react';
import {
  listGames,
  listOmnisaves,
  listPresence,
  UnauthorizedError,
  type CatalogGame,
  type Omnisave,
} from '../../lib/omnisave-api.js';
import { preloadGameArtwork } from '../game/game-artwork.js';
import { applyPresence } from '../game/playing-devices.js';

/** One load of the Library: the catalog of Games and every save. */
export type LibrarySnapshot = {
  catalog: CatalogGame[] | null;
  saves: Omnisave[];
  error: string;
  // A rejected credential returns the browser to sign-in.
  unauthorized?: boolean;
};

/** A Library load the dashboard suspends on, abortable when a newer one replaces it. */
export type LibraryResource = {
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

// Keyed by token so a repeated first render reuses the load it already started.
const initialLibraryResources = new Map<string, LibraryResource>();

function initialLibraryResource(token: string) {
  let resource = initialLibraryResources.get(token);
  if (!resource) {
    resource = createLibraryResource(token);
    initialLibraryResources.set(token, resource);
  }
  return resource;
}

/**
 * The Library as loaded with one credential. Loads start as a `resource` the
 * dashboard suspends on; the last snapshot it showed is kept so reloads and
 * presence updates build on it rather than blanking the screen.
 */
export function useLibrary(token: string) {
  const [resource, setResource] = useState<LibraryResource | null>(() =>
    token ? initialLibraryResource(token) : null
  );
  const activeResource = useRef(resource);
  const latestSnapshot = useRef<LibrarySnapshot | undefined>(undefined);
  const [libraryPending, startLibraryTransition] = useTransition();

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

  /** Reloads behind the shown Library, keeping it if the reload fails. */
  const reloadLibrary = useCallback(
    () => installResource(createLibraryResource(token, latestSnapshot.current), true),
    [installResource, token]
  );

  /** Shows a snapshot already updated locally, without asking the server. */
  const replaceLibrary = useCallback(
    (snapshot: LibrarySnapshot) => {
      latestSnapshot.current = snapshot;
      void installResource(createSettledLibraryResource(snapshot), true);
    },
    [installResource]
  );

  /** Records the snapshot on screen as the base for later reloads. */
  const rememberSnapshot = useCallback((snapshot: LibrarySnapshot) => {
    latestSnapshot.current = snapshot;
  }, []);

  /** Starts a fresh load for a newly issued credential, suspending rather than transitioning. */
  const loadLibrary = useCallback(
    (nextToken: string) => {
      latestSnapshot.current = undefined;
      void installResource(createLibraryResource(nextToken), false);
    },
    [installResource]
  );

  /** Stops loading and forgets what was shown when the credential is discarded. */
  const clearLibrary = useCallback(() => {
    activeResource.current?.abort();
    activeResource.current = null;
    latestSnapshot.current = undefined;
    setResource(null);
  }, []);

  /**
   * Presence events update playing flags on the shown catalog. With no
   * catalog shown yet it runs `fallback` instead; a failed read reloads.
   */
  const refreshPresence = useCallback(
    async (fallback: () => Promise<void>) => {
      if (!token || !latestSnapshot.current?.catalog) {
        await fallback();
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
    },
    [token, reloadLibrary, replaceLibrary]
  );

  return {
    /** The load the dashboard suspends on; null while signed out. */
    resource,
    /** True while a reload renders behind the shown Library. */
    libraryPending,
    reloadLibrary,
    replaceLibrary,
    rememberSnapshot,
    loadLibrary,
    clearLibrary,
    refreshPresence,
  };
}
