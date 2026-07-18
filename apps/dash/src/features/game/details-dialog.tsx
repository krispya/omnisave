import { useEffect, useId, type ReactNode } from 'react';
import type { GameIdentifier } from '../../lib/omnisave-api.js';
import type { GameSummary } from './game-summary.js';
import { formatBytes, formatDate } from '../../lib/format.js';

// Known identifier namespaces and metadata sources, mapped to the external
// app they reference. Unknown namespaces fall back to their raw name.
const externalApps: Record<string, { label: string; url?: (value: string) => string }> = {
  'igdb.game': { label: 'IGDB' },
  'hasheous.game': { label: 'Hasheous' },
  'steam.app': { label: 'Steam', url: (value) => `https://store.steampowered.com/app/${value}` },
  igdb: { label: 'IGDB' },
  hasheous: { label: 'Hasheous' },
  steam: { label: 'Steam' },
};

function appLabel(name: string) {
  return externalApps[name]?.label ?? name;
}

function IdentifierBadge({ identifier }: { identifier: GameIdentifier }) {
  const app = externalApps[identifier.namespace];
  const url = app?.url?.(identifier.value);
  const content = (
    <>
      <span className="text-slate-500">{app?.label ?? identifier.namespace}</span>
      <span className="font-mono text-xs text-neutral-200">{identifier.value}</span>
    </>
  );

  return url ? (
    <a
      href={url}
      target="_blank"
      rel="noreferrer"
      className="inline-flex items-center gap-1.5 rounded bg-white/5 px-2 py-1 transition hover:bg-white/10"
    >
      {content}
      <span className="text-[10px] text-slate-500" aria-hidden="true">
        ↗
      </span>
    </a>
  ) : (
    <span className="inline-flex items-center gap-1.5 rounded bg-white/5 px-2 py-1">{content}</span>
  );
}

function DetailRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-slate-500">{label}</dt>
      <dd className="min-w-0 text-neutral-300">{children}</dd>
    </>
  );
}

// A debug-style dump of everything the dash knows about the game.
export function GameDetailsDialog({ game, onClose }: { game: GameSummary; onClose: () => void }) {
  const titleID = useId();
  const metadataEntries = Object.entries(game.metadata ?? {}).sort(([left], [right]) =>
    left.localeCompare(right)
  );

  useEffect(() => {
    function closeOnEscape(event: KeyboardEvent) {
      if (event.key === 'Escape') onClose();
    }

    window.addEventListener('keydown', closeOnEscape);
    return () => window.removeEventListener('keydown', closeOnEscape);
  }, [onClose]);

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/80 px-5" role="presentation">
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleID}
        className="max-h-[85vh] w-full max-w-xl overflow-y-auto rounded-lg border border-white/10 bg-[#202020] p-6 shadow-2xl"
      >
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0">
            <h2 id={titleID} className="truncate text-lg font-medium text-white">
              {game.label}
            </h2>
          </div>
          <button
            type="button"
            onClick={onClose}
            autoFocus
            aria-label="Close details"
            className="-mt-1 -mr-1 rounded-md p-2 text-neutral-400 transition hover:bg-white/5 hover:text-white"
          >
            ✕
          </button>
        </div>
        <dl className="mt-5 grid grid-cols-1 gap-x-8 gap-y-3 text-sm sm:grid-cols-[8rem_minmax(0,1fr)]">
          <DetailRow label="Game ID">
            <span className="font-mono text-xs break-all">{game.id}</span>
          </DetailRow>
          <DetailRow label="Sort title">{game.sortKey}</DetailRow>
          {game.platform ? <DetailRow label="Platform">{game.platform}</DetailRow> : null}
          {game.platformCompany ? (
            <DetailRow label="Company">{game.platformCompany}</DetailRow>
          ) : null}
          {game.publisher ? <DetailRow label="Publisher">{game.publisher}</DetailRow> : null}
          {game.metadataSource ? (
            <DetailRow label="Source">{appLabel(game.metadataSource)}</DetailRow>
          ) : null}
          {game.refreshedAt ? (
            <DetailRow label="Refreshed">{formatDate(game.refreshedAt)}</DetailRow>
          ) : null}
          {game.identifiers.length > 0 ? (
            <DetailRow label="Identifiers">
              <span className="flex flex-wrap gap-2">
                {game.identifiers.map((identifier) => (
                  <IdentifierBadge
                    key={`${identifier.namespace}:${identifier.value}`}
                    identifier={identifier}
                  />
                ))}
              </span>
            </DetailRow>
          ) : null}
          {game.fingerprints.length > 0 ? (
            <DetailRow label="Fingerprints">
              <span className="grid gap-1.5">
                {game.fingerprints.map((fingerprint) => (
                  <span
                    key={`${fingerprint.platform}:${fingerprint.algorithm}:${fingerprint.value}`}
                    className="flex flex-wrap gap-x-2 text-xs"
                  >
                    <span className="shrink-0 text-slate-500">
                      {fingerprint.platform} · {fingerprint.algorithm}
                    </span>
                    <span className="min-w-0 font-mono break-all text-neutral-200">
                      {fingerprint.value}
                    </span>
                  </span>
                ))}
              </span>
            </DetailRow>
          ) : null}
          {game.media.length > 0 ? (
            <DetailRow label="Media">
              <span className="grid gap-1.5">
                {game.media.map((media) => (
                  <span key={media.id} className="flex flex-wrap gap-x-2 text-xs">
                    <span className="text-neutral-200">{media.kind}</span>
                    <span className="text-slate-500">
                      {media.format} · {formatBytes(media.size)} · #{media.position}
                      {media.attribution ? ` · ${media.attribution}` : ''}
                    </span>
                  </span>
                ))}
              </span>
            </DetailRow>
          ) : null}
          {metadataEntries.length > 0 ? (
            <DetailRow label="Metadata">
              <span className="grid gap-1.5">
                {metadataEntries.map(([key, value]) => (
                  <span key={key} className="flex flex-wrap gap-x-2 text-xs">
                    <span className="shrink-0 text-slate-500">{key}</span>
                    <span className="min-w-0 font-mono break-all text-neutral-200">
                      {typeof value === 'string' ? value : JSON.stringify(value)}
                    </span>
                  </span>
                ))}
              </span>
            </DetailRow>
          ) : null}
        </dl>
      </section>
    </div>
  );
}
