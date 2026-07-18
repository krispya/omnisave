import { useState } from 'react';
import type { Omnisave, Revision } from '../../lib/omnisave-api.js';
import { GameArtwork, GameMediaImage } from './game-artwork.js';
import type { GameSummary } from './game-summary.js';
import { defaultSaveName, displaySaveName } from '../omnisave/save-name.js';
import { GameDetailsDialog } from './details-dialog.js';
import { RevisionPanel } from '../omnisave/revision-panel.js';
import { SaveList } from '../omnisave/save-list.js';
import { SaveTree } from '../omnisave/save-tree.js';
import { TrackedDevices } from './tracked-devices.js';

function gameReleaseYear(game: GameSummary) {
  const value = game.metadata?.['release_year'];
  return typeof value === 'string' || typeof value === 'number' ? String(value) : undefined;
}

function gameGenres(game: GameSummary) {
  const value = game.metadata?.['genres'];
  if (Array.isArray(value)) {
    return value.filter((genre): genre is string => typeof genre === 'string');
  }
  return typeof value === 'string' ? [value] : [];
}

type GameDetailProps = {
  game: GameSummary;
  token: string;
  selectedSave?: Omnisave;
  revisions: Revision[];
  loadingRevisions: boolean;
  revisionError: string;
  onSelectSave: (save: Omnisave) => void;
  onRequestDelete: (save: Omnisave, name: string) => void;
  onRenameSave: (save: Omnisave, displayName: string) => Promise<void>;
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
  const [detailsOpen, setDetailsOpen] = useState(false);
  const selectedSaveIndex = selectedSave
    ? game.saves.findIndex((save) => save.id === selectedSave.id)
    : -1;
  const selectedSaveName = selectedSave
    ? displaySaveName(selectedSave, defaultSaveName(Math.max(selectedSaveIndex, 0)))
    : '';
  const releaseYear = gameReleaseYear(game);
  const genres = gameGenres(game);
  const facts = [
    releaseYear,
    game.publisher,
    genres.length > 0 ? genres.join(', ') : undefined,
  ].filter((fact): fact is string => Boolean(fact));

  return (
    <div className="mt-8">
      <section className="flex items-end gap-6 border-b border-white/5 pb-6">
        <GameArtwork
          game={game}
          token={token}
          className="aspect-[3/4] w-28 shrink-0 shadow-lg sm:w-36"
        />
        <div className="min-w-0 pb-1">
          {game.platform ? (
            <p className="text-xs font-semibold tracking-[0.16em] text-[#e5a00d] uppercase">
              {game.platform}
            </p>
          ) : null}
          <h2 className="mt-2 text-3xl font-semibold tracking-tight text-white sm:text-4xl">
            {game.label}
          </h2>
          <p className="mt-3 flex flex-wrap items-center gap-x-2.5 gap-y-1 text-sm text-slate-400">
            {facts.map((fact, index) => (
              <span key={fact} className="flex items-center gap-x-2.5">
                {index > 0 ? (
                  <span className="text-slate-600" aria-hidden="true">
                    ·
                  </span>
                ) : null}
                {fact}
              </span>
            ))}
          </p>
          <div className="mt-4">
            <button
              type="button"
              onClick={() => setDetailsOpen(true)}
              className="rounded-md bg-white/5 px-3.5 py-2 text-xs font-medium text-neutral-300 transition hover:bg-white/10 hover:text-white"
            >
              Details
            </button>
          </div>
        </div>
      </section>

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

      <TrackedDevices provenance={game.provenance} />

      <div className="mt-6 grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(22rem,0.8fr)]">
        <section aria-label={`Saves for ${game.label}`}>
          <div className="mb-4 flex items-center justify-between">
            <h3 className="text-sm font-semibold text-white">Saves</h3>
            {game.saves.length > 0 ? (
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
            ) : null}
          </div>
          {game.saves.length === 0 ? (
            <div className="rounded-md border border-dashed border-white/10 bg-white/[0.02] px-6 py-14 text-center">
              <p className="text-sm font-medium text-white">This game has no saves</p>
              <p className="mx-auto mt-2 max-w-xs text-xs leading-5 text-slate-500">
                It stays in your library either way. Add one with Debug → New save, or bind a local
                save from a client.
              </p>
            </div>
          ) : saveView === 'tree' ? (
            <SaveTree saves={game.saves} selectedSave={selectedSave} onSelectSave={onSelectSave} />
          ) : (
            <SaveList
              saves={game.saves}
              selectedSave={selectedSave}
              onSelectSave={onSelectSave}
              onRequestDelete={onRequestDelete}
              onRenameSave={onRenameSave}
            />
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

      {detailsOpen ? <GameDetailsDialog game={game} onClose={() => setDetailsOpen(false)} /> : null}
    </div>
  );
}
