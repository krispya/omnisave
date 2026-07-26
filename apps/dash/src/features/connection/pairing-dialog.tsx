import { useEffect, useId } from 'react';
import type { PairingRequest } from '../../lib/omnisave-api.js';

type PairingDialogProps = {
  requests: PairingRequest[];
  busyID: string;
  error: string;
  onApprove: (request: PairingRequest) => void;
  onDeny: (request: PairingRequest) => void;
  onDismiss: () => void;
};

/**
 * A device asking to connect, over whatever the owner was doing.
 *
 * It interrupts because the thing on the other end is a person standing at a
 * device with a code on its screen, and the request expires in minutes. The
 * server settings list the same requests for anyone who goes looking; this is
 * for everyone who does not know to.
 *
 * The code is what the owner matches against that screen — the name and
 * address in a request are supplied by whoever sent it (FDR-006).
 */
export function PairingDialog({
  requests,
  busyID,
  error,
  onApprove,
  onDeny,
  onDismiss,
}: PairingDialogProps) {
  const titleID = useId();
  const busy = busyID !== '';

  useEffect(() => {
    function closeOnEscape(event: KeyboardEvent) {
      if (event.key === 'Escape' && !busy) onDismiss();
    }

    window.addEventListener('keydown', closeOnEscape);
    return () => window.removeEventListener('keydown', closeOnEscape);
  }, [busy, onDismiss]);

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/80 px-5" role="presentation">
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleID}
        className="w-full max-w-md rounded-lg border border-white/10 bg-[#202020] p-6 shadow-2xl"
      >
        <h2 id={titleID} className="text-lg font-medium text-white">
          {requests.length > 1 ? 'Devices want to connect' : 'A device wants to connect'}
        </h2>
        <p className="mt-2 text-sm leading-6 text-slate-400">
          Approve it only if this code matches the one on the device&rsquo;s screen.
        </p>

        <ul className="mt-5 flex flex-col gap-3">
          {requests.map((request) => (
            <li key={request.id} className="rounded-md border border-white/10 bg-[#141414] p-4">
              <p
                className="text-center font-mono text-2xl font-semibold tracking-[0.35em] text-[#e5a00d]"
                aria-label={`Code ${request.code.split('').join(' ')}`}
              >
                {request.code}
              </p>
              <p className="mt-3 truncate text-center text-sm text-white">{request.device_name}</p>
              <p className="truncate text-center text-xs text-neutral-500">
                {request.source_address}
                {request.platform ? ` · ${request.platform}` : ''}
              </p>
              <div className="mt-4 flex gap-2">
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => onApprove(request)}
                  className="flex-1 rounded-md bg-[#e5a00d] px-4 py-2 text-sm font-semibold text-black transition hover:bg-[#f2b51d] disabled:cursor-not-allowed disabled:opacity-40"
                >
                  {busyID === request.id ? 'Approving…' : 'Approve'}
                </button>
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => onDeny(request)}
                  className="flex-1 rounded-md border border-white/10 px-4 py-2 text-sm font-medium text-neutral-300 transition hover:bg-white/5 hover:text-white disabled:cursor-not-allowed disabled:opacity-40"
                >
                  Deny
                </button>
              </div>
            </li>
          ))}
        </ul>

        {error ? <p className="mt-4 text-sm text-red-300">{error}</p> : null}

        <button
          type="button"
          disabled={busy}
          onClick={onDismiss}
          className="mt-4 w-full text-xs text-neutral-500 underline underline-offset-4 transition hover:text-neutral-300 disabled:opacity-40"
        >
          Not now
        </button>
      </section>
    </div>
  );
}
