import type { Omnisave } from '../../lib/omnisave-api.js';
import type { GameSummary } from '../game/game-summary.js';
import type { LibrarySnapshot } from './use-library.js';

/** What a delete dialog removes: a Game with its saves, every save of a Game, or one save. */
export type DeleteTarget =
  | { type: 'game'; game: GameSummary }
  | { type: 'game-saves'; game: GameSummary }
  | { type: 'save'; game: GameSummary; save: Omnisave; name: string };

function deletedSaveIDs(target: DeleteTarget) {
  return new Set(
    target.type === 'save' ? [target.save.id] : target.game.saves.map((save) => save.id)
  );
}

/** The Library once a confirmed deletion is gone, shown without waiting for a reload. */
export function withoutDeleted(snapshot: LibrarySnapshot, target: DeleteTarget): LibrarySnapshot {
  const saveIDs = deletedSaveIDs(target);
  const gameID = target.type === 'game' ? target.game.id : undefined;
  return {
    ...snapshot,
    catalog: snapshot.catalog?.filter((game) => game.id !== gameID) ?? null,
    saves: snapshot.saves.filter((save) => !saveIDs.has(save.id) && save.game_id !== gameID),
  };
}

/** Whether a deletion is still on screen. Its dialog stays open until it is not. */
export function deletionShown(snapshot: LibrarySnapshot, target: DeleteTarget): boolean {
  const saveIDs = deletedSaveIDs(target);
  if (snapshot.saves.some((save) => saveIDs.has(save.id))) return true;
  return target.type === 'game' && !!snapshot.catalog?.some((game) => game.id === target.game.id);
}
