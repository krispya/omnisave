import { useId } from 'react';
import { releaseYear, type GameSummary } from '../game/game-summary.js';

export type LibrarySort = 'title' | 'year' | 'added' | 'updated';

const sortLabels: Record<LibrarySort, string> = {
  title: 'Title',
  year: 'Release Year',
  added: 'Date Added',
  updated: 'Last Updated',
};

const librarySorts = Object.keys(sortLabels) as LibrarySort[];

const sortStorageKey = 'omnisave.librarySort';

/** The sort chosen last visit, defaulting to the library's title order. */
export function storedLibrarySort(): LibrarySort {
  try {
    const stored = localStorage.getItem(sortStorageKey) as LibrarySort | null;
    return stored && librarySorts.includes(stored) ? stored : 'title';
  } catch {
    return 'title';
  }
}

/** When a save last moved: its newest upload, or its creation for a fresh fork
    whose current revision predates it. */
function lastUpdated(game: GameSummary) {
  let latest = 0;
  for (const save of game.saves) {
    for (const stamp of [save.current_revision_created_at, save.created_at]) {
      const time = Date.parse(stamp);
      if (!Number.isNaN(time) && time > latest) latest = time;
    }
  }
  return latest;
}

/** When the game was added: the server records no such moment, so the earliest
    sighting — a device first tracking it or a save arriving — stands in. */
function addedAt(game: GameSummary) {
  let earliest = Infinity;
  const stamps = [
    ...game.provenance.map((record) => record.first_tracked_at),
    ...game.saves.map((save) => save.created_at),
  ];
  for (const stamp of stamps) {
    const time = Date.parse(stamp);
    if (!Number.isNaN(time) && time < earliest) earliest = time;
  }
  return earliest === Infinity ? 0 : earliest;
}

function releaseYearValue(game: GameSummary) {
  const year = Number(releaseYear(game));
  return Number.isFinite(year) ? year : 0;
}

/** Reorders the title-sorted library, newest first; ties and games the metric
    knows nothing about (no saves, no year) keep title order at the end. */
export function sortLibrary(games: GameSummary[], sort: LibrarySort): GameSummary[] {
  if (sort === 'title') return games;
  const metric = sort === 'year' ? releaseYearValue : sort === 'added' ? addedAt : lastUpdated;
  return [...games].sort((left, right) => metric(right) - metric(left));
}

export function LibrarySortControl({
  sort,
  onSortChange,
}: {
  sort: LibrarySort;
  onSortChange: (sort: LibrarySort) => void;
}) {
  const menuID = useId();

  return (
    <div>
      <button
        type="button"
        popoverTarget={menuID}
        className="flex cursor-pointer items-center gap-1.5 text-sm font-medium text-text/80 transition duration-120 hover:text-text"
      >
        <span className="sr-only">Sort </span>
        By {sortLabels[sort]}
        <svg viewBox="0 0 24 24" className="size-3.5" aria-hidden="true">
          <path d="M6 9l6 6 6-6" fill="none" stroke="currentColor" strokeWidth="2" />
        </svg>
      </button>
      <div
        id={menuID}
        popover="auto"
        className="anchored-popover anchored-popover-start w-44 rounded-md border border-outline bg-surface p-1"
      >
        {librarySorts.map((option) => (
          <button
            key={option}
            type="button"
            popoverTarget={menuID}
            popoverTargetAction="hide"
            aria-pressed={option === sort}
            onClick={() => {
              try {
                localStorage.setItem(sortStorageKey, option);
              } catch {
                // A private window that refuses storage still gets the sort for the session.
              }
              onSortChange(option);
            }}
            className="flex w-full items-center justify-between rounded px-3 py-2 text-left text-sm text-text hover:bg-text/8"
          >
            {sortLabels[option]}
            {option === sort ? (
              <svg viewBox="0 0 24 24" className="size-3.5" aria-hidden="true">
                <path d="M5 12l5 5 9-10" fill="none" stroke="currentColor" strokeWidth="2" />
              </svg>
            ) : null}
          </button>
        ))}
      </div>
    </div>
  );
}
