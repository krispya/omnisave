import type { Omnisave, Revision } from '../../lib/omnisave-api.js';

type RevisionPanelProps = {
  save: Omnisave;
  name: string;
  revisions: Revision[];
  loading: boolean;
  error: string;
  onDownloadRevision: (revision: Revision) => void;
};

function displayName(save: Omnisave) {
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

function DownloadIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      className="size-3.5"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M12 4v11" />
      <path d="m7 11 5 5 5-5" />
      <path d="M4.5 19.5h15" />
    </svg>
  );
}

export function RevisionPanel({
  save,
  name,
  revisions,
  loading,
  error,
  onDownloadRevision,
}: RevisionPanelProps) {
  return (
    <aside className="rounded-lg border border-white/5 bg-[#181818] p-5 xl:sticky xl:top-6 xl:self-start">
      <div className="flex items-start justify-between gap-4 border-b border-white/5 pb-4">
        <div className="min-w-0">
          <p className="text-sm font-medium text-white">Revisions</p>
          <h2 className="mt-1 truncate text-sm text-neutral-400">{displayName(save)}</h2>
          <p className="mt-1 truncate font-mono text-xs text-slate-500">
            {name} · {shortID(save.id)}
          </p>
          {save.forked_from ? (
            <p className="mt-1 truncate font-mono text-[10px] text-[#e5a00d]/70">
              forked from {shortID(save.forked_from.omnisave_id)} ·{' '}
              {shortID(save.forked_from.revision_id)}
            </p>
          ) : null}
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
        <ol className="mt-4">
          {[...revisions].reverse().map((revision, index) => {
            const isHead = revision.id === save.head_revision_id;
            const totalSize = revision.files.reduce((total, file) => total + file.artifact.size, 0);
            return (
              <li key={revision.id} className="relative grid grid-cols-[1.25rem_minmax(0,1fr)] gap-3">
                <div className="relative flex justify-center" aria-hidden="true">
                  {index < revisions.length - 1 ? (
                    <span className="absolute top-3 bottom-0 w-px bg-white/10" />
                  ) : null}
                  <span
                    className={`relative mt-2 size-2.5 rounded-full border-2 bg-[#181818] ${
                      isHead ? 'border-[#e5a00d]' : 'border-neutral-600'
                    }`}
                  />
                </div>
                <article className="min-w-0 border-b border-white/5 pb-5 last:border-0">
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex min-w-0 items-center gap-2">
                      <span className="truncate font-mono text-xs text-slate-300" title={revision.id}>
                        {shortID(revision.id)}
                      </span>
                      <button
                        type="button"
                        onClick={() => onDownloadRevision(revision)}
                        title="Download this revision"
                        className="shrink-0 rounded p-0.5 text-slate-500 transition hover:text-white"
                      >
                        <span className="sr-only">Download revision {shortID(revision.id)}</span>
                        <DownloadIcon />
                      </button>
                    </div>
                    <time className="shrink-0 text-[10px] text-slate-600">
                      {formatDate(revision.created_at)}
                    </time>
                  </div>
                  {isHead ? (
                    <span className="mt-2 inline-block rounded bg-[#e5a00d]/15 px-1.5 py-0.5 text-[10px] font-medium text-[#e5a00d]">
                      Current head
                    </span>
                  ) : null}
                  <p className="mt-3 text-xs text-slate-500">
                    {revision.files.length} {revision.files.length === 1 ? 'file' : 'files'} ·{' '}
                    {formatSize(totalSize)}
                  </p>
                  <p className="mt-2 truncate font-mono text-[10px] text-slate-600">
                    {revision.parent_id ? `parent → ${shortID(revision.parent_id)}` : 'root'}
                  </p>
                  <details className="group mt-3">
                    <summary className="cursor-pointer list-none text-xs font-medium text-slate-400 hover:text-white marker:content-none">
                      Files <span className="text-slate-600 group-open:hidden">›</span>
                      <span className="hidden text-slate-600 group-open:inline">⌄</span>
                    </summary>
                    <ul className="mt-2 space-y-2">
                      {revision.files.map((file) => (
                        <li key={file.path} className="min-w-0 rounded bg-white/[0.025] px-2.5 py-2">
                          <p
                            className="truncate font-mono text-[11px] text-slate-300"
                            title={file.path}
                          >
                            {file.path}
                          </p>
                          <p
                            className="mt-1 truncate font-mono text-[10px] text-slate-600"
                            title={file.artifact.sha256}
                          >
                            {formatSize(file.artifact.size)} · {file.artifact.sha256}
                          </p>
                        </li>
                      ))}
                    </ul>
                  </details>
                </article>
              </li>
            );
          })}
        </ol>
      )}
    </aside>
  );
}
