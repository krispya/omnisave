import { Button } from '../../components/button.js';
import { GroupNote, SettingsGroup } from '../../components/settings-group.js';
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
    <div>
      <SettingsGroup title="Asking to connect">
        {requests.length === 0 ? (
          <p className="px-4 py-6 text-center text-[13px] text-muted">
            Nothing is waiting. Run <code className="text-text">omnisave connect</code> on a device to
            start.
          </p>
        ) : (
          requests.map((pairingRequest) => (
            <div
              key={pairingRequest.id}
              className="flex flex-wrap items-center gap-x-5 gap-y-3 px-4 py-3.5"
            >
              <span
                className="font-mono text-lg font-semibold tracking-[0.3em] text-text"
                aria-label={`Code ${pairingRequest.code.split('').join(' ')}`}
              >
                {pairingRequest.code}
              </span>
              <span className="min-w-0 flex-1">
                <span className="block truncate text-[13px] text-text">
                  {pairingRequest.device_name}
                </span>
                <span className="block truncate text-xs text-muted">
                  {pairingRequest.source_address}
                  {pairingRequest.platform ? ` · ${pairingRequest.platform}` : ''}
                </span>
              </span>
              <span className="flex shrink-0 gap-2">
                <Button
                  variant="filled"
                  disabled={busyID === pairingRequest.id}
                  onClick={() => onApprove(pairingRequest)}
                >
                  Approve
                </Button>
                <Button
                  disabled={busyID === pairingRequest.id}
                  onClick={() => onDeny(pairingRequest)}
                >
                  Deny
                </Button>
              </span>
            </div>
          ))
        )}
      </SettingsGroup>
      <GroupNote>
        Approve the request whose code matches the one shown on the device. Requests expire after a
        few minutes.
      </GroupNote>
    </div>
  );
}
