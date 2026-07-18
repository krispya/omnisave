import { useEffect, useState } from 'react';
import { loadGameMedia, type CatalogGame, type GameMedia } from '../../lib/omnisave-api.js';
import { createPromiseCache } from '../cache/promise-cache.js';
import type { GameSummary } from './game-summary.js';

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
          key={cover.url}
          token={token}
          media={cover}
          alt=""
          className="absolute inset-0 size-full object-cover"
        />
      ) : null}
    </div>
  );
}

// Media object URLs live for the whole session so a remounted poster renders
// its image on the first paint instead of flashing the fallback artwork.
const gameMediaCache = createPromiseCache<string, string>({
  dispose: (objectURL) => URL.revokeObjectURL(objectURL),
});
const failedArtworkPreloads = new Set<string>();

async function loadDecodedGameMedia(token: string, mediaURL: string, signal?: AbortSignal) {
  const blob = await loadGameMedia(token, mediaURL, signal);
  const objectURL = URL.createObjectURL(blob);
  const image = new Image();
  image.src = objectURL;
  try {
    await image.decode();
    return objectURL;
  } catch (error) {
    URL.revokeObjectURL(objectURL);
    throw error;
  }
}

function cacheGameMedia(token: string, mediaURL: string, signal?: AbortSignal) {
  return gameMediaCache.load(mediaURL, () => loadDecodedGameMedia(token, mediaURL, signal));
}

export async function preloadGameArtwork(token: string, games: CatalogGame[], signal?: AbortSignal) {
  const covers = new Map<string, GameMedia>();
  for (const game of games) {
    const cover = game.media.find((media) => media.kind === 'cover');
    if (cover) covers.set(cover.url, cover);
  }

  const coverList = Array.from(covers.values());
  for (const cover of coverList) failedArtworkPreloads.delete(cover.url);

  const results = await Promise.allSettled(
    coverList.map((cover) => cacheGameMedia(token, cover.url, signal))
  );
  if (signal?.aborted) throw new DOMException('The library load was aborted.', 'AbortError');
  results.forEach((result, index) => {
    if (result.status === 'rejected') failedArtworkPreloads.add(coverList[index].url);
  });
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
  const cachedSource = gameMediaCache.get(media.url);
  const preloadFailed = failedArtworkPreloads.has(media.url);
  const [source, setSource] = useState<string | null | undefined>(() =>
    preloadFailed ? null : cachedSource
  );

  useEffect(() => {
    if (cachedSource) {
      setSource(cachedSource);
      return;
    }
    if (preloadFailed) {
      setSource(null);
      return;
    }

    let cancelled = false;
    setSource(undefined);
    cacheGameMedia(token, media.url)
      .then((objectURL) => {
        if (!cancelled) setSource(objectURL);
      })
      .catch(() => {
        if (!cancelled) setSource(null);
      });
    return () => {
      cancelled = true;
    };
  }, [cachedSource, media.url, preloadFailed, token]);

  if (source) return <img src={source} alt={alt} className={className} />;
  if (source === undefined) {
    return <div className={`animate-pulse bg-neutral-800 ${className}`} aria-hidden="true" />;
  }
  return null;
}
