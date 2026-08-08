import { Button } from '../../components/button.js';
import { Dialog, DialogError } from '../../components/dialog.js';
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
  const busy = busyID !== '';

  return (
    <Dialog
      title={requests.length > 1 ? 'Devices want to connect' : 'A device wants to connect'}
      description="Approve it only if this code matches the one on the device’s screen."
      busy={busy}
      onDismiss={onDismiss}
    >
      <ul className="mt-5 flex flex-col gap-3">
        {requests.map((request) => (
          <li key={request.id} className="rounded-md border border-outline p-4">
            <p
              className="text-center font-mono text-2xl font-semibold tracking-[0.35em] text-text"
              aria-label={`Code ${request.code.split('').join(' ')}`}
            >
              {request.code}
            </p>
            <p className="mt-3 truncate text-center text-sm text-text">{request.device_name}</p>
            <p className="truncate text-center text-xs text-muted">
              {request.source_address}
              {request.platform ? ` · ${request.platform}` : ''}
            </p>
            <div className="mt-4 flex gap-2">
              <Button
                variant="filled"
                className="flex-1"
                disabled={busy}
                onClick={() => onApprove(request)}
              >
                {busyID === request.id ? 'Approving…' : 'Approve'}
              </Button>
              <Button className="flex-1" disabled={busy} onClick={() => onDeny(request)}>
                Deny
              </Button>
            </div>
          </li>
        ))}
      </ul>

      {error ? <DialogError>{error}</DialogError> : null}

      <button
        type="button"
        disabled={busy}
        onClick={onDismiss}
        className="mt-4 w-full text-xs text-muted underline underline-offset-4 transition duration-120 hover:text-text disabled:opacity-40"
      >
        Not now
      </button>
    </Dialog>
  );
}
