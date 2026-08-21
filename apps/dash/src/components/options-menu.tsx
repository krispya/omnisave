import { useId } from 'react';

type OptionsMenuProps = {
  label: string;
  className: string;
  onDownload?: () => void;
  onDownloadAllRevisions?: () => void;
  onFixMatch?: () => void;
  deleteLabel?: string;
  onDelete?: () => void;
  onDeleteGame?: () => void;
};

export function OptionsMenu({
  label,
  className,
  onDownload,
  onDownloadAllRevisions,
  onFixMatch,
  deleteLabel = 'Delete',
  onDelete,
  onDeleteGame,
}: OptionsMenuProps) {
  const menuID = useId();

  if (!onDownload && !onDownloadAllRevisions && !onFixMatch && !onDelete && !onDeleteGame) {
    return null;
  }

  return (
    <div className={`popover-owner ${className}`}>
      <button
        type="button"
        popoverTarget={menuID}
        className="grid size-8 cursor-pointer place-items-center rounded-full bg-black/75 text-text hover:bg-black"
      >
        <span className="sr-only">Options for {label}</span>
        <svg viewBox="0 0 24 24" className="size-4" aria-hidden="true">
          <circle cx="5" cy="12" r="1.75" fill="currentColor" />
          <circle cx="12" cy="12" r="1.75" fill="currentColor" />
          <circle cx="19" cy="12" r="1.75" fill="currentColor" />
        </svg>
      </button>
      <div
        id={menuID}
        popover="auto"
        className={`anchored-popover rounded-md border border-outline bg-surface p-1 ${
          onDownloadAllRevisions ? 'w-48' : 'w-36'
        }`}
      >
        {onDownload ? (
          <button
            type="button"
            popoverTarget={menuID}
            popoverTargetAction="hide"
            onClick={onDownload}
            className="w-full rounded px-3 py-2 text-left text-sm text-text hover:bg-text/8"
          >
            {onDownloadAllRevisions ? 'Download Current' : 'Download'}
          </button>
        ) : null}
        {onDownloadAllRevisions ? (
          <button
            type="button"
            popoverTarget={menuID}
            popoverTargetAction="hide"
            onClick={onDownloadAllRevisions}
            className="w-full rounded px-3 py-2 text-left text-sm text-text hover:bg-text/8"
          >
            Download All
          </button>
        ) : null}
        {onFixMatch ? (
          <button
            type="button"
            popoverTarget={menuID}
            popoverTargetAction="hide"
            onClick={onFixMatch}
            className="w-full rounded px-3 py-2 text-left text-sm text-text hover:bg-text/8"
          >
            Fix Match…
          </button>
        ) : null}
        {onDelete ? (
          <button
            type="button"
            popoverTarget={menuID}
            popoverTargetAction="hide"
            onClick={onDelete}
            className="w-full rounded px-3 py-2 text-left text-sm text-danger hover:bg-text/8"
          >
            {deleteLabel}
          </button>
        ) : null}
        {onDeleteGame ? (
          <button
            type="button"
            popoverTarget={menuID}
            popoverTargetAction="hide"
            onClick={onDeleteGame}
            className="w-full rounded px-3 py-2 text-left text-sm text-danger hover:bg-text/8"
          >
            Delete Game…
          </button>
        ) : null}
      </div>
    </div>
  );
}
