import { useEffect, useId, type ReactNode } from 'react';
import type { GameSummary } from './game-library.js';

type DeleteDialogProps = {
  title: string;
  description: ReactNode;
  deleting: boolean;
  error: string;
  onCancel: () => void;
  onConfirm: () => void;
};

function DeleteDialog({
  title,
  description,
  deleting,
  error,
  onCancel,
  onConfirm,
}: DeleteDialogProps) {
  const titleID = useId();

  useEffect(() => {
    function closeOnEscape(event: KeyboardEvent) {
      if (event.key === 'Escape' && !deleting) onCancel();
    }

    window.addEventListener('keydown', closeOnEscape);
    return () => window.removeEventListener('keydown', closeOnEscape);
  }, [deleting, onCancel]);

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/80 px-5" role="presentation">
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleID}
        className="w-full max-w-md rounded-lg border border-white/10 bg-[#202020] p-6 shadow-2xl"
      >
        <h2 id={titleID} className="text-lg font-medium text-white">
          {title}
        </h2>
        <p className="mt-3 text-sm leading-6 text-neutral-400">{description}</p>

        {error ? (
          <p role="alert" className="mt-4 rounded-md bg-red-400/10 px-3 py-2 text-sm text-red-200">
            {error}
          </p>
        ) : null}

        <div className="mt-6 flex justify-end gap-3">
          <button
            type="button"
            onClick={onCancel}
            disabled={deleting}
            autoFocus
            className="rounded-md bg-white/5 px-4 py-2 text-sm font-medium text-neutral-300 transition hover:bg-white/10 disabled:opacity-40"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={onConfirm}
            disabled={deleting}
            className="rounded-md bg-red-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-red-500 disabled:opacity-40"
          >
            {deleting ? 'Deleting…' : 'Delete'}
          </button>
        </div>
      </section>
    </div>
  );
}

type DeleteStateProps = Pick<DeleteDialogProps, 'deleting' | 'error' | 'onCancel' | 'onConfirm'>;

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

export function DeleteSaveDialog({ name, ...state }: DeleteStateProps & { name: string }) {
  return (
    <DeleteDialog
      {...state}
      title={`Delete ${name}?`}
      description={
        <>
          This permanently deletes this save, its revision history, and any unshared artifacts. This
          cannot be undone.
        </>
      }
    />
  );
}
