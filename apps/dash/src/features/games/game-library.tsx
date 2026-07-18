import { useEffect, useState } from 'react';
import {
  loadGameMedia,
  type CatalogGame,
  type GameMedia,
  type OmniSave,
} from '../../lib/omnisave-api.js';
import { DeleteOptions } from './delete-options.js';

export type GameSummary = {
  id: string;
  label: string;
  sortKey: string;
  platform?: string;
  publisher?: string;
  description?: string;
  media: GameMedia[];
  saves: OmniSave[];
  inCatalog: boolean;
};

const collator = new Intl.Collator(undefined, { sensitivity: 'base', numeric: true });

// The server's Library is the source of truth: every Game appears whether or
// not it has saves. Saves whose game the server does not list (games endpoints
// disabled, or out of sync) still render, described by their own metadata.
export function buildLibrary(catalog: CatalogGame[] | null, saves: OmniSave[]): GameSummary[] {
  const savesByGame = new Map<string, OmniSave[]>();
  for (const save of saves) {
    savesByGame.set(save.game_id, [...(savesByGame.get(save.game_id) ?? []), save]);
  }

  const library: GameSummary[] = (catalog ?? []).map((game) => ({
    id: game.id,
    label: game.title,
    sortKey: game.sort_title?.trim() || game.title,
    platform: game.platform,
    publisher: game.publisher,
    description: game.description,
    media: game.media,
    saves: savesByGame.get(game.id) ?? [],
    inCatalog: true,
  }));

  const known = new Set(library.map((game) => game.id));
  for (const [gameID, gameSaves] of savesByGame) {
    if (known.has(gameID)) continue;
    const label = gameSaves[0]?.metadata?.label ?? gameID;
    library.push({
      id: gameID,
      label,
      sortKey: label,
      platform: gameSaves[0]?.metadata?.platform,
      media: [],
      saves: gameSaves,
      inCatalog: false,
    });
  }

  return library.sort((left, right) => collator.compare(left.sortKey, right.sortKey));
}

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

export function GameArtwork({
  game,
  token,
  className = '',
}: {
  game: GameSummary;
  token: string;
  className?: string;
}) {
  const artworkStyle = artworkStyles[artworkIndex(game.id)];
  const cover = game.media.find((media) => media.kind === 'cover');

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
      {cover ? (
        <GameMediaImage
          token={token}
          media={cover}
          alt=""
          className="absolute inset-0 size-full object-cover"
        />
      ) : null}
    </div>
  );
}

export function GameMediaImage({
  token,
  media,
  alt,
  className = '',
}: {
  token: string;
  media: GameMedia;
  alt: string;
  className?: string;
}) {
  const [source, setSource] = useState('');

  useEffect(() => {
    const controller = new AbortController();
    let objectURL = '';
    setSource('');
    void loadGameMedia(token, media.url, controller.signal)
      .then((blob) => {
        objectURL = URL.createObjectURL(blob);
        setSource(objectURL);
      })
      .catch((error: unknown) => {
        if (!(error instanceof DOMException && error.name === 'AbortError')) setSource('');
      });
    return () => {
      controller.abort();
      if (objectURL) URL.revokeObjectURL(objectURL);
    };
  }, [media.url, token]);

  return source ? <img src={source} alt={alt} className={className} /> : null;
}

export function GameLibrary({
  games,
  token,
  onOpenGame,
  onRequestFixMatch,
  onRequestDeleteSaves,
}: {
  games: GameSummary[];
  token: string;
  onOpenGame: (game: GameSummary) => void;
  onRequestFixMatch: (game: GameSummary) => void;
  onRequestDeleteSaves: (game: GameSummary) => void;
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
          onOpenGame={onOpenGame}
          onRequestFixMatch={onRequestFixMatch}
          onRequestDeleteSaves={onRequestDeleteSaves}
        />
      ))}
    </div>
  );
}

function GameCard({
  game,
  token,
  onOpenGame,
  onRequestFixMatch,
  onRequestDeleteSaves,
}: {
  game: GameSummary;
  token: string;
  onOpenGame: (game: GameSummary) => void;
  onRequestFixMatch: (game: GameSummary) => void;
  onRequestDeleteSaves: (game: GameSummary) => void;
}) {
  const saveCount = game.saves.length;

  return (
    <article className="group min-w-0">
      <div className="relative">
        <button type="button" onClick={() => onOpenGame(game)} className="block w-full">
          <GameArtwork
            game={game}
            token={token}
            className={`aspect-[3/4] w-full shadow-md shadow-black/30 ring-1 ring-white/10 transition duration-150 group-hover:scale-[1.02] group-hover:shadow-xl group-hover:shadow-black/50 group-hover:ring-[#e5a00d] ${
              saveCount === 0 ? 'opacity-60 saturate-[0.65]' : ''
            }`}
          />
        </button>
        {saveCount > 0 ? (
          <span
            className="absolute -top-1.5 -right-1.5 z-10 grid h-6 min-w-6 place-items-center rounded-md bg-[#e5a00d] px-1.5 text-xs font-bold text-black shadow-lg shadow-black/40"
            aria-label={`${saveCount} ${saveCount === 1 ? 'save' : 'saves'}`}
          >
            {saveCount}
          </span>
        ) : null}
        <DeleteOptions
          label={game.label}
          className="absolute right-2 bottom-2 z-10 opacity-0 transition group-focus-within:opacity-100 group-hover:opacity-100 open:opacity-100"
          onFixMatch={game.inCatalog ? () => onRequestFixMatch(game) : undefined}
          deleteLabel="Delete Saves…"
          onDelete={saveCount > 0 ? () => onRequestDeleteSaves(game) : undefined}
        />
      </div>
      <button type="button" onClick={() => onOpenGame(game)} className="block w-full text-left">
        <h2 className="mt-2.5 truncate text-[13px] font-medium text-neutral-200 group-hover:text-white">
          {game.label}
        </h2>
        <p className={`mt-1 text-xs ${saveCount === 0 ? 'text-slate-600' : 'text-slate-500'}`}>
          {saveCount === 0 ? 'No saves yet' : `${saveCount} ${saveCount === 1 ? 'save' : 'saves'}`}
        </p>
      </button>
    </article>
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
