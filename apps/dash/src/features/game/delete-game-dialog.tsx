import { DeleteDialog, type DeleteStateProps } from '../../components/delete-dialog.js';
import type { GameSummary } from './game-summary.js';

export function DeleteGameDialog({ game, ...state }: DeleteStateProps & { game: GameSummary }) {
  return (
    <DeleteDialog
      {...state}
      title={`Delete ${game.label}?`}
      description={
        game.saves.length > 0 ? (
          <>
            This permanently removes {game.label} from your library and deletes{' '}
            {game.saves.length === 1 ? 'its save' : `all ${game.saves.length} of its saves`}, their
            revision history, and any unshared artifacts. This cannot be undone.
          </>
        ) : (
          <>This permanently removes {game.label} from your library. This cannot be undone.</>
        )
      }
    />
  );
}

export function DeleteGameSavesDialog({ game, ...state }: DeleteStateProps & { game: GameSummary }) {
  return (
    <DeleteDialog
      {...state}
      title={`Delete all saves for ${game.label}?`}
      description={
        <>
          This permanently deletes {game.saves.length}{' '}
          {game.saves.length === 1 ? 'Omnisave' : 'Omnisaves'}, their revision history, and any
          unshared artifacts. {game.label} stays in your library. This cannot be undone.
        </>
      }
    />
  );
}
