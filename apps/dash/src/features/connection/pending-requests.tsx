import type { PairingRequest } from '../../lib/omnisave-api.js';

type PendingRequestsProps = {
  requests: PairingRequest[];
  busyID: string;
  onApprove: (request: PairingRequest) => void;
  onDeny: (request: PairingRequest) => void;
};

/**
 * The devices asking to connect, each shown with its code.
 *
 * The code is what the owner is here to match: a request's name and identity
 * are minted by the device that sent them, and its address is worth what its
 * network path is worth, so neither separates a device from something
 * imitating it. Approving is meant to be a deliberate match against the screen
 * in the room, not a reflex (FDR-006).
 */
export function PendingRequests({ requests, busyID, onApprove, onDeny }: PendingRequestsProps) {
  return (
    <section className="rounded-lg border border-white/5 bg-[#1a1a1a] p-6">
      <h2 className="font-semibold text-white">Devices asking to connect</h2>
      <p className="mt-2 text-sm leading-6 text-slate-400">
        Approve the request whose code matches the one shown on the device. Requests expire after a
        few minutes.
      </p>

      {requests.length === 0 ? (
        <p className="mt-5 rounded-md border border-dashed border-white/10 px-4 py-6 text-center text-sm text-neutral-500">
          Nothing is waiting. Run <code className="text-neutral-300">omnisave-client connect</code> on
          a device to start.
        </p>
      ) : (
        <ul className="mt-5 flex flex-col gap-3">
          {requests.map((pairingRequest) => (
            <li
              key={pairingRequest.id}
              className="flex flex-wrap items-center gap-x-5 gap-y-3 rounded-md border border-white/10 bg-[#111111] px-4 py-3.5"
            >
              <span
                className="font-mono text-lg font-semibold tracking-[0.3em] text-[#e5a00d]"
                aria-label={`Code ${pairingRequest.code.split('').join(' ')}`}
              >
                {pairingRequest.code}
              </span>
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm text-white">
                  {pairingRequest.device_name}
                </span>
                <span className="block truncate text-xs text-neutral-500">
                  {pairingRequest.source_address}
                  {pairingRequest.platform ? ` · ${pairingRequest.platform}` : ''}
                </span>
              </span>
              <span className="flex shrink-0 gap-2">
                <button
                  type="button"
                  disabled={busyID === pairingRequest.id}
                  onClick={() => onApprove(pairingRequest)}
                  className="rounded-md bg-[#e5a00d] px-4 py-2 text-sm font-semibold text-black transition hover:bg-[#f2b51d] disabled:cursor-not-allowed disabled:opacity-40"
                >
                  Approve
                </button>
                <button
                  type="button"
                  disabled={busyID === pairingRequest.id}
                  onClick={() => onDeny(pairingRequest)}
                  className="rounded-md border border-white/10 px-4 py-2 text-sm font-medium text-neutral-300 transition hover:bg-white/5 hover:text-white disabled:cursor-not-allowed disabled:opacity-40"
                >
                  Deny
                </button>
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
