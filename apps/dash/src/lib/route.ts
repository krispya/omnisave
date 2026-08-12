import { useMemo, useSyncExternalStore } from 'react';

/** A shareable Dash view: the Library, one game, or server settings. */
export type Route = { name: 'library' } | { name: 'game'; gameID: string } | { name: 'settings' };

const routeChanged = 'omnisave:routechange';

export function parseRoute(pathname: string): Route {
  const segments = pathname.split('/').filter(Boolean).map(decodeURIComponent);
  if (segments[0] === 'settings') return { name: 'settings' };
  if (segments[0] !== 'games' || !segments[1]) return { name: 'library' };

  return { name: 'game', gameID: segments[1] };
}

export function routePath(route: Route) {
  switch (route.name) {
    case 'library':
      return '/';
    case 'settings':
      return '/settings';
    default:
      return `/games/${encodeURIComponent(route.gameID)}`;
  }
}

/** Shows a route, replacing history when correcting stale or inaccessible state. */
export function navigate(route: Route, options?: { replace?: boolean }) {
  const path = routePath(route);
  if (path === window.location.pathname) return;

  if (options?.replace) window.history.replaceState(null, '', path);
  else window.history.pushState(null, '', path);
  // The history API stays silent about its own calls, so listeners need telling.
  window.dispatchEvent(new Event(routeChanged));
}

/** Reports whether an unmodified same-origin link should use client-side navigation. */
export function handledByRouter(event: {
  defaultPrevented: boolean;
  button: number;
  metaKey: boolean;
  ctrlKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
}) {
  return (
    !event.defaultPrevented &&
    event.button === 0 &&
    !event.metaKey &&
    !event.ctrlKey &&
    !event.shiftKey &&
    !event.altKey
  );
}

function subscribe(onChange: () => void) {
  window.addEventListener('popstate', onChange);
  window.addEventListener(routeChanged, onChange);
  return () => {
    window.removeEventListener('popstate', onChange);
    window.removeEventListener(routeChanged, onChange);
  };
}

export function useRoute(): Route {
  const pathname = useSyncExternalStore(subscribe, () => window.location.pathname);
  return useMemo(() => parseRoute(pathname), [pathname]);
}
