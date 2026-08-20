import { useEffect, useId, useRef, useState } from 'react';
import type { Achievement, Omnisave, Revision } from '../../lib/omnisave-api.js';
import { achievementsByRevision } from './revision-achievements.js';
import { ForkIcon } from './fork-icon.js';
import { forkLineage } from './fork-lineage.js';
import { formatBytes, formatDateTime, formatHistoryStamp } from '../../lib/format.js';
import { RevisionDetailsDialog } from './revision-details-dialog.js';
import { RevisionNameEditor } from './revision-name-editor.js';
import { buildRevisionRail } from './revision-rail.js';
import { revisionMove, revisionMoveLabel } from './revision-navigation.js';

/** A fork target to reveal; an omitted revision identifies the target's fork point. */
export type RevisionFocus = {
  saveID: string;
  revisionID?: string;
};

type RevisionLogProps = {
  save: Omnisave;
  /** The save's siblings, which is where its fork edges are read from. */
  saves: Omnisave[];
  revisions: Revision[];
  /** Unlocks recorded against this save, marked on the revision each landed on. */
  achievements: Achievement[];
  loading: boolean;
  error: string;
  labelerAvailable: boolean;
  focus?: RevisionFocus;
  onDownloadRevision: (revision: Revision) => void;
  onRenameRevision: (revision: Revision, displayName: string) => Promise<void>;
  onRunLabeler: (revision: Revision) => Promise<void>;
  onOpenSave: (save: Omnisave, revisionID?: string) => void;
  onRequestRestore: (revision: Revision) => void;
  onRequestFork: (revision: Revision) => void;
  onRequestDelete: (revision: Revision) => void;
};

/** Lane geometry, in rem: lane width, and a dot's center within its row. */
const LANE = 1;
const DOT_CENTER = 1.125;

const laneCenter = (lane: number) => `${lane * LANE + LANE / 2}rem`;

function shortID(id: string) {
  return id.slice(0, 8);
}

/** A manual name remains protected even when the game has a labeler. */
export function canRunRevisionLabeler(
  labelerAvailable: boolean,
  revision: Pick<Revision, 'name_source'>
) {
  return labelerAvailable && revision.name_source !== 'manual';
}

/** The row's actions behind one dismissible menu, matching the save card's. */
function RevisionRowMenu({
  name,
  onFork,
  onDownload,
  onRunLabeler,
  onDetails,
  onDelete,
}: {
  name: string;
  onFork: () => void;
  onDownload: () => void;
  onRunLabeler?: () => void;
  onDetails: () => void;
  onDelete?: () => void;
}) {
  const menuID = useId();

  return (
    <div className="popover-owner relative shrink-0">
      <button
        type="button"
        popoverTarget={menuID}
        className="grid size-6 cursor-pointer place-items-center rounded-sm text-muted transition duration-120 hover:bg-text/8 hover:text-text"
      >
        <span className="sr-only">Actions for revision {name}</span>
        <svg viewBox="0 0 24 24" className="size-4" aria-hidden="true">
          <circle cx="5" cy="12" r="1.75" fill="currentColor" />
          <circle cx="12" cy="12" r="1.75" fill="currentColor" />
          <circle cx="19" cy="12" r="1.75" fill="currentColor" />
        </svg>
      </button>
      <div
        id={menuID}
        popover="auto"
        className="anchored-popover w-36 rounded-md border border-outline bg-surface p-1"
      >
        <button
          type="button"
          popoverTarget={menuID}
          popoverTargetAction="hide"
          onClick={onFork}
          className="w-full cursor-pointer rounded-sm px-3 py-2 text-left text-sm text-text hover:bg-text/8"
        >
          Fork
        </button>
        <button
          type="button"
          popoverTarget={menuID}
          popoverTargetAction="hide"
          onClick={onDownload}
          className="w-full cursor-pointer rounded-sm px-3 py-2 text-left text-sm text-text hover:bg-text/8"
        >
          Download
        </button>
        {onRunLabeler ? (
          <button
            type="button"
            popoverTarget={menuID}
            popoverTargetAction="hide"
            onClick={onRunLabeler}
            className="w-full cursor-pointer rounded-sm px-3 py-2 text-left text-sm text-text hover:bg-text/8"
          >
            Run labeler
          </button>
        ) : null}
        <button
          type="button"
          popoverTarget={menuID}
          popoverTargetAction="hide"
          onClick={onDetails}
          className="w-full cursor-pointer rounded-sm px-3 py-2 text-left text-sm text-text hover:bg-text/8"
        >
          Details
        </button>
        {onDelete ? (
          <button
            type="button"
            popoverTarget={menuID}
            popoverTargetAction="hide"
            onClick={onDelete}
            className="w-full cursor-pointer rounded-sm px-3 py-2 text-left text-sm text-danger hover:bg-text/8"
          >
            Delete
          </button>
        ) : null}
      </div>
    </div>
  );
}

function TrophyIcon({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} aria-hidden="true" fill="currentColor">
      <path d="M6 4h12v2h3v3a4 4 0 0 1-4 4h-.35A6 6 0 0 1 13 16.9V19h3v2H8v-2h3v-2.1A6 6 0 0 1 7.35 13H7a4 4 0 0 1-4-4V6h3V4Zm0 4H5v1a2 2 0 0 0 1 1.73V8Zm12 2.73A2 2 0 0 0 19 9V8h-1v2.73Z" />
    </svg>
  );
}

/**
 * The achievements this revision is the first to carry. The row says where
 * they landed in the history: everything below it predates them.
 */
function AchievementMarks({ achievements }: { achievements: Achievement[] }) {
  const popoverID = useId();
  const additional = achievements.length - 1;

  return (
    <span className="group/achievements relative inline-flex shrink-0">
      <button
        type="button"
        aria-label={`${achievements.length} ${achievements.length === 1 ? 'achievement' : 'achievements'} on this revision`}
        aria-describedby={popoverID}
        className="inline-flex h-5 min-w-5 items-center justify-center gap-0.5 rounded-full bg-accent/12 px-1.5 text-[10px] font-medium text-accent outline-none transition duration-120 hover:bg-accent/20 focus-visible:ring-2 focus-visible:ring-text"
      >
        <TrophyIcon className="size-3 shrink-0" />
        {additional > 0 ? <span>+{additional}</span> : null}
      </button>
      <span
        id={popoverID}
        role="tooltip"
        className="pointer-events-none absolute top-full left-0 z-30 mt-1.5 hidden w-max max-w-64 rounded-md border border-outline bg-surface px-2.5 py-2 text-left text-xs text-text shadow-lg group-hover/achievements:block group-focus-within/achievements:block"
      >
        {achievements.map((achievement) => (
          <span key={achievement.id} className="block not-last:mb-1">
            {achievement.name}
          </span>
        ))}
      </span>
    </span>
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
          className="absolute inset-y-0 w-px bg-outline"
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
  achievements,
  loading,
  error,
  labelerAvailable,
  focus,
  onDownloadRevision,
  onRenameRevision,
  onRunLabeler,
  onOpenSave,
  onRequestRestore,
  onRequestFork,
  onRequestDelete,
}: RevisionLogProps) {
  const [detailsRevisionID, setDetailsRevisionID] = useState('');
  // Resolved from the list each render, so an open dialog follows renames and reloads.
  const detailsRevision = revisions.find((revision) => revision.id === detailsRevisionID);
  const lineage = forkLineage(save, saves);
  const marks = achievementsByRevision(achievements);
  const forkPointID = save.forked_from?.revision_id;
  const focusedID = focus?.saveID === save.id ? (focus.revisionID ?? forkPointID) : undefined;
  const focusedRow = useRef<HTMLLIElement>(null);

  useEffect(() => {
    focusedRow.current?.scrollIntoView?.({ block: 'nearest' });
  }, [focusedID, revisions]);

  // Draw newest-first history with rewind branches and fork markers.
  const rail = buildRevisionRail(save, revisions);
  const gutterStyle = { width: `${rail.laneCount * LANE}rem` };

  if (loading) {
    // Placeholders preserve row height while history loads.
    return (
      <div aria-label="Loading revisions">
        {Array.from({ length: 3 }, (_, index) => (
          <div key={index} className="flex h-9 items-center gap-2.5">
            <span className="flex shrink-0 justify-center" style={{ width: '1rem' }}>
              <span className="size-2 rounded-full bg-text/15" />
            </span>
            <span className="h-3 w-40 max-w-full animate-pulse rounded-sm bg-text/8" />
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
          className="mb-2 rounded-sm border border-danger/30 bg-danger/10 px-3 py-2.5 text-xs text-danger"
        >
          {error}
        </div>
      ) : null}

      {revisions.length === 0 ? (
        <p className="py-6 text-center text-xs text-muted">No revisions yet.</p>
      ) : (
        <ol>
          {rail.rows.map((railRow) => {
            const { node, lineUp, lineDown, through, curvesIn } = railRow;
            const revision = node.revision;
            const forkLinks = lineage.forks.get(revision.id) ?? [];
            const rowMarks = marks.get(revision.id) ?? [];
            const isCurrent = revision.id === save.current_revision_id;
            const isForkPoint = revision.id === save.forked_from?.revision_id;
            // Offer deletion only for a non-current tip with no fork dependents.
            const deletable =
              !isCurrent &&
              !isForkPoint &&
              forkLinks.length === 0 &&
              !revisions.some((candidate) => candidate.parent_id === revision.id);
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
                      className="absolute top-0 w-px bg-outline"
                      style={{ left: laneCenter(node.lane), height: `${DOT_CENTER}rem` }}
                    />
                  ) : null}
                  {lineDown ? (
                    <span
                      aria-hidden="true"
                      className="absolute bottom-0 w-px bg-outline"
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
                        className={`absolute top-0 border-b border-outline ${
                          fromLane > node.lane ? 'rounded-br border-r' : 'rounded-bl border-l'
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
                      className="absolute size-2 rounded-full border-2 border-accent bg-accent"
                      style={{ left: `calc(${laneCenter(node.lane)} - 0.25rem)`, top: '0.875rem' }}
                    />
                  ) : (
                    <button
                      type="button"
                      onClick={() => onRequestRestore(revision)}
                      title={`${moveLabel} here — make this revision current`}
                      className="group/dot absolute grid size-4 cursor-pointer place-items-center rounded-full outline-none focus-visible:ring-2 focus-visible:ring-text"
                      style={{ left: dotLeft, top: '0.625rem' }}
                    >
                      <span className="sr-only">
                        {moveLabel} to {name}
                      </span>
                      <span className="size-2 rounded-full border-2 border-text/40 bg-bg transition duration-120 group-hover/row:scale-125 group-hover/row:border-accent group-focus-visible/dot:scale-125 group-focus-visible/dot:border-accent" />
                    </button>
                  )}
                </div>
                <div
                  className={`min-w-0 rounded-sm transition-colors group-hover/row:bg-text/5 ${focused ? 'bg-text/10' : ''}`}
                >
                  <div className="flex h-9 items-center gap-2.5">
                    <div className="relative flex h-full min-w-0 flex-1 items-center gap-2.5 px-1.5">
                      {rowMarks.length > 0 ? <AchievementMarks achievements={rowMarks} /> : null}
                      <RevisionNameEditor
                        revision={revision}
                        fallbackName={shortID(revision.id)}
                        onSave={onRenameRevision}
                      />
                      {isCurrent ? (
                        <span className="shrink-0 rounded-full bg-accent px-1.5 py-0.5 text-[10px] font-medium text-bg">
                          current
                        </span>
                      ) : null}
                      {isForkPoint ? (
                        <span className="shrink-0 rounded-full border border-outline px-1.5 py-0.5 text-[10px] text-muted">
                          shared fork point
                        </span>
                      ) : null}
                      {forkLinks.map((link) => (
                        <button
                          key={link.save.id}
                          type="button"
                          onClick={() => onOpenSave(link.save)}
                          title={`Open ${link.name} — forked from this revision`}
                          className="inline-flex min-w-0 shrink-0 cursor-pointer items-center gap-1 rounded-sm px-1.5 py-0.5 text-[10px] text-muted outline-none transition duration-120 hover:bg-text/8 hover:text-text focus-visible:ring-2 focus-visible:ring-text"
                        >
                          <ForkIcon className="size-2.5 shrink-0" />
                          <span className="max-w-40 truncate">{link.name}</span>
                        </button>
                      ))}
                      <span className="ml-auto flex shrink-0 items-center gap-4">
                        <span className="text-[11px] text-muted">
                          {revision.files.length} {revision.files.length === 1 ? 'file' : 'files'} ·{' '}
                          {formatBytes(totalSize)}
                        </span>
                        <span
                          className="text-[10px] text-muted"
                          title={
                            revision.saved_at
                              ? `Saved ${formatDateTime(revision.saved_at)} · Synced ${formatDateTime(revision.created_at)}`
                              : formatDateTime(revision.created_at)
                          }
                        >
                          {formatHistoryStamp(revision.saved_at ?? revision.created_at)}
                        </span>
                      </span>
                    </div>
                    <RevisionRowMenu
                      name={name}
                      onFork={() => onRequestFork(revision)}
                      onDownload={() => onDownloadRevision(revision)}
                      onRunLabeler={
                        canRunRevisionLabeler(labelerAvailable, revision)
                          ? () => void onRunLabeler(revision)
                          : undefined
                      }
                      onDetails={() => setDetailsRevisionID(revision.id)}
                      onDelete={deletable ? () => onRequestDelete(revision) : undefined}
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
