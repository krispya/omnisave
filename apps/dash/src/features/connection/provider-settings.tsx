import type { OwnerSetting } from '../../lib/omnisave-api.js';

type ProviderSettingsProps = {
  clientID?: OwnerSetting;
  clientSecret?: OwnerSetting;
  busy: boolean;
  onConnect: () => void;
  onDisconnect: () => void;
};

/**
 * The metadata providers this server can use, and what each one needs.
 *
 * IGDB needs credentials from the owner's own Twitch account (ADR-011), so it
 * reads as a thing to connect rather than as a pair of fields — connected or
 * not is the state worth seeing at a glance, and the credentials themselves
 * are a detail behind one click.
 */
export function ProviderSettings({
  clientID,
  clientSecret,
  busy,
  onConnect,
  onDisconnect,
}: ProviderSettingsProps) {
  const pinned = clientID?.editable === false || clientSecret?.editable === false;
  const connected = Boolean(clientID?.configured && clientSecret?.configured);

  return (
    <section className="rounded-lg border border-white/5 bg-[#1a1a1a] p-6">
      <h2 className="font-semibold text-white">Game metadata</h2>
      <p className="mt-2 text-sm leading-6 text-slate-400">
        Where titles, platforms, and cover art come from when a game is added to your library.
      </p>

      <div className="mt-5 flex flex-wrap items-center gap-x-4 gap-y-3 rounded-md border border-white/10 bg-[#111111] px-4 py-3.5">
        <span className="min-w-0 flex-1">
          <span className="flex items-center gap-2">
            <span
              className={`size-1.5 rounded-full ${connected ? 'bg-[#e5a00d]' : 'bg-neutral-600'}`}
              aria-hidden="true"
            />
            <span className="text-sm text-white">IGDB</span>
          </span>
          <span className="mt-0.5 block truncate text-xs text-neutral-500">
            {pinned
              ? `Set by the deployment (${clientID?.env_var})`
              : connected
                ? `Connected · client ${clientID?.text}`
                : 'Not connected · titles and art come from Hasheous alone'}
          </span>
        </span>

        {pinned ? null : connected ? (
          <span className="flex shrink-0 gap-2">
            <button
              type="button"
              disabled={busy}
              onClick={onConnect}
              className="rounded-md border border-white/10 px-3.5 py-1.5 text-xs font-medium text-neutral-300 transition hover:bg-white/5 hover:text-white disabled:cursor-not-allowed disabled:opacity-40"
            >
              Edit
            </button>
            <button
              type="button"
              disabled={busy}
              onClick={onDisconnect}
              className="rounded-md border border-white/10 px-3.5 py-1.5 text-xs font-medium text-neutral-300 transition hover:border-red-400/40 hover:text-red-300 disabled:cursor-not-allowed disabled:opacity-40"
            >
              Disconnect
            </button>
          </span>
        ) : (
          <button
            type="button"
            disabled={busy}
            onClick={onConnect}
            className="shrink-0 rounded-md bg-[#e5a00d] px-4 py-2 text-sm font-semibold text-black transition hover:bg-[#f2b51d] disabled:cursor-not-allowed disabled:opacity-40"
          >
            Connect
          </button>
        )}
      </div>
    </section>
  );
}
