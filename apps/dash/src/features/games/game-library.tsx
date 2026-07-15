import { Suspense, useEffect, useState, type ReactNode } from 'react';
import {
  loadGameMedia,
  type CatalogGame,
  type GameMedia,
  type OmniSave,
} from '../../lib/omnisave-api.js';
import { DeleteOptions } from './delete-options.js';
import { useCatalogGame } from './game-catalog-cache.js';

export type GameSummary = {
  id: string;
  label: string;
  platform?: string;
  publisher?: string;
  description?: string;
  media: GameMedia[];
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
      media: [],
      saves: [save],
      updatedAt: save.created_at,
    });
  }

  return [...games.values()].sort((left, right) => right.updatedAt.localeCompare(left.updatedAt));
}

function withCatalog(game: GameSummary, catalog: CatalogGame | null): GameSummary {
  if (!catalog) return game;
  return {
    ...game,
    label: catalog.title,
    platform: catalog.platform ?? game.platform,
    publisher: catalog.publisher,
    description: catalog.description,
    media: catalog.media,
  };
}

export function ResolvedGame({
  game,
  token,
  children,
}: {
  game: GameSummary;
  token: string;
  children: (game: GameSummary) => ReactNode;
}) {
  return children(withCatalog(game, useCatalogGame(token, game.id)));
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
  onRequestDelete,
}: {
  games: GameSummary[];
  token: string;
  onOpenGame: (game: GameSummary) => void;
  onRequestFixMatch: (game: GameSummary) => void;
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
        <Suspense key={game.id} fallback={<GameCardSkeleton />}>
          <ResolvedGame game={game} token={token}>
            {(resolved) => (
              <GameCard
                game={resolved}
                token={token}
                onOpenGame={onOpenGame}
                onRequestFixMatch={onRequestFixMatch}
                onRequestDelete={onRequestDelete}
              />
            )}
          </ResolvedGame>
        </Suspense>
      ))}
    </div>
  );
}

function GameCard({
  game,
  token,
  onOpenGame,
  onRequestFixMatch,
  onRequestDelete,
}: {
  game: GameSummary;
  token: string;
  onOpenGame: (game: GameSummary) => void;
  onRequestFixMatch: (game: GameSummary) => void;
  onRequestDelete: (game: GameSummary) => void;
}) {
  return (
    <article className="group min-w-0">
      <div className="relative">
        <button type="button" onClick={() => onOpenGame(game)} className="block w-full">
          <GameArtwork
            game={game}
            token={token}
            className="aspect-[3/4] w-full shadow-md shadow-black/30 ring-1 ring-white/10 transition duration-150 group-hover:scale-[1.02] group-hover:shadow-xl group-hover:shadow-black/50 group-hover:ring-[#e5a00d]"
          />
        </button>
        <DeleteOptions
          label={game.label}
          className="absolute right-2 bottom-2 z-10 opacity-0 transition group-focus-within:opacity-100 group-hover:opacity-100 open:opacity-100"
          onFixMatch={() => onRequestFixMatch(game)}
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
  );
}

function GameCardSkeleton() {
  return (
    <article className="min-w-0" aria-label="Loading game metadata">
      <div className="catalog-shimmer aspect-[3/4] rounded-md" />
      <div className="catalog-shimmer mt-3 h-3 w-3/4 rounded-full" />
      <div className="catalog-shimmer mt-2 h-2.5 w-1/3 rounded-full" />
    </article>
  );
}

export function GameDetailSkeleton() {
  return (
    <div className="mt-8">
      <div className="flex items-end gap-5 border-b border-white/5 pb-6">
        <div className="catalog-shimmer aspect-[3/4] w-20 rounded-md sm:w-28" />
        <div className="w-full max-w-sm pb-2">
          <div className="catalog-shimmer h-3 w-24 rounded-full" />
          <div className="catalog-shimmer mt-4 h-7 w-3/4 rounded" />
          <div className="catalog-shimmer mt-3 h-3 w-1/2 rounded-full" />
        </div>
      </div>
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
