import { DeleteDialog, type DeleteStateProps } from '../../components/delete-dialog.js';

export function DeleteRevisionDialog({ name, ...state }: DeleteStateProps & { name: string }) {
  return (
    <DeleteDialog
      {...state}
      title={`Delete revision ${name}?`}
      description={
        <>
          This permanently deletes this snapshot and any content only it references. This cannot be
          undone.
        </>
      }
      confirmLabel="Delete revision"
      busyLabel="Deleting…"
    />
  );
}
