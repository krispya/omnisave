import type { Omnisave } from '../../lib/omnisave-api.js';
import { DeleteOptions } from '../../components/delete-options.js';
import { defaultSaveName, displaySaveName } from './save-name.js';
import { formatDate } from '../../lib/format.js';
import { SaveNameEditor } from './save-name-editor.js';

function ForkIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      className="size-3 shrink-0"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <circle cx="6" cy="4" r="2" />
      <circle cx="18" cy="6" r="2" />
      <circle cx="6" cy="20" r="2" />
      <path d="M6 6v12M18 8v2a4 4 0 0 1-4 4H6" />
    </svg>
  );
}

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

export function SaveList({
  saves,
  selectedSave,
  onSelectSave,
  onRequestDelete,
  onRenameSave,
}: {
  saves: Omnisave[];
  selectedSave?: Omnisave;
  onSelectSave: (save: Omnisave) => void;
  onRequestDelete: (save: Omnisave, name: string) => void;
  onRenameSave: (save: Omnisave, displayName: string) => Promise<void>;
}) {
  return (
    <div className="space-y-3">
      {saves.map((save, index) => {
        const selected = save.id === selectedSave?.id;
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
            className={`group relative rounded-md border bg-[#1a1a1a] transition hover:bg-[#202020] ${
              selected ? 'border-[#e5a00d]' : 'border-white/5'
            }`}
          >
            <button
              type="button"
              aria-pressed={selected}
              aria-label={`Select ${name}`}
              onClick={() => onSelectSave(save)}
              className="absolute inset-0 rounded-md outline-none focus-visible:ring-2 focus-visible:ring-[#e5a00d]"
            />
            <div className="pointer-events-none relative z-10 grid grid-cols-[2.25rem_minmax(0,1fr)_auto_2rem] items-center gap-4 p-3.5">
              <div className="grid size-9 shrink-0 place-items-center rounded bg-white/5 text-[#e5a00d]">
                <SaveFileIcon />
              </div>
              <div className="min-w-0">
                <div className="h-5">
                  <SaveNameEditor save={save} fallbackName={fallbackName} onSave={onRenameSave} />
                </div>
                <p className="mt-1 text-xs text-slate-500">Created {formatDate(save.created_at)}</p>
              </div>
              <div className="max-w-36 min-w-0 text-right text-[11px] text-[#e5a00d]/80">
                {sourceName ? (
                  <p
                    className="flex items-center justify-end gap-1.5"
                    title={`Forked from ${sourceName}`}
                  >
                    <ForkIcon />
                    <span className="truncate">{sourceName}</span>
                  </p>
                ) : null}
                {forkCount > 0 ? (
                  <p className="flex items-center justify-end gap-1.5">
                    <ForkIcon />
                    <span className="truncate">
                      {forkCount} {forkCount === 1 ? 'fork' : 'forks'}
                    </span>
                  </p>
                ) : null}
              </div>
              <DeleteOptions
                label={name}
                className="pointer-events-auto relative z-20 opacity-0 transition group-focus-within:opacity-100 group-hover:opacity-100 open:opacity-100"
                onDelete={() => onRequestDelete(save, name)}
              />
            </div>
          </div>
        );
      })}
    </div>
  );
}
