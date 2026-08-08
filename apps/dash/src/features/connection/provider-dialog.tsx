import { useState, type FormEvent } from 'react';
import { Button } from '../../components/button.js';
import { Dialog, DialogActions, DialogError } from '../../components/dialog.js';
import { Field } from '../../components/field.js';

type ProviderDialogProps = {
  /** True when a secret is already stored, which changes what blank means. */
  configured: boolean;
  saving: boolean;
  error: string;
  clientID: string;
  onCancel: () => void;
  onSave: (values: { clientID: string; clientSecret: string }) => void;
};

/**
 * Where IGDB credentials are typed.
 *
 * The secret field starts empty even when one is stored, because the server
 * never hands a stored secret back (ADR-011). On a connected provider, leaving
 * it empty means "keep the one you have".
 */
export function ProviderDialog({
  configured,
  saving,
  error,
  clientID,
  onCancel,
  onSave,
}: ProviderDialogProps) {
  const [id, setID] = useState(clientID);
  const [secret, setSecret] = useState('');

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!saving) onSave({ clientID: id.trim(), clientSecret: secret.trim() });
  }

  return (
    <Dialog
      title={configured ? 'Update IGDB credentials' : 'Connect IGDB'}
      description="Create an application in the Twitch developer console and paste its client ID and secret. They take effect immediately."
      busy={saving}
      onDismiss={onCancel}
    >
      <form onSubmit={submit} className="mt-5 flex flex-col gap-3">
        <Field
          label="Client ID"
          type="text"
          value={id}
          onChange={(event) => setID(event.target.value)}
          autoComplete="off"
          spellCheck={false}
          autoFocus
          className="font-mono"
        />
        <Field
          label="Client secret"
          type="password"
          value={secret}
          onChange={(event) => setSecret(event.target.value)}
          autoComplete="new-password"
          placeholder={configured ? 'Leave empty to keep the stored secret' : ''}
          className="font-mono placeholder:font-sans"
        />

        {error ? <DialogError>{error}</DialogError> : null}

        <DialogActions>
          <Button variant="plain" onClick={onCancel} disabled={saving}>
            Cancel
          </Button>
          <Button
            type="submit"
            variant="filled"
            disabled={saving || !id.trim() || (!configured && !secret.trim())}
          >
            {saving ? 'Saving…' : configured ? 'Save' : 'Connect'}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}
