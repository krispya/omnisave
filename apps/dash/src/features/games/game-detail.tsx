import { useLayoutEffect, useRef, useState } from 'react';
import type { OmniSave, Revision } from '../../lib/omnisave-api.js';
import { DeleteOptions } from './delete-options.js';
import { GameArtwork, GameMediaImage, type GameSummary } from './game-library.js';
import { RevisionPanel } from './revision-panel.js';
import { SaveNameEditor } from './save-name-editor.js';
import { defaultSaveName, displaySaveName } from './save-name.js';

function formatDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return 'Unknown date';
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(date);
}

function GameDescription({ description }: { description: string }) {
  const text = useRef<HTMLParagraphElement>(null);
  const [expanded, setExpanded] = useState(false);
  const [canExpand, setCanExpand] = useState(false);

  useLayoutEffect(() => {
    if (expanded || !text.current) return;
    const element = text.current;
    const measure = () => setCanExpand(element.scrollHeight > element.clientHeight + 1);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [description, expanded]);

  return (
    <div className="mt-6 max-w-4xl">
      <p
        ref={text}
        className={`whitespace-pre-line text-sm leading-6 text-slate-400 ${expanded ? '' : 'line-clamp-3'}`}
      >
        {description}
      </p>
      {canExpand ? (
        <button
          type="button"
          aria-expanded={expanded}
          onClick={() => setExpanded((current) => !current)}
          className="mt-2 text-xs font-medium text-[#e5a00d] hover:text-[#f2b51d]"
        >
          {expanded ? 'Show less' : 'Show more'}
        </button>
      ) : null}
    </div>
  );
}

type GameDetailProps = {
  game: GameSummary;
  token: string;
  selectedSave?: OmniSave;
  revisions: Revision[];
  loadingRevisions: boolean;
  revisionError: string;
  onSelectSave: (save: OmniSave) => void;
  onRequestDelete: (save: OmniSave, name: string) => void;
  onRenameSave: (save: OmniSave, displayName: string) => Promise<void>;
};

export function GameDetail({
  game,
  token,
  selectedSave,
  revisions,
  loadingRevisions,
  revisionError,
  onSelectSave,
  onRequestDelete,
  onRenameSave,
}: GameDetailProps) {
  const selectedSaveIndex = selectedSave
    ? game.saves.findIndex((save) => save.id === selectedSave.id)
    : -1;
  const selectedSaveName = selectedSave
    ? displaySaveName(selectedSave, defaultSaveName(Math.max(selectedSaveIndex, 0)))
    : '';

  return (
    <div className="mt-8">
      <section className="flex items-end gap-5 border-b border-white/5 pb-6">
        <GameArtwork
          game={game}
          token={token}
          className="aspect-[3/4] w-20 shrink-0 shadow-lg sm:w-28"
        />
        <div className="min-w-0 pb-1">
          {game.platform ? (
            <p className="text-xs font-medium text-[#e5a00d]">{game.platform}</p>
          ) : null}
          <h2 className="mt-1.5 text-2xl font-medium tracking-tight text-white">{game.label}</h2>
          {game.publisher ? <p className="mt-1 text-sm text-slate-400">{game.publisher}</p> : null}
          <p className="mt-2 truncate font-mono text-xs text-slate-500">{game.id}</p>
          <p className="mt-3 text-sm text-slate-400">
            {game.saves.length} {game.saves.length === 1 ? 'save' : 'saves'}
          </p>
        </div>
      </section>

      {game.description ? (
        <GameDescription key={game.description} description={game.description} />
      ) : null}

      {game.media.some((media) => media.kind === 'screenshot') ? (
        <section className="mt-6" aria-label={`Screenshots for ${game.label}`}>
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            {game.media
              .filter((media) => media.kind === 'screenshot')
              .map((media, index) => (
                <div
                  key={media.id}
                  className="aspect-video overflow-hidden rounded-md bg-white/5 ring-1 ring-white/10"
                >
                  <GameMediaImage
                    token={token}
                    media={media}
                    alt={`${game.label} screenshot ${index + 1}`}
                    className="size-full object-cover"
                  />
                </div>
              ))}
          </div>
        </section>
      ) : null}

      <div className="mt-6 grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(22rem,0.8fr)]">
        <section aria-label={`Saves for ${game.label}`}>
          <div className="mb-4 flex items-center justify-between">
            <h3 className="text-sm font-semibold text-white">Saves</h3>
            <span className="text-xs text-slate-500">Select a save to view its history</span>
          </div>
          <div className="space-y-3">
            {game.saves.map((save, index) => {
              const selected = save.id === selectedSave?.id;
              const fallbackName = defaultSaveName(index);
              const name = displaySaveName(save, fallbackName);
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
                  <div className="pointer-events-none relative z-10 grid grid-cols-[2.25rem_minmax(0,1fr)_2rem] items-center gap-4 p-3.5">
                    <div className="grid size-9 shrink-0 place-items-center rounded bg-white/5 text-[#e5a00d]">
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
                    </div>
                    <div className="min-w-0">
                      <div className="h-5">
                        <SaveNameEditor
                          save={save}
                          fallbackName={fallbackName}
                          onSave={onRenameSave}
                        />
                      </div>
                      <p className="mt-1 text-xs text-slate-500">
                        Created {formatDate(save.created_at)}
                      </p>
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
        </section>

        {selectedSave ? (
          <RevisionPanel
            save={selectedSave}
            name={selectedSaveName}
            revisions={revisions}
            loading={loadingRevisions}
            error={revisionError}
          />
        ) : null}
      </div>
    </div>
  );
}
