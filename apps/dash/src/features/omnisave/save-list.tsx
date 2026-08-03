import type { Omnisave, Revision } from '../../lib/omnisave-api.js';
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
  loadingRevisions: boolean;
  revisionError: string;
  focus?: RevisionFocus;
  onToggleSave: (save: Omnisave) => void;
  /** Pointing at a save is enough intent to start loading it. */
  onPrefetchSave: (save: Omnisave) => void;
  onDownloadSave: (save: Omnisave, name: string) => void;
  onDownloadRevision: (save: Omnisave, name: string, revision: Revision) => void;
  onRequestDelete: (save: Omnisave, name: string) => void;
  onRenameSave: (save: Omnisave, displayName: string) => Promise<void>;
  onRenameRevision: (revision: Revision, displayName: string) => Promise<void>;
  onOpenSave: (save: Omnisave, revisionID?: string) => void;
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
  loadingRevisions,
  revisionError,
  focus,
  onToggleSave,
  onPrefetchSave,
  onDownloadSave,
  onDownloadRevision,
  onRequestDelete,
  onRenameSave,
  onRenameRevision,
  onOpenSave,
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
            // Never clip the card: the options menu opens upward, past its top edge.
            className={`group rounded-md border bg-[#1a1a1a] transition ${
              selected ? 'border-[#e5a00d]' : 'border-white/5'
            }`}
          >
            <div
              className={`relative transition hover:bg-[#202020] ${
                expanded ? 'rounded-t-md' : 'rounded-md'
              }`}
            >
              <button
                type="button"
                aria-expanded={expanded}
                aria-label={`${expanded ? 'Collapse' : 'Expand'} ${name}`}
                onPointerEnter={() => onPrefetchSave(save)}
                onFocus={() => onPrefetchSave(save)}
                onClick={() => onToggleSave(save)}
                className={`absolute inset-0 outline-none focus-visible:ring-2 focus-visible:ring-[#e5a00d] ${
                  expanded ? 'rounded-t-md' : 'rounded-md'
                }`}
              />
              <div className="pointer-events-none relative z-10 grid grid-cols-[2.25rem_minmax(0,1fr)_auto_1rem_2rem] items-center gap-4 p-3.5">
                <div className="grid size-9 shrink-0 place-items-center rounded bg-white/5 text-[#e5a00d]">
                  <SaveFileIcon />
                </div>
                <div className="min-w-0">
                  <div className="h-5">
                    <SaveNameEditor save={save} fallbackName={fallbackName} onSave={onRenameSave} />
                  </div>
                  <p className="mt-1 text-xs text-slate-500">Updated {formatDate(save.updated_at)}</p>
                </div>
                <div className="max-w-36 min-w-0 text-right text-[11px] text-[#e5a00d]/80">
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
                <span className={expanded ? 'text-[#e5a00d]' : 'text-slate-500'}>
                  <DisclosureIcon open={expanded} />
                </span>
                <OptionsMenu
                  label={name}
                  className="pointer-events-auto relative z-20 opacity-0 transition group-focus-within:opacity-100 group-hover:opacity-100 open:opacity-100"
                  onDownload={save.head_revision_id ? () => onDownloadSave(save, name) : undefined}
                  onDelete={() => onRequestDelete(save, name)}
                />
              </div>
            </div>

            {expanded ? (
              <div className="rounded-b-md border-t border-white/5 bg-[#151515] px-3.5 py-3">
                <RevisionLog
                  save={save}
                  saves={saves}
                  revisions={revisions}
                  loading={loadingRevisions}
                  error={revisionError}
                  focus={focus}
                  onDownloadRevision={(revision) => onDownloadRevision(save, name, revision)}
                  onRenameRevision={onRenameRevision}
                  onOpenSave={onOpenSave}
                />
              </div>
            ) : null}
          </div>
        );
      })}
    </div>
  );
}
