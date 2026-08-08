import { OptionsMenu } from '../../components/options-menu.js';
import { RouteLink } from '../../components/route-link.js';
import { GameArtwork } from '../game/game-artwork.js';
import { releaseYear, type GameSummary } from '../game/game-summary.js';

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
        <h2 className="font-medium text-text">No games in the library</h2>
        <p className="mx-auto mt-2 max-w-sm text-sm leading-6 text-muted">
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
            className="aspect-[3/4] w-full ring-1 ring-outline transition duration-120 group-hover:ring-2 group-hover:ring-text"
          />
        </RouteLink>
        {saveCount > 0 ? (
          <span
            className="absolute top-2 right-2 z-10 grid h-6 min-w-6 place-items-center rounded-full bg-text px-1.5 text-xs font-bold text-bg"
            aria-label={`${saveCount} ${saveCount === 1 ? 'save' : 'saves'}`}
          >
            {saveCount}
          </span>
        ) : null}
        {/* Saves whose game the server does not list render from their own
            metadata, and actions that need a library entry (Fix Match) are
            missing. The mark sits over the middle of the artwork — this card
            is a problem to notice, not a game with a footnote — and lets
            clicks fall through to the card underneath. */}
        {!game.inLibrary ? (
          <span
            className="pointer-events-none absolute inset-0 z-10 grid place-items-center"
            aria-label="Not in the library"
          >
            <span className="grid size-12 place-items-center rounded-full bg-danger text-2xl font-bold text-bg">
              !
            </span>
          </span>
        ) : null}
        <OptionsMenu
          label={game.label}
          className="absolute right-2 bottom-2 z-10 opacity-0 transition group-focus-within:opacity-100 group-hover:opacity-100"
          onFixMatch={game.inLibrary ? () => onRequestFixMatch(game) : undefined}
          deleteLabel="Delete Saves…"
          onDelete={saveCount > 0 ? () => onRequestDeleteSaves(game) : undefined}
          onDeleteGame={game.inLibrary ? () => onRequestDeleteGame(game) : undefined}
        />
      </div>
      <RouteLink to={gameRoute} className="block w-full text-left">
        <h2 className="mt-2.5 truncate text-[13px] font-medium text-text/80 group-hover:text-text">
          {game.label}
        </h2>
        {/* The save count is already the badge on the artwork, so this line is
            free to carry the one fact that tells two games of the same name
            apart. It keeps its height when no provider supplied a year, so the
            captions across a row stay on one baseline. */}
        <p className="mt-1 h-4 truncate text-xs text-muted">{releaseYear(game) ?? ''}</p>
      </RouteLink>
    </article>
  );
}

export function GameLibraryLoading() {
  return (
    <div className="grid min-h-[50vh] place-items-center" role="status">
      <div
        className="size-9 animate-spin rounded-full border-2 border-outline border-t-text"
        aria-hidden="true"
      />
      <span className="sr-only">Loading game library…</span>
    </div>
  );
}
