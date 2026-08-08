import { useRef, useState, type KeyboardEvent } from 'react';
import type { Omnisave } from '../../lib/omnisave-api.js';
import { displaySaveName } from './save-name.js';

type SaveNameEditorProps = {
  save: Omnisave;
  fallbackName: string;
  onSave: (save: Omnisave, displayName: string) => Promise<void>;
};

export function SaveNameEditor({ save, fallbackName, onSave }: SaveNameEditorProps) {
  const input = useRef<HTMLInputElement>(null);
  const cancelNextBlur = useRef(false);
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  function startEditing() {
    cancelNextBlur.current = false;
    setValue(displaySaveName(save, fallbackName));
    setError('');
    setEditing(true);
  }

  function cancelEditing() {
    if (saving) return;
    cancelNextBlur.current = true;
    setEditing(false);
    setError('');
  }

  async function commitEdit() {
    if (cancelNextBlur.current) {
      cancelNextBlur.current = false;
      return;
    }
    const displayName = value.trim();
    const currentName = save.display_name?.trim() ?? '';
    // A save always has a name, so clearing the field cancels the rename.
    if (
      !displayName ||
      displayName === currentName ||
      (!currentName && displayName === fallbackName)
    ) {
      setEditing(false);
      return;
    }

    setSaving(true);
    setError('');
    try {
      await onSave(save, displayName);
      setSaving(false);
      setEditing(false);
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : 'Could not rename this save.');
      setSaving(false);
      requestAnimationFrame(() => input.current?.focus());
    }
  }

  function handleKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === 'Enter') {
      event.preventDefault();
      event.currentTarget.blur();
    } else if (event.key === 'Escape') {
      event.preventDefault();
      cancelEditing();
    }
  }

  if (!editing) {
    return (
      <button
        type="button"
        onClick={startEditing}
        className="pointer-events-auto inline-block h-5 max-w-full truncate rounded align-top text-sm leading-5 font-semibold text-text outline-none hover:text-text focus-visible:ring-2 focus-visible:ring-text"
        title={`Rename ${displaySaveName(save, fallbackName)}`}
      >
        {displaySaveName(save, fallbackName)}
      </button>
    );
  }

  return (
    <div className="relative h-5 min-w-0">
      <input
        ref={input}
        autoFocus
        value={value}
        onFocus={(event) => event.currentTarget.select()}
        onChange={(event) => {
          setValue(event.target.value);
          setError('');
        }}
        onBlur={() => void commitEdit()}
        onKeyDown={handleKeyDown}
        disabled={saving}
        maxLength={100}
        aria-label={`Display name for ${fallbackName}`}
        aria-invalid={Boolean(error)}
        className={`pointer-events-auto -ml-2 h-5 w-[calc(100%+0.5rem)] min-w-0 rounded border-0 px-2 text-sm leading-5 font-semibold text-text outline-none disabled:opacity-60 ${
          error ? 'bg-danger/20' : 'bg-bg'
        }`}
      />
      {error ? (
        <p
          role="alert"
          className="absolute top-full left-0 z-30 mt-1 whitespace-nowrap rounded-sm border border-outline bg-surface px-2 py-1 text-xs text-danger"
        >
          {error}
        </p>
      ) : null}
    </div>
  );
}
