import { RouteLink } from '../../components/route-link.js';
import { GameMediaImage } from '../game/game-artwork.js';
import { backdrop, playingOn, type GameSummary } from '../game/game-summary.js';

/**
 * What is being played right now, across every device the server tracks.
 *
 * It sits above the Library because it answers a different question: the
 * Library is for finding a game, this is for seeing what is already happening.
 * When nothing is being played it is absent rather than empty — a row that
 * says "nothing" every day is a row that costs the Library space for nothing.
 *
 * The cards are landscape where the Library's are portrait, so the two never
 * read as the same list twice.
 */
export function NowPlaying({ games, token }: { games: GameSummary[]; token: string }) {
  const playing = games
    .map((game) => ({ game, devices: playingOn(game) }))
    .filter((entry) => entry.devices.length > 0);

  if (playing.length === 0) return null;

  return (
    <section className="mb-10" aria-label="Playing now">
      <h2 className="mb-4 text-xs font-semibold tracking-wide text-muted uppercase">Playing now</h2>
      {/* One row, scrolled rather than wrapped: this is a short list of what is
          happening, and wrapping it would let it grow into a second Library. */}
      <div className="-mx-1 flex snap-x gap-4 overflow-x-auto px-1 pb-2">
        {playing.map(({ game, devices }) => (
          <PlayingCard key={game.id} game={game} devices={devices} token={token} />
        ))}
      </div>
    </section>
  );
}

function PlayingCard({
  game,
  devices,
  token,
}: {
  game: GameSummary;
  devices: GameSummary['provenance'];
  token: string;
}) {
  const art = backdrop(game);
  const on = devices.map((device) => device.device_name).join(', ');

  return (
    <RouteLink
      to={{ name: 'game', gameID: game.id }}
      className="group w-80 shrink-0 snap-start"
      aria-label={`${game.label}, playing on ${on}`}
    >
      <div className="relative aspect-video overflow-hidden rounded-md bg-surface ring-1 ring-outline transition duration-120 group-hover:ring-2 group-hover:ring-accent">
        {art ? (
          <GameMediaImage
            token={token}
            media={art}
            alt=""
            className="absolute inset-0 size-full object-cover"
          />
        ) : null}
        {/* The scrim is the only reason the title is legible over artwork
            nobody chose for its contrast. */}
        <div className="absolute inset-0 bg-gradient-to-t from-black/85 via-black/25 to-transparent" />

        <div className="absolute inset-x-0 bottom-0 p-3.5">
          <p className="flex items-center gap-2 text-[11px] font-semibold tracking-wide text-accent uppercase">
            <span className="size-1.5 shrink-0 animate-pulse rounded-full bg-accent" />
            Playing
          </p>
          <h3 className="mt-1 truncate text-sm font-semibold text-white">{game.label}</h3>
          <p className="truncate text-xs text-white/60">{on}</p>
        </div>
      </div>
    </RouteLink>
  );
}
