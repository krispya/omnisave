import { useEffect, useId, useRef, type KeyboardEvent as ReactKeyboardEvent } from 'react';
import type { Omnisave, Revision } from '../../lib/omnisave-api.js';
import { formatBytes, formatDateTime } from '../../lib/format.js';

type RevisionDetailsDialogProps = {
  save: Omnisave;
  /** The save's full history, for naming the parent. */
  revisions: Revision[];
  revision: Revision;
  onClose: () => void;
};

function revisionName(revision: Revision) {
  return revision.display_name || revision.id.slice(0, 8);
}

// Displays all revision metadata available to the Dash.
export function RevisionDetailsDialog({
  save,
  revisions,
  revision,
  onClose,
}: RevisionDetailsDialogProps) {
  const titleID = useId();
  const dialogRef = useRef<HTMLElement>(null);
  const isCurrent = revision.id === save.current_revision_id;
  const isForkPoint = revision.id === save.forked_from?.revision_id;
  const parent = revisions.find((candidate) => candidate.id === revision.parent_id);
  const totalSize = revision.files.reduce((total, file) => total + file.artifact.size, 0);

  useEffect(() => {
    function closeOnEscape(event: KeyboardEvent) {
      if (event.key === 'Escape') onClose();
    }
    window.addEventListener('keydown', closeOnEscape);
    return () => window.removeEventListener('keydown', closeOnEscape);
  }, [onClose]);

  // Focus stays inside the dialog: the close control takes it on open, Tab
  // wraps around the edges, and whatever opened the dialog gets it back.
  useEffect(() => {
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    dialogRef.current?.querySelector<HTMLElement>('[data-autofocus]')?.focus();
    return () => {
      if (opener?.isConnected) opener.focus();
    };
  }, []);

  function wrapTab(event: ReactKeyboardEvent<HTMLElement>) {
    if (event.key !== 'Tab') return;
    const stops = [
      ...(dialogRef.current?.querySelectorAll<HTMLElement>('button:not(:disabled)') ?? []),
    ];
    const edge = event.shiftKey ? stops[0] : stops[stops.length - 1];
    if (document.activeElement !== edge) return;
    event.preventDefault();
    (event.shiftKey ? stops[stops.length - 1] : stops[0])?.focus();
  }

  return (
    <div
      className="fixed inset-0 z-50 grid place-items-center bg-black/70 px-5"
      role="presentation"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <section
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleID}
        onKeyDown={wrapTab}
        className="max-h-[85vh] w-full max-w-xl overflow-y-auto rounded-lg border border-outline bg-surface p-6"
      >
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0">
            <h2
              id={titleID}
              className="flex flex-wrap items-center gap-2 text-lg font-medium text-text"
            >
              <span className="truncate">{revisionName(revision)}</span>
              {isCurrent ? (
                <span className="rounded bg-text/15 px-1.5 py-0.5 text-[10px] font-medium text-text">
                  current
                </span>
              ) : null}
              {isForkPoint ? (
                <span className="rounded bg-text/8 px-1.5 py-0.5 text-[10px] font-normal text-muted">
                  shared fork point
                </span>
              ) : null}
            </h2>
          </div>
          <button
            type="button"
            onClick={onClose}
            data-autofocus
            aria-label="Close details"
            className="-mt-1 -mr-1 rounded-md p-2 text-muted transition hover:bg-text/8 hover:text-text"
          >
            ✕
          </button>
        </div>
        <dl className="mt-5 grid grid-cols-1 gap-x-8 gap-y-3 text-sm sm:grid-cols-[8rem_minmax(0,1fr)]">
          <dt className="text-muted">Revision ID</dt>
          <dd className="min-w-0 font-mono text-xs break-all text-text">{revision.id}</dd>
          <dt className="text-muted">Created</dt>
          <dd className="min-w-0 text-text">{formatDateTime(revision.created_at)}</dd>
          <dt className="text-muted">Parent</dt>
          <dd className="min-w-0 text-text">
            {revision.parent_id ? (
              <span className="font-mono text-xs break-all">
                {parent ? `${revisionName(parent)} · ` : ''}
                {revision.parent_id}
              </span>
            ) : (
              'root'
            )}
          </dd>
          <dt className="text-muted">Files</dt>
          <dd className="min-w-0 text-text">
            <span className="text-xs text-muted">
              {revision.files.length} {revision.files.length === 1 ? 'file' : 'files'} ·{' '}
              {formatBytes(totalSize)}
            </span>
            <ul className="mt-2 space-y-2">
              {revision.files.map((file) => (
                <li key={file.path} className="min-w-0">
                  <p className="font-mono text-xs break-all text-text">{file.path}</p>
                  <p className="font-mono text-[10px] break-all text-muted">
                    {formatBytes(file.artifact.size)} · {file.artifact.sha256}
                  </p>
                </li>
              ))}
            </ul>
          </dd>
        </dl>
      </section>
    </div>
  );
}
