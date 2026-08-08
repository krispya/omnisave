import { useEffect, useState } from 'react';
import { loadGameMedia, type CatalogGame, type GameMedia } from '../../lib/omnisave-api.js';
import { createPromiseCache } from '../cache/promise-cache.js';
import type { GameSummary } from './game-summary.js';

// A game without cover art still needs to be told apart from its neighbours, so
// the fallback varies — but in value only. Hue here would make the covers the
// library is actually about compete with the ones it is still missing.
const artworkStyles = [
  'from-[#14161a] to-[#2c2f38]',
  'from-[#111318] to-[#242730]',
  'from-[#181a20] to-[#33363f]',
  'from-[#101216] to-[#2a2d36]',
  'from-[#15171d] to-[#383b45]',
  'from-[#131519] to-[#282b33]',
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
      className={`relative overflow-hidden rounded-sm bg-gradient-to-br ${artworkStyle} ${className}`}
      aria-hidden="true"
    >
      <div className="absolute inset-0 bg-gradient-to-t from-black/40 via-transparent to-white/5" />
      {/* The monogram stands in for cover art a library game does not have
          yet. A game the server does not list gets no such identity mark —
          its card presents a problem, not a game missing a picture. */}
      {game.inLibrary ? (
        <span className="absolute inset-0 grid place-items-center text-4xl font-black tracking-tighter text-text/70 sm:text-5xl">
          {initials(game.label)}
        </span>
      ) : null}
      {game.platform ? (
        <span className="absolute right-3 bottom-3 left-3 truncate text-[10px] font-semibold tracking-[0.16em] text-text/50 uppercase">
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
    return <div className={`animate-pulse bg-text/10 ${className}`} aria-hidden="true" />;
  }
  return null;
}
