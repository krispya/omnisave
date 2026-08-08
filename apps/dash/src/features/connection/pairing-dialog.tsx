import { Button } from '../../components/button.js';
import { Dialog, DialogActions, DialogError } from '../../components/dialog.js';
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
 * The devices asking to connect.
 *
 * It opens by itself when a request arrives, because the thing on the other
 * end is a person standing at a device with a code on its screen, and the
 * request expires in minutes. The menu's "Asking to connect" item opens the
 * same dialog deliberately — which is when it can be empty, and says so
 * rather than opening onto nothing.
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
  const empty = requests.length === 0;

  return (
    <Dialog
      title={
        empty
          ? 'Asking to connect'
          : requests.length > 1
            ? 'Devices want to connect'
            : 'A device wants to connect'
      }
      description={
        empty ? undefined : 'Approve it only if this code matches the one on the device’s screen.'
      }
      busy={busy}
      onDismiss={onDismiss}
    >
      {empty ? (
        <p className="mt-4 text-sm leading-6 text-muted">
          Nothing is waiting. Run <code className="text-text">omnisave connect</code> on a device to
          start.
        </p>
      ) : null}
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

      <DialogActions>
        <Button variant="plain" disabled={busy} onClick={onDismiss}>
          {empty ? 'Close' : 'Not now'}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
