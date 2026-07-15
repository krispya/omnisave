import type { OmniSave, Revision } from '../../lib/omnisave-api.js';
import { DeleteOptions } from './delete-options.js';
import { GameArtwork, type GameSummary } from './game-library.js';
import { RevisionPanel } from './revision-panel.js';
import { SlotNameEditor } from './slot-name-editor.js';
import { displaySlotName } from './slot-name.js';

function formatDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return 'Unknown date';
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(date);
}

type GameDetailProps = {
  game: GameSummary;
  selectedSave?: OmniSave;
  revisions: Revision[];
  loadingRevisions: boolean;
  revisionError: string;
  onSelectSave: (save: OmniSave) => void;
  onRequestDelete: (save: OmniSave) => void;
  onRenameSave: (save: OmniSave, displayName: string) => Promise<void>;
};

export function GameDetail({
  game,
  selectedSave,
  revisions,
  loadingRevisions,
  revisionError,
  onSelectSave,
  onRequestDelete,
  onRenameSave,
}: GameDetailProps) {
  return (
    <div className="mt-8">
      <section className="flex items-end gap-5 border-b border-white/5 pb-6">
        <GameArtwork game={game} className="aspect-[3/4] w-20 shrink-0 shadow-lg sm:w-28" />
        <div className="min-w-0 pb-1">
          {game.platform ? (
            <p className="text-xs font-medium text-[#e5a00d]">{game.platform}</p>
          ) : null}
          <h2 className="mt-1.5 text-2xl font-medium tracking-tight text-white">{game.label}</h2>
          <p className="mt-2 truncate font-mono text-xs text-slate-500">{game.id}</p>
          <p className="mt-3 text-sm text-slate-400">
            {game.saves.length} {game.saves.length === 1 ? 'save' : 'saves'}
          </p>
        </div>
      </section>

      <div className="mt-6 grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(22rem,0.8fr)]">
        <section aria-label={`Saves for ${game.label}`}>
          <div className="mb-4 flex items-center justify-between">
            <h3 className="text-sm font-semibold text-white">Saves</h3>
            <span className="text-xs text-slate-500">Select a save to view its history</span>
          </div>
          <div className="space-y-3">
            {game.saves.map((save, index) => {
              const selected = save.id === selectedSave?.id;
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
                    aria-label={`Select ${displaySlotName(save)}`}
                    onClick={() => onSelectSave(save)}
                    className="absolute inset-0 rounded-md outline-none focus-visible:ring-2 focus-visible:ring-[#e5a00d]"
                  />
                  <div className="pointer-events-none relative z-10 grid grid-cols-[2.25rem_minmax(0,1fr)_2rem] items-center gap-4 p-3.5">
                    <div className="grid size-9 shrink-0 place-items-center rounded bg-white/5 text-sm font-semibold text-[#e5a00d]">
                      {index + 1}
                    </div>
                    <div className="min-w-0">
                      <div className="h-5">
                        <SlotNameEditor save={save} onSave={onRenameSave} />
                      </div>
                      <p className="mt-1 text-xs text-slate-500">
                        Created {formatDate(save.created_at)}
                      </p>
                    </div>
                    <DeleteOptions
                      label={displaySlotName(save)}
                      className="pointer-events-auto relative z-20 opacity-0 transition group-focus-within:opacity-100 group-hover:opacity-100 open:opacity-100"
                      onDelete={() => onRequestDelete(save)}
                    />
                  </div>
                </div>
              );
            })}
          </div>
        </section>

        {selectedSave ? (
          <RevisionPanel
            save={selectedSave}
            revisions={revisions}
            loading={loadingRevisions}
            error={revisionError}
          />
        ) : null}
      </div>
    </div>
  );
}
