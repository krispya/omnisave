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

function SaveTree({
  saves,
  selectedSave,
  onSelectSave,
}: {
  saves: OmniSave[];
  selectedSave?: OmniSave;
  onSelectSave: (save: OmniSave) => void;
}) {
  const ids = new Set(saves.map((save) => save.id));
  const children = new Map<string, OmniSave[]>();
  for (const save of saves) {
    const parentID = save.forked_from?.omnisave_id;
    if (!parentID || !ids.has(parentID)) continue;
    children.set(parentID, [...(children.get(parentID) ?? []), save]);
  }
  const roots = saves.filter((save) => !save.forked_from || !ids.has(save.forked_from.omnisave_id));
  const rows: Array<{ save: OmniSave; depth: number }> = [];
  const visited = new Set<string>();
  function visit(save: OmniSave, depth: number) {
    if (visited.has(save.id)) return;
    visited.add(save.id);
    rows.push({ save, depth });
    for (const child of children.get(save.id) ?? []) visit(child, depth + 1);
  }
  for (const root of roots) visit(root, 0);
  for (const save of saves) visit(save, 0);

  return (
    <div className="overflow-hidden rounded-md border border-white/5 bg-[#181818]">
      {rows.map(({ save, depth }) => {
        const saveIndex = saves.findIndex((candidate) => candidate.id === save.id);
        const name = displaySaveName(save, defaultSaveName(Math.max(saveIndex, 0)));
        const forkCount = children.get(save.id)?.length ?? 0;
        return (
          <button
            key={save.id}
            type="button"
            onClick={() => onSelectSave(save)}
            className={`relative flex w-full items-center gap-3 border-b border-white/5 py-3 pr-3 text-left transition last:border-0 ${
              save.id === selectedSave?.id ? 'bg-[#e5a00d]/10' : 'hover:bg-white/[0.035]'
            }`}
            style={{ paddingLeft: `${12 + depth * 24}px` }}
          >
            {depth > 0 ? (
              <span className="font-mono text-sm text-slate-600" aria-hidden="true">
                └─
              </span>
            ) : (
              <span className="size-2 rounded-full bg-[#e5a00d]" aria-hidden="true" />
            )}
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm font-medium text-white">{name}</span>
              <span className="mt-0.5 block truncate font-mono text-[10px] text-slate-600">
                {save.head_revision_id?.slice(0, 8) ?? 'No revisions'}
              </span>
            </span>
            {forkCount > 0 ? (
              <span className="text-[10px] text-slate-500">
                {forkCount} {forkCount === 1 ? 'fork' : 'forks'}
              </span>
            ) : null}
          </button>
        );
      })}
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
  const [saveView, setSaveView] = useState<'cards' | 'tree'>('cards');
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
            <div className="flex rounded-md bg-white/5 p-0.5 text-xs">
              {(['cards', 'tree'] as const).map((view) => (
                <button
                  key={view}
                  type="button"
                  aria-pressed={saveView === view}
                  onClick={() => setSaveView(view)}
                  className={`rounded px-2.5 py-1.5 capitalize transition ${
                    saveView === view ? 'bg-white/10 text-white' : 'text-slate-500 hover:text-white'
                  }`}
                >
                  {view}
                </button>
              ))}
            </div>
          </div>
          {saveView === 'tree' ? (
            <SaveTree saves={game.saves} selectedSave={selectedSave} onSelectSave={onSelectSave} />
          ) : (
            <div className="space-y-3">
              {game.saves.map((save, index) => {
                const selected = save.id === selectedSave?.id;
                const fallbackName = defaultSaveName(index);
                const name = displaySaveName(save, fallbackName);
                const sourceIndex = save.forked_from
                  ? game.saves.findIndex(
                      (candidate) => candidate.id === save.forked_from?.omnisave_id
                    )
                  : -1;
                const source = sourceIndex >= 0 ? game.saves[sourceIndex] : undefined;
                const sourceName = source
                  ? displaySaveName(source, defaultSaveName(sourceIndex))
                  : save.forked_from
                    ? 'unavailable save'
                    : undefined;
                const forkCount = game.saves.filter(
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
          )}
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
