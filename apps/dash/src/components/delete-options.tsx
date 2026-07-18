import { useDismissibleDetails } from '../lib/use-dismissible-details.js';

type DeleteOptionsProps = {
  label: string;
  className: string;
  onFixMatch?: () => void;
  deleteLabel?: string;
  onDelete?: () => void;
  onDeleteGame?: () => void;
};

export function DeleteOptions({
  label,
  className,
  onFixMatch,
  deleteLabel = 'Delete',
  onDelete,
  onDeleteGame,
}: DeleteOptionsProps) {
  const menu = useDismissibleDetails();

  if (!onFixMatch && !onDelete && !onDeleteGame) return null;

  function requestDelete() {
    menu.current?.removeAttribute('open');
    onDelete?.();
  }

  function requestDeleteGame() {
    menu.current?.removeAttribute('open');
    onDeleteGame?.();
  }

  function requestFixMatch() {
    menu.current?.removeAttribute('open');
    onFixMatch?.();
  }

  return (
    <details ref={menu} className={className}>
      <summary className="grid size-8 cursor-pointer list-none place-items-center rounded-full bg-black/75 text-white marker:content-none hover:bg-black">
        <span className="sr-only">Options for {label}</span>
        <svg viewBox="0 0 24 24" className="size-4" aria-hidden="true">
          <circle cx="5" cy="12" r="1.75" fill="currentColor" />
          <circle cx="12" cy="12" r="1.75" fill="currentColor" />
          <circle cx="19" cy="12" r="1.75" fill="currentColor" />
        </svg>
      </summary>
      <div className="absolute right-0 bottom-full mb-2 w-36 rounded-md border border-white/10 bg-[#202020] p-1 shadow-xl">
        {onFixMatch ? (
          <button
            type="button"
            onClick={requestFixMatch}
            className="w-full rounded px-3 py-2 text-left text-sm text-neutral-200 hover:bg-white/5"
          >
            Fix Match…
          </button>
        ) : null}
        {onDelete ? (
          <button
            type="button"
            onClick={requestDelete}
            className="w-full rounded px-3 py-2 text-left text-sm text-red-300 hover:bg-white/5"
          >
            {deleteLabel}
          </button>
        ) : null}
        {onDeleteGame ? (
          <button
            type="button"
            onClick={requestDeleteGame}
            className="w-full rounded px-3 py-2 text-left text-sm text-red-300 hover:bg-white/5"
          >
            Delete Game…
          </button>
        ) : null}
      </div>
    </details>
  );
}
