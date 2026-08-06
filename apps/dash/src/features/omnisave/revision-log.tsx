import { useEffect, useRef, useState } from 'react';
import type { Omnisave, Revision } from '../../lib/omnisave-api.js';
import { ForkIcon } from './fork-icon.js';
import { useDismissibleDetails } from '../../lib/use-dismissible-details.js';
import { forkLineage } from './fork-lineage.js';
import { formatBytes, formatDateTime, formatHistoryStamp } from '../../lib/format.js';
import { RevisionDetailsDialog } from './revision-details-dialog.js';
import { RevisionNameEditor } from './revision-name-editor.js';
import { buildRevisionRail } from './revision-rail.js';
import { revisionMove, revisionMoveLabel } from './revision-navigation.js';

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
  onRequestRestore: (revision: Revision) => void;
  onRequestFork: (revision: Revision) => void;
};


/** Lane geometry, in rem: lane width, and a dot's center within its row. */
const LANE = 1;
const DOT_CENTER = 1.125;

const laneCenter = (lane: number) => `${lane * LANE + LANE / 2}rem`;

function shortID(id: string) {
  return id.slice(0, 8);
}

/** The row's actions behind one dismissible menu, matching the save card's. */
function RevisionRowMenu({
  name,
  onFork,
  onDownload,
  onDetails,
}: {
  name: string;
  onFork: () => void;
  onDownload: () => void;
  onDetails: () => void;
}) {
  const menu = useDismissibleDetails();
  const act = (action: () => void) => () => {
    menu.current?.removeAttribute('open');
    action();
  };

  return (
    <details ref={menu} className="relative shrink-0 open:z-30">
      <summary className="grid size-6 cursor-pointer list-none place-items-center rounded text-slate-500 transition marker:content-none hover:bg-white/5 hover:text-white">
        <span className="sr-only">Actions for revision {name}</span>
        <svg viewBox="0 0 24 24" className="size-4" aria-hidden="true">
          <circle cx="5" cy="12" r="1.75" fill="currentColor" />
          <circle cx="12" cy="12" r="1.75" fill="currentColor" />
          <circle cx="19" cy="12" r="1.75" fill="currentColor" />
        </svg>
      </summary>
      <div className="absolute top-full right-0 z-20 mt-1 w-36 rounded-md border border-white/10 bg-[#202020] p-1 shadow-xl">
        <button
          type="button"
          onClick={act(onFork)}
          className="w-full cursor-pointer rounded px-3 py-2 text-left text-sm text-neutral-200 hover:bg-white/5"
        >
          Fork
        </button>
        <button
          type="button"
          onClick={act(onDownload)}
          className="w-full cursor-pointer rounded px-3 py-2 text-left text-sm text-neutral-200 hover:bg-white/5"
        >
          Download
        </button>
        <button
          type="button"
          onClick={act(onDetails)}
          className="w-full cursor-pointer rounded px-3 py-2 text-left text-sm text-neutral-200 hover:bg-white/5"
        >
          Details
        </button>
      </div>
    </details>
  );
}

/** Vertical rail lines crossing one row, one hairline per lane. */
function RailLines({ lanes }: { lanes: number[] }) {
  return (
    <>
      {lanes.map((lane) => (
        <span
          key={lane}
          aria-hidden="true"
          className="absolute inset-y-0 w-px bg-white/10"
          style={{ left: laneCenter(lane) }}
        />
      ))}
    </>
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
  onRequestRestore,
  onRequestFork,
}: RevisionLogProps) {
  const [detailsRevisionID, setDetailsRevisionID] = useState('');
  // Resolved from the list each render, so an open dialog follows renames and reloads.
  const detailsRevision = revisions.find((revision) => revision.id === detailsRevisionID);
  const lineage = forkLineage(save, saves);
  const forkPointID = save.forked_from?.revision_id;
  const focusedID = focus?.saveID === save.id ? (focus.revisionID ?? forkPointID) : undefined;
  const focusedRow = useRef<HTMLLIElement>(null);

  useEffect(() => {
    focusedRow.current?.scrollIntoView?.({ block: 'nearest' });
  }, [focusedID, revisions]);

  // One newest-first column, git style: the trunk holds the first lane, and
  // branch lanes curve into the node they sprouted from. A fork shows as an
  // inline decoration on the shared revision it began at, like a git ref.
  const rail = buildRevisionRail(save, revisions);
  const gutterStyle = { width: `${rail.laneCount * LANE}rem` };

  if (loading) {
    // Placeholders keep the row rhythm, so the history fills them in rather than
    // resizing the card a second time.
    return (
      <div aria-label="Loading revisions">
        {Array.from({ length: 3 }, (_, index) => (
          <div key={index} className="flex h-9 items-center gap-2.5">
            <span className="flex shrink-0 justify-center" style={{ width: '1rem' }}>
              <span className="size-2 rounded-full bg-white/10" />
            </span>
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
          {rail.rows.map((railRow) => {
            const { node, lineUp, lineDown, through, curvesIn } = railRow;
            const revision = node.revision;
            const forkLinks = lineage.forks.get(revision.id) ?? [];
            const isCurrent = revision.id === save.current_revision_id;
            const isForkPoint = revision.id === save.forked_from?.revision_id;
            const focused = revision.id === focusedID;
            const name = revision.display_name || shortID(revision.id);
            const totalSize = revision.files.reduce((total, file) => total + file.artifact.size, 0);
            const moveLabel = isCurrent
              ? ''
              : revisionMoveLabel(revisionMove(revisions, save.current_revision_id, revision.id));
            const dotLeft = `calc(${laneCenter(node.lane)} - 0.5rem)`;
            return (
              <li
                key={revision.id}
                ref={focused ? focusedRow : undefined}
                className="group/row grid grid-cols-[var(--rail)_minmax(0,1fr)] gap-2.5"
                style={{ '--rail': gutterStyle.width } as React.CSSProperties}
              >
                <div className="relative">
                  <RailLines lanes={through} />
                  {lineUp ? (
                    <span
                      aria-hidden="true"
                      className="absolute top-0 w-px bg-white/10"
                      style={{ left: laneCenter(node.lane), height: `${DOT_CENTER}rem` }}
                    />
                  ) : null}
                  {lineDown ? (
                    <span
                      aria-hidden="true"
                      className="absolute bottom-0 w-px bg-white/10"
                      style={{ left: laneCenter(node.lane), top: `${DOT_CENTER}rem` }}
                    />
                  ) : null}
                  {curvesIn.map((fromLane) => {
                    const [left, right] =
                      fromLane > node.lane ? [node.lane, fromLane] : [fromLane, node.lane];
                    return (
                      <span
                        key={fromLane}
                        aria-hidden="true"
                        className={`absolute top-0 border-b border-white/10 ${
                          fromLane > node.lane
                            ? 'rounded-br border-r'
                            : 'rounded-bl border-l'
                        }`}
                        style={{
                          left: laneCenter(left),
                          width: `${(right - left) * LANE}rem`,
                          height: `${DOT_CENTER}rem`,
                        }}
                      />
                    );
                  })}
                  {isCurrent ? (
                    <span
                      title="Current revision"
                      className="absolute size-2 rounded-full border-2 border-[#e5a00d] bg-[#e5a00d]"
                      style={{ left: `calc(${laneCenter(node.lane)} - 0.25rem)`, top: '0.875rem' }}
                    />
                  ) : (
                    <button
                      type="button"
                      onClick={() => onRequestRestore(revision)}
                      title={`${moveLabel} here — make this revision current`}
                      className="group/dot absolute grid size-4 cursor-pointer place-items-center rounded-full outline-none focus-visible:ring-2 focus-visible:ring-[#e5a00d]"
                      style={{ left: dotLeft, top: '0.625rem' }}
                    >
                      <span className="sr-only">
                        {moveLabel} to {name}
                      </span>
                      <span className="size-2 rounded-full border-2 border-neutral-600 bg-[#151515] transition group-hover/row:scale-125 group-hover/row:border-[#e5a00d] group-focus-visible/dot:scale-125 group-focus-visible/dot:border-[#e5a00d]" />
                    </button>
                  )}
                </div>
                <div
                  className={`min-w-0 rounded transition-colors group-hover/row:bg-white/[0.04] ${focused ? 'bg-[#e5a00d]/[0.07]' : ''}`}
                >
                  <div className="flex h-9 items-center gap-2.5">
                    <div className="relative flex h-full min-w-0 flex-1 items-center gap-2.5 px-1.5">
                      <RevisionNameEditor
                        revision={revision}
                        fallbackName={shortID(revision.id)}
                        onSave={onRenameRevision}
                      />
                      {isCurrent ? (
                        <span className="shrink-0 rounded bg-[#e5a00d]/15 px-1.5 py-0.5 text-[10px] font-medium text-[#e5a00d]">
                          current
                        </span>
                      ) : null}
                      {isForkPoint ? (
                        <span className="shrink-0 rounded bg-white/5 px-1.5 py-0.5 text-[10px] text-neutral-400">
                          shared fork point
                        </span>
                      ) : null}
                      {forkLinks.map((link) => (
                        <button
                          key={link.save.id}
                          type="button"
                          onClick={() => onOpenSave(link.save)}
                          title={`Open ${link.name} — forked from this revision`}
                          className="inline-flex min-w-0 shrink-0 cursor-pointer items-center gap-1 rounded px-1.5 py-0.5 text-[10px] text-[#e5a00d]/70 outline-none transition hover:bg-[#e5a00d]/10 hover:text-[#e5a00d] focus-visible:ring-2 focus-visible:ring-[#e5a00d]"
                        >
                          <ForkIcon className="size-2.5 shrink-0" />
                          <span className="max-w-40 truncate">{link.name}</span>
                        </button>
                      ))}
                      <span className="ml-auto flex shrink-0 items-center gap-4">
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
                    <RevisionRowMenu
                      name={name}
                      onFork={() => onRequestFork(revision)}
                      onDownload={() => onDownloadRevision(revision)}
                      onDetails={() => setDetailsRevisionID(revision.id)}
                    />
                  </div>
                </div>
              </li>
            );
          })}
        </ol>
      )}

      {detailsRevision ? (
        <RevisionDetailsDialog
          save={save}
          revisions={revisions}
          revision={detailsRevision}
          onClose={() => setDetailsRevisionID('')}
        />
      ) : null}
    </>
  );
}
