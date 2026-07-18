import { DeleteDialog, type DeleteStateProps } from '../../components/delete-dialog.js';

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
