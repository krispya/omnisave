import { useState, type FormEvent } from 'react';
import { Button } from '../../components/button.js';
import { Dialog, DialogActions, DialogError } from '../../components/dialog.js';
import { fieldClass } from '../../components/field.js';

type PINDialogProps = {
  /** True when the server already has a PIN, which this one replaces. */
  pinSet: boolean;
  saving: boolean;
  error: string;
  onCancel: () => void;
  onSave: (pin: string) => void;
};

/** Sets or changes the owner PIN other browsers sign in with (ADR-010). */
export function PINDialog({ pinSet, saving, error, onCancel, onSave }: PINDialogProps) {
  const [pin, setPin] = useState('');

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!saving && pin.length === 4) onSave(pin);
  }

  return (
    <Dialog
      title={pinSet ? 'Change PIN' : 'Set a PIN'}
      description="Other browsers sign in with these four digits."
      busy={saving}
      onDismiss={onCancel}
    >
      <form onSubmit={submit} className="mt-5 flex flex-col gap-3">
        <label className="sr-only" htmlFor="new-owner-pin">
          New PIN
        </label>
        <input
          id="new-owner-pin"
          type="password"
          inputMode="numeric"
          pattern="[0-9]*"
          maxLength={4}
          value={pin}
          onChange={(event) => setPin(event.target.value.replace(/\D/g, '').slice(0, 4))}
          autoComplete="new-password"
          placeholder="New 4-digit PIN"
          autoFocus
          className={`${fieldClass} font-mono tracking-[0.4em] placeholder:font-sans placeholder:tracking-normal`}
        />

        {error ? <DialogError>{error}</DialogError> : null}

        <DialogActions>
          <Button variant="plain" onClick={onCancel} disabled={saving}>
            Cancel
          </Button>
          <Button type="submit" variant="filled" disabled={saving || pin.length !== 4}>
            {saving ? 'Saving…' : pinSet ? 'Change PIN' : 'Set PIN'}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}
