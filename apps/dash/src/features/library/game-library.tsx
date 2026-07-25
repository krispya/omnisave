import { OptionsMenu } from '../../components/options-menu.js';
import { RouteLink } from '../../components/route-link.js';
import { GameArtwork } from '../game/game-artwork.js';
import type { GameSummary } from '../game/game-summary.js';

export function GameLibrary({
  games,
  token,
  onRequestFixMatch,
  onRequestDeleteSaves,
  onRequestDeleteGame,
}: {
  games: GameSummary[];
  token: string;
  onRequestFixMatch: (game: GameSummary) => void;
  onRequestDeleteSaves: (game: GameSummary) => void;
  onRequestDeleteGame: (game: GameSummary) => void;
}) {
  if (games.length === 0) {
    return (
      <div className="py-24 text-center">
        <h2 className="font-medium text-white">No games in the library</h2>
        <p className="mx-auto mt-2 max-w-sm text-sm leading-6 text-slate-400">
          Your library shows every game the server knows.
        </p>
      </div>
    );
  }

  return (
    <div className="grid grid-cols-2 gap-x-5 gap-y-7 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 xl:grid-cols-8">
      {games.map((game) => (
        <GameCard
          key={game.id}
          game={game}
          token={token}
          onRequestFixMatch={onRequestFixMatch}
          onRequestDeleteSaves={onRequestDeleteSaves}
          onRequestDeleteGame={onRequestDeleteGame}
        />
      ))}
    </div>
  );
}

function GameCard({
  game,
  token,
  onRequestFixMatch,
  onRequestDeleteSaves,
  onRequestDeleteGame,
}: {
  game: GameSummary;
  token: string;
  onRequestFixMatch: (game: GameSummary) => void;
  onRequestDeleteSaves: (game: GameSummary) => void;
  onRequestDeleteGame: (game: GameSummary) => void;
}) {
  const saveCount = game.saves.length;
  const gameRoute = { name: 'game', gameID: game.id } as const;

  return (
    <article className="group min-w-0">
      <div className="relative">
        {/* The title below is the same link, named; this one is here for the pointer. */}
        <RouteLink to={gameRoute} aria-hidden="true" tabIndex={-1} className="block w-full">
          <GameArtwork
            game={game}
            token={token}
            className="aspect-[3/4] w-full shadow-md shadow-black/30 ring-1 ring-white/10 transition duration-150 group-hover:scale-[1.02] group-hover:shadow-xl group-hover:shadow-black/50 group-hover:ring-[#e5a00d]"
          />
        </RouteLink>
        {saveCount > 0 ? (
          <span
            className="absolute -top-1.5 -right-1.5 z-10 grid h-6 min-w-6 place-items-center rounded-md bg-[#e5a00d] px-1.5 text-xs font-bold text-black shadow-lg shadow-black/40"
            aria-label={`${saveCount} ${saveCount === 1 ? 'save' : 'saves'}`}
          >
            {saveCount}
          </span>
        ) : null}
        <OptionsMenu
          label={game.label}
          className="absolute right-2 bottom-2 z-10 opacity-0 transition group-focus-within:opacity-100 group-hover:opacity-100 open:opacity-100"
          onFixMatch={game.inLibrary ? () => onRequestFixMatch(game) : undefined}
          deleteLabel="Delete Saves…"
          onDelete={saveCount > 0 ? () => onRequestDeleteSaves(game) : undefined}
          onDeleteGame={game.inLibrary ? () => onRequestDeleteGame(game) : undefined}
        />
      </div>
      <RouteLink to={gameRoute} className="block w-full text-left">
        <h2 className="mt-2.5 truncate text-[13px] font-medium text-neutral-200 group-hover:text-white">
          {game.label}
        </h2>
        <p className="mt-1 text-xs text-slate-500">
          {saveCount === 0 ? 'No saves yet' : `${saveCount} ${saveCount === 1 ? 'save' : 'saves'}`}
        </p>
      </RouteLink>
    </article>
  );
}

export function GameLibraryLoading() {
  return (
    <div className="grid min-h-[50vh] place-items-center" role="status">
      <div
        className="size-9 animate-spin rounded-full border-2 border-white/10 border-t-[#e5a00d]"
        aria-hidden="true"
      />
      <span className="sr-only">Loading game library…</span>
    </div>
  );
}
