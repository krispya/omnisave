import { useEffect, useId, useState, type FormEvent } from 'react';

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
  const titleID = useId();
  const [id, setID] = useState(clientID);
  const [secret, setSecret] = useState('');

  useEffect(() => {
    function closeOnEscape(event: KeyboardEvent) {
      if (event.key === 'Escape' && !saving) onCancel();
    }

    window.addEventListener('keydown', closeOnEscape);
    return () => window.removeEventListener('keydown', closeOnEscape);
  }, [saving, onCancel]);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!saving) onSave({ clientID: id.trim(), clientSecret: secret.trim() });
  }

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/80 px-5" role="presentation">
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleID}
        className="w-full max-w-md rounded-lg border border-white/10 bg-[#202020] p-6 shadow-2xl"
      >
        <h2 id={titleID} className="text-lg font-medium text-white">
          {configured ? 'Update IGDB credentials' : 'Connect IGDB'}
        </h2>
        <p className="mt-3 text-sm leading-6 text-neutral-400">
          Create an application in the Twitch developer console and paste its client ID and secret.
          They take effect immediately.
        </p>

        <form onSubmit={submit} className="mt-5 flex flex-col gap-3">
          <label className="flex flex-col gap-1.5 text-xs text-neutral-400">
            Client ID
            <input
              type="text"
              value={id}
              onChange={(event) => setID(event.target.value)}
              autoComplete="off"
              spellCheck={false}
              autoFocus
              className="rounded-md border border-white/10 bg-[#111111] px-3.5 py-2.5 font-mono text-sm text-white outline-none focus:border-[#e5a00d]"
            />
          </label>
          <label className="flex flex-col gap-1.5 text-xs text-neutral-400">
            Client secret
            <input
              type="password"
              value={secret}
              onChange={(event) => setSecret(event.target.value)}
              autoComplete="new-password"
              placeholder={configured ? 'Leave empty to keep the stored secret' : ''}
              className="rounded-md border border-white/10 bg-[#111111] px-3.5 py-2.5 font-mono text-sm text-white outline-none placeholder:font-sans placeholder:text-neutral-600 focus:border-[#e5a00d]"
            />
          </label>

          {error ? (
            <p role="alert" className="rounded-md bg-red-400/10 px-3 py-2 text-sm text-red-200">
              {error}
            </p>
          ) : null}

          <div className="mt-2 flex justify-end gap-3">
            <button
              type="button"
              onClick={onCancel}
              disabled={saving}
              className="rounded-md bg-white/5 px-4 py-2 text-sm font-medium text-neutral-300 transition hover:bg-white/10 disabled:opacity-40"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={saving || !id.trim() || (!configured && !secret.trim())}
              className="rounded-md bg-[#e5a00d] px-4 py-2 text-sm font-semibold text-black transition hover:bg-[#f2b51d] disabled:cursor-not-allowed disabled:opacity-40"
            >
              {saving ? 'Saving…' : configured ? 'Save' : 'Connect'}
            </button>
          </div>
        </form>
      </section>
    </div>
  );
}
