import type { OmniSave, Revision } from './api.js';

type RevisionPanelProps = {
  save: OmniSave;
  revisions: Revision[];
  loading: boolean;
  error: string;
};

function displayName(save: OmniSave) {
  return save.metadata?.label ?? save.game_id;
}

function formatDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return 'Unknown date';

  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date);
}

function formatSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  return `${(bytes / 1024).toFixed(1)} KB`;
}

function shortID(id: string) {
  return id.slice(0, 8);
}

export function RevisionPanel({ save, revisions, loading, error }: RevisionPanelProps) {
  const latestRevisionID = revisions.at(-1)?.id;

  return (
    <aside className="rounded-lg border border-white/5 bg-[#181818] p-5 xl:sticky xl:top-6 xl:self-start">
      <div className="flex items-start justify-between gap-4 border-b border-white/5 pb-4">
        <div className="min-w-0">
          <p className="text-sm font-medium text-white">Revisions</p>
          <h2 className="mt-1 truncate text-sm text-neutral-400">{displayName(save)}</h2>
          <p className="mt-1 truncate font-mono text-xs text-slate-500">
            {save.slot} · {shortID(save.id)}
          </p>
        </div>
        <span className="shrink-0 text-xs text-neutral-500">
          {revisions.length} {revisions.length === 1 ? 'revision' : 'revisions'}
        </span>
      </div>

      {error ? (
        <div
          role="alert"
          className="mt-4 rounded-md border border-red-400/20 bg-red-400/10 px-4 py-3 text-sm text-red-200"
        >
          {error}
        </div>
      ) : null}

      {loading ? (
        <div className="mt-5 space-y-3" aria-label="Loading revisions">
          {Array.from({ length: 2 }, (_, index) => (
            <div key={index} className="h-28 animate-pulse rounded-md bg-white/5" />
          ))}
        </div>
      ) : revisions.length === 0 && !error ? (
        <div className="py-12 text-center">
          <h3 className="text-sm font-medium text-white">No revisions yet</h3>
          <p className="mt-2 text-sm leading-6 text-slate-500">
            Use the Debug menu to add the first test revision.
          </p>
        </div>
      ) : (
        <ol className="mt-1 divide-y divide-white/5">
          {[...revisions].reverse().map((revision) => {
            const parentIDs = revision.parent_ids ?? [];
            return (
              <li key={revision.id} className="py-4">
                <div className="flex items-center justify-between gap-3">
                  <div className="flex min-w-0 items-center gap-2">
                    <span
                      className="size-1.5 shrink-0 rounded-full bg-[#e5a00d]"
                      aria-hidden="true"
                    />
                    <p className="truncate font-mono text-xs text-slate-300">
                      {shortID(revision.id)}
                    </p>
                  </div>
                  {revision.id === latestRevisionID ? (
                    <span className="text-[10px] font-semibold text-[#e5a00d] uppercase">Latest</span>
                  ) : null}
                </div>

                <dl className="mt-4 grid grid-cols-2 gap-x-4 gap-y-3 text-xs">
                  <div>
                    <dt className="text-slate-600">Created</dt>
                    <dd className="mt-1 text-slate-400">{formatDate(revision.created_at)}</dd>
                  </div>
                  <div>
                    <dt className="text-slate-600">Artifact</dt>
                    <dd className="mt-1 text-slate-400">{formatSize(revision.artifact.size)}</dd>
                  </div>
                  <div className="col-span-2">
                    <dt className="text-slate-600">SHA-256</dt>
                    <dd
                      className="mt-1 truncate font-mono text-slate-400"
                      title={revision.artifact.sha256}
                    >
                      {revision.artifact.sha256}
                    </dd>
                  </div>
                  <div className="col-span-2">
                    <dt className="text-slate-600">Parent</dt>
                    <dd className="mt-1 font-mono text-slate-400">
                      {parentIDs.length > 0 ? parentIDs.map(shortID).join(', ') : 'Initial revision'}
                    </dd>
                  </div>
                </dl>
              </li>
            );
          })}
        </ol>
      )}
    </aside>
  );
}
