import { expect, it } from 'vitest';
import type { GameSummary } from '../src/features/game/game-summary.js';
import { deletionShown, withoutDeleted } from '../src/features/library/library-deletion.js';
import type { LibrarySnapshot } from '../src/features/library/use-library.js';
import type { CatalogGame, Omnisave } from '../src/lib/omnisave-api.js';

const save = (id: string, gameID: string) => ({ id, game_id: gameID }) as Omnisave;
const library = (): LibrarySnapshot => ({
  catalog: [{ id: 'spire' }, { id: 'zomboid' }] as CatalogGame[],
  saves: [save('profile-1', 'spire'), save('profile-2', 'spire'), save('farm', 'zomboid')],
  error: '',
});
const spire = {
  id: 'spire',
  saves: [save('profile-1', 'spire'), save('profile-2', 'spire')],
} as GameSummary;

it('removes a deleted save from the Library without waiting for a reload', () => {
  const target = {
    type: 'save' as const,
    game: spire,
    save: save('profile-2', 'spire'),
    name: 'Profile 2',
  };
  expect(deletionShown(library(), target)).toBe(true);
  const shown = withoutDeleted(library(), target);
  expect(shown.saves.map((candidate) => candidate.id)).toEqual(['profile-1', 'farm']);
  expect(deletionShown(shown, target)).toBe(false);
});

it('removes a deleted game together with its saves', () => {
  const target = { type: 'game' as const, game: spire };
  const shown = withoutDeleted(library(), target);
  expect(shown.catalog?.map((game) => game.id)).toEqual(['zomboid']);
  expect(shown.saves.map((candidate) => candidate.id)).toEqual(['farm']);
  expect(deletionShown(shown, target)).toBe(false);
});

it('keeps a game whose saves alone were deleted', () => {
  const target = { type: 'game-saves' as const, game: spire };
  const shown = withoutDeleted(library(), target);
  expect(shown.catalog?.map((game) => game.id)).toEqual(['spire', 'zomboid']);
  expect(deletionShown(shown, target)).toBe(false);
});
