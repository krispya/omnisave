import { useMemo, useSyncExternalStore } from 'react';

/**
 * What the dash is showing, as a shareable link:
 *
 *   /                   the Library
 *   /games/<game>       one game
 *
 * Only whole views get a route. Which save has its history open and which way a game's
 * saves are drawn are things a reader does inside a game, not places to link to; a
 * focused save view would be its own route.
 */
export type Route = { name: 'library' } | { name: 'game'; gameID: string };

const routeChanged = 'omnisave:routechange';

export function parseRoute(pathname: string): Route {
  const segments = pathname.split('/').filter(Boolean).map(decodeURIComponent);
  if (segments[0] !== 'games' || !segments[1]) return { name: 'library' };

  return { name: 'game', gameID: segments[1] };
}

export function routePath(route: Route) {
  return route.name === 'library' ? '/' : `/games/${encodeURIComponent(route.gameID)}`;
}

/**
 * Shows a route. `replace` is for corrections the reader did not ask for — a stale
 * link, a disconnect — so the back button still walks their own steps.
 */
export function navigate(route: Route, options?: { replace?: boolean }) {
  const path = routePath(route);
  if (path === window.location.pathname) return;

  if (options?.replace) window.history.replaceState(null, '', path);
  else window.history.pushState(null, '', path);
  // The history API stays silent about its own calls, so listeners need telling.
  window.dispatchEvent(new Event(routeChanged));
}

/**
 * True when a click on a link should be handled here instead of by the browser. Anything
 * the reader means as "open this elsewhere" — a new tab, a download, another origin —
 * is left alone.
 */
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
