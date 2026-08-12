import { Button } from '../../components/button.js';
import { SettingRow } from '../../components/setting-row.js';
import { GroupNote, SettingsGroup } from '../../components/settings-group.js';
import { formatDate, formatRelativeDate } from '../../lib/format.js';
import type { Credential } from '../../lib/omnisave-api.js';

type CredentialListProps = {
  credentials: Credential[];
  currentID: string;
  busyID: string;
  onRevoke: (credential: Credential) => void;
};

function Badge({ children }: { children: string }) {
  return (
    <span className="shrink-0 rounded-full border border-outline px-2 py-0.5 text-[10px] tracking-wide text-muted uppercase">
      {children}
    </span>
  );
}

/** Lists active and revoked credentials with their most recent use. */
export function CredentialList({ credentials, currentID, busyID, onRevoke }: CredentialListProps) {
  return (
    <div>
      <SettingsGroup title="Connected devices">
        {credentials.length === 0 ? (
          <p className="px-4 py-6 text-center text-[13px] text-muted">
            Nothing holds a credential yet.
          </p>
        ) : (
          credentials.map((credential) => {
            const revoked = Boolean(credential.revoked_at);
            return (
              <div key={credential.id} className={revoked ? 'opacity-50' : undefined}>
                <SettingRow
                  icon="devices"
                  title={
                    <span className="flex items-center gap-2">
                      <span className="truncate">{credential.device_name}</span>
                      {credential.kind === 'dash' ? <Badge>Browser</Badge> : null}
                      {credential.id === currentID ? <Badge>This browser</Badge> : null}
                    </span>
                  }
                  subtitle={
                    revoked
                      ? `Revoked ${formatRelativeDate(credential.revoked_at ?? '')}`
                      : credential.last_used_at
                        ? `Last used ${formatRelativeDate(credential.last_used_at)}`
                        : `Issued ${formatDate(credential.created_at)}, not used yet`
                  }
                  trailing={
                    revoked ? null : (
                      <Button
                        variant="danger"
                        disabled={busyID === credential.id}
                        onClick={() => onRevoke(credential)}
                      >
                        Revoke
                      </Button>
                    )
                  }
                />
              </div>
            );
          })
        )}
      </SettingsGroup>
      <GroupNote>
        Every credential this server has issued. Revoking one leaves the others working; that device
        connects again by pairing again.
      </GroupNote>
    </div>
  );
}
