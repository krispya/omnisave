import type { OmniSave } from '../../lib/omnisave-api.js';
import { DeleteOptions } from './delete-options.js';

export type GameSummary = {
  id: string;
  label: string;
  platform?: string;
  saves: OmniSave[];
  updatedAt: string;
};

const artworkStyles = [
  'from-emerald-950 via-emerald-800 to-teal-400',
  'from-violet-950 via-violet-800 to-fuchsia-400',
  'from-amber-950 via-orange-800 to-amber-400',
  'from-sky-950 via-blue-800 to-cyan-400',
  'from-rose-950 via-red-800 to-orange-400',
  'from-indigo-950 via-indigo-800 to-violet-400',
];

function artworkIndex(id: string) {
  return (
    id.split('').reduce((total, character) => total + character.charCodeAt(0), 0) %
    artworkStyles.length
  );
}

function initials(label: string) {
  return label
    .split(/\s+/)
    .filter((word) => !['of', 'the', 'a'].includes(word.toLowerCase()))
    .slice(0, 2)
    .map((word) => word[0])
    .join('')
    .toUpperCase();
}

export function groupOmniSavesByGame(saves: OmniSave[]) {
  const games = new Map<string, GameSummary>();

  for (const save of saves) {
    const existing = games.get(save.game_id);
    if (existing) {
      existing.saves.push(save);
      if (save.created_at > existing.updatedAt) existing.updatedAt = save.created_at;
      continue;
    }

    games.set(save.game_id, {
      id: save.game_id,
      label: save.metadata?.label ?? save.game_id,
      platform: save.metadata?.platform,
      saves: [save],
      updatedAt: save.created_at,
    });
  }

  return [...games.values()].sort((left, right) => right.updatedAt.localeCompare(left.updatedAt));
}

export function GameArtwork({ game, className = '' }: { game: GameSummary; className?: string }) {
  const artworkStyle = artworkStyles[artworkIndex(game.id)];

  return (
    <div
      className={`relative overflow-hidden rounded-md bg-gradient-to-br ${artworkStyle} ${className}`}
      aria-hidden="true"
    >
      <div className="absolute inset-0 bg-gradient-to-t from-black/55 via-transparent to-white/5" />
      <span className="absolute inset-0 grid place-items-center text-4xl font-black tracking-tighter text-white/85 sm:text-5xl">
        {initials(game.label)}
      </span>
      {game.platform ? (
        <span className="absolute right-3 bottom-3 left-3 truncate text-[10px] font-semibold tracking-[0.16em] text-white/70 uppercase">
          {game.platform}
        </span>
      ) : null}
    </div>
  );
}

export function GameLibrary({
  games,
  onOpenGame,
  onRequestDelete,
}: {
  games: GameSummary[];
  onOpenGame: (game: GameSummary) => void;
  onRequestDelete: (game: GameSummary) => void;
}) {
  if (games.length === 0) {
    return (
      <div className="py-24 text-center">
        <h2 className="font-medium text-white">No games yet</h2>
        <p className="mx-auto mt-2 max-w-sm text-sm leading-6 text-slate-400">
          Open the Debug menu and create an OmniSave to add a random game.
        </p>
      </div>
    );
  }

  return (
    <div className="grid grid-cols-2 gap-x-5 gap-y-7 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 xl:grid-cols-8">
      {games.map((game) => (
        <article key={game.id} className="group min-w-0">
          <div className="relative">
            <button type="button" onClick={() => onOpenGame(game)} className="block w-full">
              <GameArtwork
                game={game}
                className="aspect-[3/4] w-full shadow-md shadow-black/30 ring-1 ring-white/10 transition duration-150 group-hover:scale-[1.02] group-hover:shadow-xl group-hover:shadow-black/50 group-hover:ring-[#e5a00d]"
              />
            </button>
            <DeleteOptions
              label={game.label}
              className="absolute right-2 bottom-2 z-10 opacity-0 transition group-focus-within:opacity-100 group-hover:opacity-100 open:opacity-100"
              onDelete={() => onRequestDelete(game)}
            />
          </div>
          <button type="button" onClick={() => onOpenGame(game)} className="block w-full text-left">
            <h2 className="mt-2.5 truncate text-[13px] font-medium text-neutral-200 group-hover:text-white">
              {game.label}
            </h2>
            <p className="mt-1 text-xs text-slate-500">
              {game.saves.length} {game.saves.length === 1 ? 'save' : 'saves'}
            </p>
          </button>
        </article>
      ))}
    </div>
  );
}

export function GameLibrarySkeleton() {
  return (
    <div className="grid grid-cols-2 gap-5 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 xl:grid-cols-8">
      {Array.from({ length: 8 }, (_, index) => (
        <div key={index} className="aspect-[3/4] animate-pulse rounded-md bg-white/5" />
      ))}
    </div>
  );
}
