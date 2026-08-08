import { useRef, useState, type KeyboardEvent } from 'react';
import type { Revision } from '../../lib/omnisave-api.js';

type RevisionNameEditorProps = {
  revision: Revision;
  fallbackName: string;
  onSave: (revision: Revision, displayName: string) => Promise<void>;
};

export function RevisionNameEditor({ revision, fallbackName, onSave }: RevisionNameEditorProps) {
  const input = useRef<HTMLInputElement>(null);
  const cancelNextBlur = useRef(false);
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const displayName = revision.display_name?.trim() || fallbackName;

  function startEditing() {
    cancelNextBlur.current = false;
    setValue(displayName);
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
    const name = value.trim();
    const currentName = revision.display_name?.trim() ?? '';
    if (!name || name === currentName || (!currentName && name === fallbackName)) {
      setEditing(false);
      return;
    }

    setSaving(true);
    setError('');
    try {
      await onSave(revision, name);
      setSaving(false);
      setEditing(false);
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : 'Could not name this revision.');
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
        className="pointer-events-auto relative z-10 max-w-48 truncate rounded font-mono text-xs text-muted outline-none hover:text-text focus-visible:ring-2 focus-visible:ring-text"
        title={`Name revision ${fallbackName}`}
      >
        {displayName}
      </button>
    );
  }

  return (
    <div className="pointer-events-auto relative z-20 min-w-0 max-w-48">
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
        aria-label={`Name for revision ${fallbackName}`}
        aria-invalid={Boolean(error)}
        className={`h-6 w-48 max-w-full rounded border-0 px-1.5 font-mono text-xs text-text outline-none disabled:opacity-60 ${
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
