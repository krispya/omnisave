import { useEffect, useRef, useState } from 'react';
import type { Omnisave, Revision } from '../../lib/omnisave-api.js';
import { ForkIcon } from './fork-icon.js';
import { forkLineage, type ForkLink, type ForkOrigin } from './fork-lineage.js';
import { formatBytes, formatDateTime, formatHistoryStamp } from '../../lib/format.js';
import { RevisionNameEditor } from './revision-name-editor.js';

/**
 * The revision to reveal after following a fork edge. `revisionID` is omitted when the
 * target is the save's own fork point, whose id the log linking to it cannot know.
 */
export type RevisionFocus = {
  saveID: string;
  revisionID?: string;
};

type RevisionLogProps = {
  save: Omnisave;
  /** The save's siblings, which is where its fork edges are read from. */
  saves: Omnisave[];
  revisions: Revision[];
  loading: boolean;
  error: string;
  focus?: RevisionFocus;
  onDownloadRevision: (revision: Revision) => void;
  onRenameRevision: (revision: Revision, displayName: string) => Promise<void>;
  onOpenSave: (save: Omnisave, revisionID?: string) => void;
};

/** One entry in the log: a revision of this save, or a fork edge leaving it. */
type LogRow =
  | { kind: 'revision'; revision: Revision }
  | { kind: 'fork'; link: ForkLink }
  | { kind: 'origin'; origin: ForkOrigin };

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

function ChevronIcon({ open }: { open: boolean }) {
  return (
    <svg
      viewBox="0 0 24 24"
      className={`size-3 transition ${open ? 'rotate-90' : ''}`}
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="m9 5 7 7-7 7" />
    </svg>
  );
}

/**
 * A save on the other side of a fork edge. It sits in the log where its revision would
 * be, dashed, because that revision lives in another save's history.
 */
function GhostRow({ name, detail, onOpen }: { name: string; detail: string; onOpen?: () => void }) {
  return (
    <button
      type="button"
      disabled={!onOpen}
      onClick={onOpen}
      aria-label={onOpen ? `Open ${name}` : undefined}
      title={onOpen ? `Open ${name}` : undefined}
      className="flex h-8 w-full items-center gap-2 rounded border border-dashed border-[#e5a00d]/25 bg-[#e5a00d]/[0.04] px-2 text-left transition enabled:hover:border-[#e5a00d]/50 enabled:hover:bg-[#e5a00d]/10 disabled:cursor-default disabled:opacity-60"
    >
      <span className="shrink-0 text-[#e5a00d]/70">
        <ForkIcon />
      </span>
      <span className="min-w-0 truncate text-xs text-[#e5a00d]/90">{name}</span>
      <span className="ml-auto shrink-0 truncate font-mono text-[10px] text-[#e5a00d]/50">
        {detail}
      </span>
      {onOpen ? (
        <span className="shrink-0 text-[#e5a00d]/60">
          <ChevronIcon open={false} />
        </span>
      ) : null}
    </button>
  );
}

export function RevisionLog({
  save,
  saves,
  revisions,
  loading,
  error,
  focus,
  onDownloadRevision,
  onRenameRevision,
  onOpenSave,
}: RevisionLogProps) {
  // One revision opens at a time: the log stays scannable however long the history is.
  const [openRevisionID, setOpenRevisionID] = useState('');
  const lineage = forkLineage(save, saves);
  // A fork's first revision is the one it was created with, so it is the fork point
  // other logs aim at when they link here without a revision id.
  const forkPointID = revisions.find((revision) => !revision.parent_id)?.id;
  const focusedID = focus?.saveID === save.id ? (focus.revisionID ?? forkPointID) : undefined;
  const focusedRow = useRef<HTMLLIElement>(null);

  useEffect(() => {
    focusedRow.current?.scrollIntoView?.({ block: 'nearest' });
  }, [focusedID, revisions]);

  // Newest first, with each fork branching off directly under the revision it came from
  // and the save's own origin closing out the oldest end of the history.
  const rows: LogRow[] = [];
  for (const revision of [...revisions].reverse()) {
    rows.push({ kind: 'revision', revision });
    for (const link of lineage.forks.get(revision.id) ?? []) rows.push({ kind: 'fork', link });
  }
  if (lineage.origin && revisions.length > 0) rows.push({ kind: 'origin', origin: lineage.origin });

  if (loading) {
    // Placeholders keep the row rhythm, so the history fills them in rather than
    // resizing the card a second time.
    return (
      <div aria-label="Loading revisions">
        {Array.from({ length: 3 }, (_, index) => (
          <div key={index} className="grid h-9 grid-cols-[1rem_minmax(0,1fr)] items-center gap-2.5">
            <span className="mx-auto size-2 rounded-full bg-white/10" />
            <span className="h-3 w-40 max-w-full animate-pulse rounded bg-white/5" />
          </div>
        ))}
      </div>
    );
  }

  return (
    <>
      {error ? (
        <div
          role="alert"
          className="mb-2 rounded border border-red-400/20 bg-red-400/10 px-3 py-2.5 text-xs text-red-200"
        >
          {error}
        </div>
      ) : null}

      {revisions.length === 0 ? (
        <p className="py-6 text-center text-xs text-slate-500">
          No revisions yet. Use the Debug menu to add the first one.
        </p>
      ) : (
        <ol>
          {rows.map((row, index) => {
            const last = index === rows.length - 1;
            // Branches hang off the trunk, so the next node on the trunk itself is the next
            // row that is not a fork. The last leg of a fork's history is dashed: it leads
            // out of this save and into the one it was forked from.
            const trunkTarget = rows.slice(index + 1).find((next) => next.kind !== 'fork');
            const trunkStyle =
              trunkTarget?.kind === 'origin'
                ? 'border-l border-dashed border-[#e5a00d]/25'
                : 'w-px bg-white/10';

            if (row.kind === 'fork') {
              return (
                <li
                  key={`fork-${row.link.save.id}`}
                  className="grid grid-cols-[1rem_minmax(0,1fr)] gap-2.5"
                >
                  <div className="relative flex justify-center" aria-hidden="true">
                    {trunkTarget ? <span className={`absolute inset-y-0 ${trunkStyle}`} /> : null}
                  </div>
                  <div className="relative min-w-0 pb-1.5 pl-4">
                    <span
                      aria-hidden="true"
                      className="absolute top-0 -left-[1.125rem] h-4 w-[2.125rem] rounded-bl border-b border-l border-dashed border-[#e5a00d]/30"
                    />
                    <GhostRow
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
                <li key="origin" className="grid grid-cols-[1rem_minmax(0,1fr)] gap-2.5">
                  <div className="relative flex justify-center" aria-hidden="true">
                    <span className="relative mt-3 size-2 rounded-full border border-dashed border-[#e5a00d]/50 bg-[#151515]" />
                  </div>
                  <div className="min-w-0">
                    <GhostRow
                      name={origin.name}
                      detail={`forked from ${shortID(origin.revisionID)}`}
                      onOpen={source ? () => onOpenSave(source, origin.revisionID) : undefined}
                    />
                  </div>
                </li>
              );
            }

            const revision = row.revision;
            const isHead = revision.id === save.head_revision_id;
            const focused = revision.id === focusedID;
            const open = revision.id === openRevisionID;
            const totalSize = revision.files.reduce((total, file) => total + file.artifact.size, 0);
            return (
              <li
                key={revision.id}
                ref={focused ? focusedRow : undefined}
                className="grid grid-cols-[1rem_minmax(0,1fr)] gap-2.5"
              >
                <div className="relative flex justify-center" aria-hidden="true">
                  {last ? null : (
                    <span className={`absolute top-[1.375rem] bottom-0 ${trunkStyle}`} />
                  )}
                  <span
                    className={`relative mt-3.5 size-2 rounded-full border-2 ${
                      isHead ? 'border-[#e5a00d] bg-[#e5a00d]' : 'border-neutral-600 bg-[#151515]'
                    }`}
                  />
                </div>
                <div className={`min-w-0 ${focused ? 'rounded bg-[#e5a00d]/[0.07]' : ''}`}>
                  <div className="flex h-9 items-center gap-2.5">
                    <div className="relative flex h-full min-w-0 flex-1 items-center gap-2.5 px-1.5">
                      <button
                        type="button"
                        aria-expanded={open}
                        onClick={() => setOpenRevisionID(open ? '' : revision.id)}
                        aria-label={`${open ? 'Collapse' : 'Expand'} revision ${revision.display_name || shortID(revision.id)}`}
                        className="absolute inset-0 rounded text-left outline-none transition hover:bg-white/5 focus-visible:ring-2 focus-visible:ring-[#e5a00d]"
                      />
                      <span className="pointer-events-none relative shrink-0 text-slate-600">
                        <ChevronIcon open={open} />
                      </span>
                      <RevisionNameEditor
                        revision={revision}
                        fallbackName={shortID(revision.id)}
                        onSave={onRenameRevision}
                      />
                      {isHead ? (
                        <span className="pointer-events-none relative shrink-0 rounded bg-[#e5a00d]/15 px-1.5 py-0.5 text-[10px] font-medium text-[#e5a00d]">
                          head
                        </span>
                      ) : null}
                      <span className="pointer-events-none relative ml-auto flex shrink-0 items-center gap-4">
                        <span className="text-[11px] text-slate-500">
                          {revision.files.length} {revision.files.length === 1 ? 'file' : 'files'} ·{' '}
                          {formatBytes(totalSize)}
                        </span>
                        <span
                          className="text-[10px] text-slate-600"
                          title={formatDateTime(revision.created_at)}
                        >
                          {formatHistoryStamp(revision.created_at)}
                        </span>
                      </span>
                    </div>
                    <button
                      type="button"
                      onClick={() => onDownloadRevision(revision)}
                      title="Download this revision"
                      className="shrink-0 rounded p-1 text-slate-500 transition hover:bg-white/5 hover:text-white"
                    >
                      <span className="sr-only">Download revision {shortID(revision.id)}</span>
                      <DownloadIcon />
                    </button>
                  </div>
                  {open ? (
                    <div className="mb-2 ml-1.5 rounded bg-black/25 p-2.5">
                      <p className="font-mono text-[10px] text-slate-600">
                        {revision.parent_id ? `parent → ${shortID(revision.parent_id)}` : 'root'}
                      </p>
                      <ul className="mt-2 max-h-56 space-y-2 overflow-y-auto pr-1">
                        {revision.files.map((file) => (
                          <li key={file.path} className="min-w-0">
                            <p
                              className="truncate font-mono text-[11px] text-slate-300"
                              title={file.path}
                            >
                              {file.path}
                            </p>
                            <p
                              className="truncate font-mono text-[10px] text-slate-600"
                              title={file.artifact.sha256}
                            >
                              {formatBytes(file.artifact.size)} · {file.artifact.sha256}
                            </p>
                          </li>
                        ))}
                      </ul>
                    </div>
                  ) : null}
                </div>
              </li>
            );
          })}
        </ol>
      )}
    </>
  );
}
