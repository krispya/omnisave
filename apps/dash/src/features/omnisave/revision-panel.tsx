import { useEffect, useRef } from 'react';
import type { Omnisave, Revision } from '../../lib/omnisave-api.js';
import { ForkIcon } from './fork-icon.js';
import { forkLineage, type ForkLink, type ForkOrigin } from './fork-lineage.js';

/**
 * The revision to reveal after following a fork edge. `revisionID` is omitted when the
 * target is the save's own fork point, whose id the panel linking to it cannot know.
 */
export type RevisionFocus = {
  saveID: string;
  revisionID?: string;
};

type RevisionPanelProps = {
  save: Omnisave;
  name: string;
  /** The save's siblings, which is where its fork edges are read from. */
  saves: Omnisave[];
  revisions: Revision[];
  loading: boolean;
  error: string;
  focus?: RevisionFocus;
  onDownloadRevision: (revision: Revision) => void;
  onOpenSave: (save: Omnisave, revisionID?: string) => void;
};

/** One entry on the timeline: a revision of this save, or a fork edge leaving it. */
type TimelineRow =
  | { kind: 'revision'; revision: Revision }
  | { kind: 'fork'; link: ForkLink }
  | { kind: 'origin'; origin: ForkOrigin };

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

/**
 * A save on the other side of a fork edge. It is drawn like a revision node but dashed,
 * because the revision it stands for lives in another save's history.
 */
function GhostNode({
  label,
  name,
  detail,
  onOpen,
}: {
  label: string;
  name: string;
  detail: string;
  onOpen?: () => void;
}) {
  return (
    <button
      type="button"
      disabled={!onOpen}
      onClick={onOpen}
      aria-label={onOpen ? `Open ${name}` : undefined}
      title={onOpen ? `Open ${name}` : undefined}
      className="flex w-full items-center gap-2.5 rounded-md border border-dashed border-[#e5a00d]/25 bg-[#e5a00d]/[0.04] px-2.5 py-2 text-left transition enabled:hover:border-[#e5a00d]/50 enabled:hover:bg-[#e5a00d]/10 disabled:cursor-default disabled:opacity-60"
    >
      <span className="shrink-0 text-[#e5a00d]/70">
        <ForkIcon className="size-3.5" />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-xs text-[#e5a00d]/90">{name}</span>
        <span className="mt-0.5 block truncate font-mono text-[10px] text-[#e5a00d]/50">
          {label} · {detail}
        </span>
      </span>
      {onOpen ? (
        <span className="shrink-0 text-xs text-[#e5a00d]/60" aria-hidden="true">
          ›
        </span>
      ) : null}
    </button>
  );
}

export function RevisionPanel({
  save,
  name,
  saves,
  revisions,
  loading,
  error,
  focus,
  onDownloadRevision,
  onOpenSave,
}: RevisionPanelProps) {
  const lineage = forkLineage(save, saves);
  // A fork's first revision is the one it was created with, so it is the fork point
  // other panels aim at when they link here without a revision id.
  const forkPointID = revisions.find((revision) => !revision.parent_id)?.id;
  const focusedID = focus?.saveID === save.id ? (focus.revisionID ?? forkPointID) : undefined;
  const focusedRow = useRef<HTMLLIElement>(null);

  useEffect(() => {
    focusedRow.current?.scrollIntoView?.({ block: 'nearest' });
  }, [focusedID, revisions]);

  // Newest first, with each fork branching off directly under the revision it came from
  // and the save's own origin closing out the oldest end of the history.
  const rows: TimelineRow[] = [];
  for (const revision of [...revisions].reverse()) {
    rows.push({ kind: 'revision', revision });
    for (const link of lineage.forks.get(revision.id) ?? []) rows.push({ kind: 'fork', link });
  }
  if (lineage.origin && revisions.length > 0) rows.push({ kind: 'origin', origin: lineage.origin });

  return (
    <aside className="rounded-lg border border-white/5 bg-[#181818] p-5 xl:sticky xl:top-6 xl:self-start">
      <div className="flex items-start justify-between gap-4 border-b border-white/5 pb-4">
        <div className="min-w-0">
          <p className="text-sm font-medium text-white">Revisions</p>
          <h2 className="mt-1 truncate text-sm text-neutral-400">{displayName(save)}</h2>
          <p className="mt-1 truncate font-mono text-xs text-slate-500">
            {name} · {shortID(save.id)}
          </p>
          {lineage.origin ? (
            <button
              type="button"
              disabled={!lineage.origin.save}
              onClick={() => {
                const origin = lineage.origin;
                if (origin?.save) onOpenSave(origin.save, origin.revisionID);
              }}
              className="mt-1 flex max-w-full items-center gap-1.5 font-mono text-[10px] text-[#e5a00d]/70 transition enabled:hover:text-[#e5a00d] disabled:cursor-default"
            >
              <ForkIcon />
              <span className="truncate">
                forked from {lineage.origin.name} · {shortID(lineage.origin.revisionID)}
              </span>
            </button>
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
          {rows.map((row, index) => {
            const last = index === rows.length - 1;
            const separator = last ? '' : 'border-b border-white/5';
            // Branches hang off the trunk, so the next node on the trunk itself is the
            // next row that is not a fork. The last leg of a fork's history is dashed:
            // it leads out of this save and into the one it was forked from.
            const trunkTarget = rows.slice(index + 1).find((next) => next.kind !== 'fork');
            const trunkStyle =
              trunkTarget?.kind === 'origin'
                ? 'border-l border-dashed border-[#e5a00d]/25'
                : 'w-px bg-white/10';

            if (row.kind === 'fork') {
              return (
                <li
                  key={`fork-${row.link.save.id}`}
                  className="grid grid-cols-[1.25rem_minmax(0,1fr)] gap-3"
                >
                  <div className="relative flex justify-center" aria-hidden="true">
                    {trunkTarget ? <span className={`absolute inset-y-0 ${trunkStyle}`} /> : null}
                  </div>
                  <div className={`relative min-w-0 pb-5 pl-6 ${separator}`}>
                    <span
                      aria-hidden="true"
                      className="absolute top-0 -left-[1.375rem] h-5 w-[2.875rem] rounded-bl-md border-b border-l border-dashed border-[#e5a00d]/30"
                    />
                    <GhostNode
                      label="fork"
                      name={row.link.name}
                      detail={shortID(row.link.save.id)}
                      onOpen={() => onOpenSave(row.link.save)}
                    />
                  </div>
                </li>
              );
            }

            if (row.kind === 'origin') {
              const origin = row.origin;
              const source = origin.save;
              return (
                <li key="origin" className="grid grid-cols-[1.25rem_minmax(0,1fr)] gap-3">
                  <div className="relative flex justify-center" aria-hidden="true">
                    <span className="relative mt-2 size-2.5 rounded-full border-2 border-dashed border-[#e5a00d]/40 bg-[#181818]" />
                  </div>
                  <div className={`min-w-0 pb-5 ${separator}`}>
                    <GhostNode
                      label="forked from"
                      name={origin.name}
                      detail={shortID(origin.revisionID)}
                      onOpen={source ? () => onOpenSave(source, origin.revisionID) : undefined}
                    />
                  </div>
                </li>
              );
            }

            const revision = row.revision;
            const isHead = revision.id === save.head_revision_id;
            const focused = revision.id === focusedID;
            const totalSize = revision.files.reduce((total, file) => total + file.artifact.size, 0);
            return (
              <li
                key={revision.id}
                ref={focused ? focusedRow : undefined}
                className="relative grid grid-cols-[1.25rem_minmax(0,1fr)] gap-3"
              >
                <div className="relative flex justify-center" aria-hidden="true">
                  {last ? null : <span className={`absolute top-3 bottom-0 ${trunkStyle}`} />}
                  <span
                    className={`relative mt-2 size-2.5 rounded-full border-2 bg-[#181818] ${
                      isHead ? 'border-[#e5a00d]' : 'border-neutral-600'
                    }`}
                  />
                </div>
                <article
                  className={`min-w-0 pb-5 ${separator} ${
                    focused
                      ? '-mx-2 rounded-md bg-[#e5a00d]/[0.06] px-2 pt-2 ring-1 ring-[#e5a00d]/25'
                      : ''
                  }`}
                >
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
