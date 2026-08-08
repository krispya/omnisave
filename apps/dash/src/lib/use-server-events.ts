import { useEffect, useEffectEvent } from 'react';
import { EventStreamAuthError, streamServerEvents } from './omnisave-api.js';

export type ServerEventStatus = 'connecting' | 'live' | 'retrying' | 'unauthorized';

const burstDelay = 200;
const maximumRetryDelay = 30_000;
const safetyRefreshInterval = 5 * 60_000;

/**
 * Keeps a view current from the server's own event stream: it refreshes on the
 * event type it was asked for, and reconnects on its own when the stream drops.
 *
 * `eventTypes` is what lets one connection serve more than one watcher. The
 * shell listens for both the Library's changes and access changes, so a
 * pairing request can interrupt whatever the owner is doing without a second
 * stream open behind it.
 *
 * `onRefresh` receives the event types that provoked the refresh, so a caller
 * can answer a cheap event with a cheap fetch. An empty list means "refresh
 * everything": the safety interval, coming back online, and returning to a
 * visible tab all carry no event to blame.
 */
export function useServerEvents({
  token,
  eventTypes,
  onRefresh,
  onStatusChange,
}: {
  token: string;
  eventTypes: string[];
  onRefresh: (events: string[]) => Promise<unknown>;
  onStatusChange: (status: ServerEventStatus) => void;
}) {
  const refresh = useEffectEvent(onRefresh);
  const changeStatus = useEffectEvent(onStatusChange);
  const watched = eventTypes.join(',');

  useEffect(() => {
    const wanted = new Set(watched.split(','));
    const controller = new AbortController();
    let refreshTimer: number | undefined;
    let refreshRunning = false;
    // The event types accumulated since the last refresh; an empty set means
    // "refresh everything" and subsumes any named event that joins it.
    let queuedEvents: Set<string> | undefined;
    let lastEventID = '';

    async function runRefresh() {
      if (refreshRunning || controller.signal.aborted) return;
      refreshRunning = true;
      try {
        while (queuedEvents !== undefined && !controller.signal.aborted) {
          const events = [...queuedEvents];
          queuedEvents = undefined;
          await refresh(events);
        }
      } finally {
        refreshRunning = false;
      }
    }

    function queueRefresh(event?: string) {
      if (controller.signal.aborted) return;
      if (event === undefined) queuedEvents = new Set();
      else if (queuedEvents === undefined) queuedEvents = new Set([event]);
      else if (queuedEvents.size > 0) queuedEvents.add(event);
      if (refreshRunning || refreshTimer !== undefined) return;
      refreshTimer = window.setTimeout(() => {
        refreshTimer = undefined;
        void runRefresh();
      }, burstDelay);
    }

    function refreshEverything() {
      queueRefresh();
    }

    function refreshWhenVisible() {
      if (document.visibilityState === 'visible') queueRefresh();
    }

    async function connect() {
      let failures = 0;
      while (!controller.signal.aborted) {
        changeStatus(failures === 0 ? 'connecting' : 'retrying');
        try {
          lastEventID = await streamServerEvents(token, {
            signal: controller.signal,
            lastEventID,
            onOpen: () => {
              failures = 0;
              changeStatus('live');
            },
            onEvent: (event) => {
              if (event.id) lastEventID = event.id;
              if (wanted.has(event.type)) queueRefresh(event.type);
            },
          });
        } catch (error) {
          if (controller.signal.aborted) return;
          if (error instanceof EventStreamAuthError) {
            changeStatus('unauthorized');
            return;
          }
        }

        failures += 1;
        changeStatus('retrying');
        const baseDelay = Math.min(1000 * 2 ** Math.min(failures - 1, 5), maximumRetryDelay);
        try {
          await abortableDelay(baseDelay + Math.random() * 250, controller.signal);
        } catch {
          return;
        }
      }
    }

    window.addEventListener('online', refreshEverything);
    document.addEventListener('visibilitychange', refreshWhenVisible);
    const safetyRefresh = window.setInterval(refreshEverything, safetyRefreshInterval);
    void connect();

    return () => {
      controller.abort();
      if (refreshTimer !== undefined) window.clearTimeout(refreshTimer);
      window.clearInterval(safetyRefresh);
      window.removeEventListener('online', refreshEverything);
      document.removeEventListener('visibilitychange', refreshWhenVisible);
    };
  }, [token, watched]);
}

function abortableDelay(milliseconds: number, signal: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    const timeout = window.setTimeout(() => {
      signal.removeEventListener('abort', abort);
      resolve();
    }, milliseconds);
    function abort() {
      window.clearTimeout(timeout);
      reject(new DOMException('The event retry was aborted.', 'AbortError'));
    }
    signal.addEventListener('abort', abort, { once: true });
  });
}
