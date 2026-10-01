import type { ReactNode } from 'react';
import { Button } from './button.js';
import { Dialog, DialogActions, DialogError } from './dialog.js';

type DeleteDialogProps = {
  title: string;
  description: ReactNode;
  deleting: boolean;
  error: string;
  /** What the confirming button says. Not every irreversible act is a delete. */
  confirmLabel?: string;
  busyLabel?: string;
  onCancel: () => void;
  onConfirm: () => void;
};

export type DeleteStateProps = Pick<
  DeleteDialogProps,
  'deleting' | 'error' | 'onCancel' | 'onConfirm'
>;

export function DeleteDialog({
  title,
  description,
  deleting,
  error,
  confirmLabel = 'Delete',
  busyLabel = 'Deleting…',
  onCancel,
  onConfirm,
}: DeleteDialogProps) {
  return (
    <Dialog title={title} description={description} busy={deleting} onDismiss={onCancel}>
      {error ? <DialogError>{error}</DialogError> : null}
      {deleting ? (
        <p role="status" className="mt-4 flex items-center gap-2 text-sm text-muted">
          <span
            className="size-4 animate-spin rounded-full border-2 border-outline border-t-text"
            aria-hidden="true"
          />
          {busyLabel}
        </p>
      ) : null}

      <DialogActions>
        {/* Focus starts on the way out, so a reflexive Enter cancels rather than deletes. */}
        <Button variant="plain" onClick={onCancel} disabled={deleting} autoFocus>
          Cancel
        </Button>
        <Button variant="danger" onClick={onConfirm} disabled={deleting}>
          {deleting ? busyLabel : confirmLabel}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
