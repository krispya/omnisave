import { formatDate, formatRelativeDate } from '../../lib/format.js';
import type { Credential } from '../../lib/omnisave-api.js';

type CredentialListProps = {
  credentials: Credential[];
  currentID: string;
  busyID: string;
  onRevoke: (credential: Credential) => void;
};

/**
 * What holds a credential, and when each was last used.
 *
 * Revoking one withdraws that credential and nothing else: every other device
 * keeps working, which is the whole reason they are issued separately (ADR-007).
 * Revoked credentials stay on the list — a record of what was withdrawn is
 * more useful than a shorter list.
 */
export function CredentialList({ credentials, currentID, busyID, onRevoke }: CredentialListProps) {
  return (
    <section className="rounded-lg border border-white/5 bg-[#1a1a1a] p-6">
      <h2 className="font-semibold text-white">Connected devices</h2>
      <p className="mt-2 text-sm leading-6 text-slate-400">
        Every credential this server has issued. Revoking one leaves the others working; that device
        connects again by pairing again.
      </p>

      {credentials.length === 0 ? (
        <p className="mt-5 rounded-md border border-dashed border-white/10 px-4 py-6 text-center text-sm text-neutral-500">
          Nothing holds a credential yet.
        </p>
      ) : (
        <ul className="mt-5 flex flex-col gap-2">
          {credentials.map((credential) => {
            const revoked = Boolean(credential.revoked_at);
            return (
              <li
                key={credential.id}
                className={`flex flex-wrap items-center gap-x-4 gap-y-2 rounded-md border border-white/10 px-4 py-3 ${
                  revoked ? 'bg-transparent opacity-50' : 'bg-[#111111]'
                }`}
              >
                <span className="min-w-0 flex-1">
                  <span className="flex items-center gap-2">
                    <span className="truncate text-sm text-white">{credential.device_name}</span>
                    {credential.kind === 'dash' ? (
                      <span className="rounded border border-white/10 px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-neutral-400">
                        Browser
                      </span>
                    ) : null}
                    {credential.id === currentID ? (
                      <span className="rounded border border-[#e5a00d]/40 px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-[#e5a00d]">
                        This browser
                      </span>
                    ) : null}
                  </span>
                  <span className="block truncate text-xs text-neutral-500">
                    {revoked
                      ? `Revoked ${formatRelativeDate(credential.revoked_at ?? '')}`
                      : credential.last_used_at
                        ? `Last used ${formatRelativeDate(credential.last_used_at)}`
                        : `Issued ${formatDate(credential.created_at)}, not used yet`}
                  </span>
                </span>
                {revoked ? null : (
                  <button
                    type="button"
                    disabled={busyID === credential.id}
                    onClick={() => onRevoke(credential)}
                    className="shrink-0 rounded-md border border-white/10 px-3.5 py-1.5 text-xs font-medium text-neutral-300 transition hover:border-red-400/40 hover:text-red-300 disabled:cursor-not-allowed disabled:opacity-40"
                  >
                    Revoke
                  </button>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
