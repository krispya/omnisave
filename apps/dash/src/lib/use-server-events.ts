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
 */
export function useServerEvents({
  token,
  eventTypes,
  onRefresh,
  onStatusChange,
}: {
  token: string;
  eventTypes: string[];
  onRefresh: () => Promise<unknown>;
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
    let refreshQueued = false;
    let lastEventID = '';

    async function runRefresh() {
      if (refreshRunning || controller.signal.aborted) return;
      refreshRunning = true;
      try {
        do {
          refreshQueued = false;
          await refresh();
        } while (refreshQueued && !controller.signal.aborted);
      } finally {
        refreshRunning = false;
      }
    }

    function queueRefresh() {
      if (controller.signal.aborted) return;
      refreshQueued = true;
      if (refreshRunning || refreshTimer !== undefined) return;
      refreshTimer = window.setTimeout(() => {
        refreshTimer = undefined;
        void runRefresh();
      }, burstDelay);
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
              if (wanted.has(event.type)) queueRefresh();
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

    window.addEventListener('online', queueRefresh);
    document.addEventListener('visibilitychange', refreshWhenVisible);
    const safetyRefresh = window.setInterval(queueRefresh, safetyRefreshInterval);
    void connect();

    return () => {
      controller.abort();
      if (refreshTimer !== undefined) window.clearTimeout(refreshTimer);
      window.clearInterval(safetyRefresh);
      window.removeEventListener('online', queueRefresh);
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
