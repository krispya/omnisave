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

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    if (restoring) onRestore();
    else if (displayName.trim()) onFork(displayName.trim());
  }

  return (
    <div
      className="fixed inset-0 z-50 grid place-items-center bg-black/80 px-5"
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
        className="w-full max-w-md rounded-lg border border-white/10 bg-[#202020] p-6 shadow-2xl"
      >
        <p className="text-xs font-semibold tracking-[0.14em] text-[#e5a00d] uppercase">
          {saveName}
        </p>
        <h2 id={titleID} className="mt-2 text-lg font-medium text-white">
          {restoring
            ? `${label} to ${revisionName(action.revision)}?`
            : `Fork from ${revisionName(action.revision)}?`}
        </h2>
        <p className="mt-3 text-sm leading-6 text-neutral-400">
          {restoring
            ? 'This snapshot becomes current for every bound Device. No revision or branch is deleted.'
            : 'The new save shares this revision and its ancestors, then synchronizes independently.'}
        </p>

        {!restoring ? (
          <div className="mt-5">
            <label htmlFor={inputID} className="text-xs font-medium text-neutral-300">
              Save name
            </label>
            <input
              id={inputID}
              value={displayName}
              onChange={(event) => setDisplayName(event.target.value)}
              data-autofocus
              maxLength={100}
              className="mt-2 w-full rounded-md border border-white/10 bg-black/25 px-3 py-2 text-sm text-white outline-none focus:border-[#e5a00d]"
            />
          </div>
        ) : null}

        {error ? (
          <p role="alert" className="mt-4 rounded-md bg-red-400/10 px-3 py-2 text-sm text-red-200">
            {error}
          </p>
        ) : null}

        <div className="mt-6 flex justify-end gap-3">
          <button
            type="button"
            onClick={onCancel}
            disabled={busy}
            data-autofocus={restoring ? true : undefined}
            className="rounded-md bg-white/5 px-4 py-2 text-sm font-medium text-neutral-300 transition hover:bg-white/10 disabled:opacity-40"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={busy || (!restoring && !displayName.trim())}
            className="rounded-md bg-[#e5a00d] px-4 py-2 text-sm font-semibold text-black transition hover:bg-[#f2b72c] disabled:opacity-40"
          >
            {busy ? `${label}ing…` : label}
          </button>
        </div>
      </form>
    </div>
  );
}
