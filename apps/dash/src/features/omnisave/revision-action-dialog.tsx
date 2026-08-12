import {
  useEffect,
  useId,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent as ReactKeyboardEvent,
} from 'react';
import type { Revision } from '../../lib/omnisave-api.js';
import type { RevisionAction } from './revision-action.js';
import { revisionMoveLabel } from './revision-navigation.js';

export type { RevisionAction } from './revision-action.js';

type RevisionActionDialogProps = {
  action: RevisionAction;
  /** The save's name resolved with the list's positional fallback, never empty. */
  saveName: string;
  /**
   * Names of the devices playing this game right now. A restore under a
   * live session waits for the game to close before it lands on that
   * device, and a save written in the meantime branches and takes current
   * back (FDR-005, decision 15), so the dialog says so and the confirm
   * reads "Rewind anyway".
   */
  playingDevices: string[];
  busy: boolean;
  error: string;
  onCancel: () => void;
  onRestore: () => void;
  onFork: (displayName: string) => void;
};

function revisionName(revision: Revision) {
  return revision.display_name || revision.id.slice(0, 8);
}

export function RevisionActionDialog({
  action,
  saveName,
  playingDevices,
  busy,
  error,
  onCancel,
  onRestore,
  onFork,
}: RevisionActionDialogProps) {
  const titleID = useId();
  const inputID = useId();
  const dialogRef = useRef<HTMLFormElement>(null);
  const [displayName, setDisplayName] = useState(`${saveName} (fork)`);

  useEffect(() => {
    function closeOnEscape(event: KeyboardEvent) {
      if (event.key === 'Escape' && !busy) onCancel();
    }
    window.addEventListener('keydown', closeOnEscape);
    return () => window.removeEventListener('keydown', closeOnEscape);
  }, [busy, onCancel]);

  // Focus stays inside the dialog: the primary control takes it on open, Tab
  // wraps around the edges, and whatever opened the dialog gets it back.
  useEffect(() => {
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    dialogRef.current?.querySelector<HTMLElement>('[data-autofocus]')?.focus();
    return () => {
      if (opener?.isConnected) opener.focus();
    };
  }, []);

  function wrapTab(event: ReactKeyboardEvent<HTMLFormElement>) {
    if (event.key !== 'Tab') return;
    const stops = [
      ...(dialogRef.current?.querySelectorAll<HTMLElement>('input, button:not(:disabled)') ?? []),
    ];
    const edge = event.shiftKey ? stops[0] : stops[stops.length - 1];
    if (document.activeElement !== edge) return;
    event.preventDefault();
    (event.shiftKey ? stops[stops.length - 1] : stops[0])?.focus();
  }

  const restoring = action.kind === 'restore';
  const label = restoring ? revisionMoveLabel(action.move) : 'Fork';
  // Forks only add a lineage; restores are what a live session can undo.
  const live = restoring && playingDevices.length > 0;

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    if (restoring) onRestore();
    else if (displayName.trim()) onFork(displayName.trim());
  }

  return (
    <div
      className="fixed inset-0 z-50 grid place-items-center bg-black/70 px-5"
      role="presentation"
      onClick={(event) => {
        if (event.target === event.currentTarget && !busy) onCancel();
      }}
    >
      <form
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleID}
        onSubmit={submit}
        onKeyDown={wrapTab}
        className="w-full max-w-md rounded-lg border border-outline bg-surface p-6"
      >
        <p className="text-xs font-semibold tracking-[0.14em] text-text uppercase">{saveName}</p>
        <h2 id={titleID} className="mt-2 text-lg font-medium text-text">
          {restoring
            ? `${label} to ${revisionName(action.revision)}?`
            : `Fork from ${revisionName(action.revision)}?`}
        </h2>
        <p className="mt-3 text-sm leading-6 text-muted">
          {restoring
            ? 'This snapshot becomes current for every bound Device. No revision or branch is deleted.'
            : 'The new save shares this revision and its ancestors, then synchronizes independently.'}
        </p>

        {!restoring ? (
          <div className="mt-5">
            <label htmlFor={inputID} className="text-xs font-medium text-text">
              Save name
            </label>
            <input
              id={inputID}
              value={displayName}
              onChange={(event) => setDisplayName(event.target.value)}
              data-autofocus
              maxLength={100}
              className="mt-2 w-full rounded-md border border-outline bg-black/25 px-3 py-2 text-sm text-text outline-none focus:bg-text/15"
            />
          </div>
        ) : null}

        {live ? (
          <p role="alert" className="mt-4 rounded-md bg-accent/10 px-3 py-2 text-sm text-accent">
            Being played on {playingDevices.join(', ')}. The {label.toLowerCase()} lands there after
            the game closes, unless it saves first.
          </p>
        ) : null}

        {error ? (
          <p role="alert" className="mt-4 rounded-md bg-danger/10 px-3 py-2 text-sm text-danger">
            {error}
          </p>
        ) : null}

        <div className="mt-6 flex justify-end gap-3">
          <button
            type="button"
            onClick={onCancel}
            disabled={busy}
            data-autofocus={restoring ? true : undefined}
            className="rounded-md bg-text/8 px-4 py-2 text-sm font-medium text-text transition hover:bg-text/15 disabled:opacity-40"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={busy || (!restoring && !displayName.trim())}
            className="rounded-md bg-text px-4 py-2 text-sm font-semibold text-black transition hover:bg-[#f2b72c] disabled:opacity-40"
          >
            {busy ? `${label}ing…` : live ? `${label} anyway` : label}
          </button>
        </div>
      </form>
    </div>
  );
}
