import type { Achievement, Omnisave, Revision } from '../../lib/omnisave-api.js';
import { OptionsMenu } from '../../components/options-menu.js';
import { defaultSaveName, displaySaveName } from './save-name.js';
import { ForkIcon } from './fork-icon.js';
import { formatDate } from '../../lib/format.js';
import { RevisionLog, type RevisionFocus } from './revision-log.js';
import { SaveNameEditor } from './save-name-editor.js';

type SaveListProps = {
  saves: Omnisave[];
  selectedSave?: Omnisave;
  /** Only one save shows its history at a time, and none does until asked. */
  expandedSaveID: string;
  /** Revisions of the expanded save; the list loads no history it does not show. */
  revisions: Revision[];
  /** Unlocks recorded against the expanded save. */
  achievements: Achievement[];
  loadingRevisions: boolean;
  revisionError: string;
  labelerAvailable: boolean;
  focus?: RevisionFocus;
  onToggleSave: (save: Omnisave) => void;
  /** Pointing at a save is enough intent to start loading it. */
  onPrefetchSave: (save: Omnisave) => void;
  onDownloadSave: (save: Omnisave, name: string) => void;
  onDownloadRevision: (save: Omnisave, name: string, revision: Revision) => void;
  onRequestDelete: (save: Omnisave, name: string) => void;
  onRenameSave: (save: Omnisave, displayName: string) => Promise<void>;
  onRenameRevision: (revision: Revision, displayName: string) => Promise<void>;
  onRunLabeler: (revision: Revision) => Promise<void>;
  onOpenSave: (save: Omnisave, revisionID?: string) => void;
  onRequestRestore: (save: Omnisave, revision: Revision) => void;
  onRequestFork: (save: Omnisave, revision: Revision) => void;
  onRequestDeleteRevision: (save: Omnisave, revision: Revision) => void;
};

function SaveFileIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      className="size-5"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M5 3.75h11.5L20.25 7.5v12.75H3.75V3.75H5Z" />
      <path d="M7.5 3.75v5.5h8v-5.5M7.25 20.25v-7h9.5v7" />
      <path d="M13.25 6.5h2.25" />
    </svg>
  );
}

function DisclosureIcon({ open }: { open: boolean }) {
  return (
    <svg
      viewBox="0 0 24 24"
      className={`size-4 transition ${open ? 'rotate-180' : ''}`}
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="m6 9 6 6 6-6" />
    </svg>
  );
}

export function SaveList({
  saves,
  selectedSave,
  expandedSaveID,
  revisions,
  achievements,
  loadingRevisions,
  revisionError,
  labelerAvailable,
  focus,
  onToggleSave,
  onPrefetchSave,
  onDownloadSave,
  onDownloadRevision,
  onRequestDelete,
  onRenameSave,
  onRenameRevision,
  onRunLabeler,
  onOpenSave,
  onRequestRestore,
  onRequestFork,
  onRequestDeleteRevision,
}: SaveListProps) {
  return (
    <div className="space-y-3">
      {saves.map((save, index) => {
        const selected = save.id === selectedSave?.id;
        const expanded = save.id === expandedSaveID;
        const fallbackName = defaultSaveName(index);
        const name = displaySaveName(save, fallbackName);
        const sourceIndex = save.forked_from
          ? saves.findIndex((candidate) => candidate.id === save.forked_from?.omnisave_id)
          : -1;
        const source = sourceIndex >= 0 ? saves[sourceIndex] : undefined;
        const sourceName = source
          ? displaySaveName(source, defaultSaveName(sourceIndex))
          : save.forked_from
            ? 'unavailable save'
            : undefined;
        const forkCount = saves.filter(
          (candidate) => candidate.forked_from?.omnisave_id === save.id
        ).length;
        return (
          <div
            key={save.id}
            className={`group rounded-lg border bg-surface transition duration-120 ${
              selected ? 'border-text/50' : 'border-outline'
            }`}
          >
            <div
              className={`relative transition duration-120 hover:bg-text/5 ${
                expanded ? 'rounded-t-lg' : 'rounded-lg'
              }`}
            >
              <button
                type="button"
                aria-expanded={expanded}
                aria-label={`${expanded ? 'Collapse' : 'Expand'} ${name}`}
                onPointerEnter={() => onPrefetchSave(save)}
                onFocus={() => onPrefetchSave(save)}
                onClick={() => onToggleSave(save)}
                className={`absolute inset-0 outline-none focus-visible:ring-2 focus-visible:ring-text ${
                  expanded ? 'rounded-t-lg' : 'rounded-lg'
                }`}
              />
              <div className="pointer-events-none relative z-10 grid grid-cols-[2.25rem_minmax(0,1fr)_auto_1rem_2rem] items-center gap-4 p-3.5">
                <div className="grid size-9 shrink-0 place-items-center rounded-sm bg-text/8 text-text">
                  <SaveFileIcon />
                </div>
                <div className="min-w-0">
                  <div className="h-5">
                    <SaveNameEditor save={save} fallbackName={fallbackName} onSave={onRenameSave} />
                  </div>
                  <p className="mt-1 text-xs text-muted">
                    {save.current_revision_id
                      ? `Current from ${formatDate(save.current_revision_saved_at ?? save.current_revision_created_at)}`
                      : 'No revisions yet'}
                  </p>
                </div>
                <div className="max-w-36 min-w-0 text-right text-[11px] text-muted">
                  {sourceName ? (
                    <p
                      className="flex items-center justify-end gap-1.5"
                      title={`Forked from ${sourceName}`}
                    >
                      <ForkIcon className="size-3 shrink-0" />
                      <span className="truncate">{sourceName}</span>
                    </p>
                  ) : null}
                  {forkCount > 0 ? (
                    <p className="flex items-center justify-end gap-1.5">
                      <ForkIcon className="size-3 shrink-0" />
                      <span className="truncate">
                        {forkCount} {forkCount === 1 ? 'fork' : 'forks'}
                      </span>
                    </p>
                  ) : null}
                </div>
                <span className={expanded ? 'text-text' : 'text-muted'}>
                  <DisclosureIcon open={expanded} />
                </span>
                <OptionsMenu
                  label={name}
                  className="pointer-events-auto relative z-20 opacity-0 transition group-focus-within:opacity-100 group-hover:opacity-100"
                  onDownload={save.current_revision_id ? () => onDownloadSave(save, name) : undefined}
                  onDelete={() => onRequestDelete(save, name)}
                />
              </div>
            </div>

            {expanded ? (
              <div className="rounded-b-lg border-t border-outline bg-bg/40 px-3.5 py-3">
                <RevisionLog
                  save={save}
                  saves={saves}
                  revisions={revisions}
                  achievements={achievements}
                  loading={loadingRevisions}
                  error={revisionError}
                  labelerAvailable={labelerAvailable}
                  focus={focus}
                  onDownloadRevision={(revision) => onDownloadRevision(save, name, revision)}
                  onRenameRevision={onRenameRevision}
                  onRunLabeler={onRunLabeler}
                  onOpenSave={onOpenSave}
                  onRequestRestore={(revision) => onRequestRestore(save, revision)}
                  onRequestFork={(revision) => onRequestFork(save, revision)}
                  onRequestDelete={(revision) => onRequestDeleteRevision(save, revision)}
                />
              </div>
            ) : null}
          </div>
        );
      })}
    </div>
  );
}
