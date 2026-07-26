import { useCallback, useEffect, useState } from 'react';
import {
  approvePairingRequest,
  denyPairingRequest,
  listCredentials,
  listSettings,
  revokeCredential,
  updateSetting,
  type Credential,
  type OwnerSetting,
  type PairingRequest,
} from '../../lib/omnisave-api.js';
import { CredentialList } from './credential-list.js';
import { PendingRequests } from './pending-requests.js';
import { ProviderDialog } from './provider-dialog.js';
import { ProviderSettings } from './provider-settings.js';
import { DeleteDialog } from '../../components/delete-dialog.js';

type ServerSettingsProps = {
  token: string;
  /** The credential this browser holds, so the list can point at itself. */
  credentialID: string;
  /** Pending requests, watched by the shell so they can interrupt anywhere. */
  requests: PairingRequest[];
  onAnswered: () => Promise<unknown>;
};

/**
 * Everything about the server rather than about the Library: who is asking to
 * connect, what already holds a credential, and whether the server announces
 * itself on the local network.
 *
 * Pending requests arrive and expire while someone is looking at this page, so
 * it follows the server's event stream rather than waiting for a reload.
 */
export function ServerSettings({ token, credentialID, requests, onAnswered }: ServerSettingsProps) {
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [settings, setSettings] = useState<OwnerSetting[]>([]);
  const [busyID, setBusyID] = useState('');
  const [error, setError] = useState('');
  const [connecting, setConnecting] = useState(false);
  const [disconnecting, setDisconnecting] = useState(false);
  const [providerError, setProviderError] = useState('');

  const refresh = useCallback(async () => {
    try {
      const [issued, owned] = await Promise.all([listCredentials(token), listSettings(token)]);
      setCredentials(issued);
      setSettings(owned);
      setError('');
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : 'Could not load server settings.');
    }
  }, [token]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  // Every owner action here has the same shape: mark the row busy, do the one
  // thing, then re-read. The re-read is deliberate rather than left to the
  // event stream — an approval has to look like it landed even if the stream
  // is down.
  async function act(id: string, action: () => Promise<unknown>) {
    setBusyID(id);
    try {
      await action();
      await Promise.all([refresh(), onAnswered()]);
    } catch (actionError) {
      setError(actionError instanceof Error ? actionError.message : 'That did not work.');
    } finally {
      setBusyID('');
    }
  }

  const clientID = settings.find((setting) => setting.key === 'igdb.client_id');
  const clientSecret = settings.find((setting) => setting.key === 'igdb.client_secret');

  // Connecting, editing, and disconnecting are all the same write: an empty
  // secret keeps the stored one, and empty everything turns the provider off.
  async function saveProvider(values: { clientID: string; clientSecret: string }) {
    setBusyID('igdb');
    setProviderError('');
    try {
      await updateSetting(token, 'igdb.client_id', values.clientID);
      if (values.clientSecret !== '' || values.clientID === '') {
        await updateSetting(token, 'igdb.client_secret', values.clientSecret);
      }
      await refresh();
      setConnecting(false);
      setDisconnecting(false);
    } catch (saveError) {
      setProviderError(saveError instanceof Error ? saveError.message : 'That did not work.');
    } finally {
      setBusyID('');
    }
  }

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-6">
      <div>
        <h1 className="text-lg font-semibold text-white">Server settings</h1>
        <p className="mt-1 text-sm text-slate-400">
          Connecting devices, the credentials they hold, and how this server is found.
        </p>
      </div>

      {error ? (
        <p className="rounded-md border border-red-400/30 bg-red-400/5 px-4 py-3 text-sm text-red-300">
          {error}
        </p>
      ) : null}

      <PendingRequests
        requests={requests}
        busyID={busyID}
        onApprove={(pairingRequest) =>
          void act(pairingRequest.id, () => approvePairingRequest(token, pairingRequest.id))
        }
        onDeny={(pairingRequest) =>
          void act(pairingRequest.id, () => denyPairingRequest(token, pairingRequest.id))
        }
      />

      <CredentialList
        credentials={credentials}
        currentID={credentialID}
        busyID={busyID}
        onRevoke={(credential) =>
          void act(credential.id, () => revokeCredential(token, credential.id))
        }
      />

      <section className="rounded-lg border border-white/5 bg-[#1a1a1a] p-6">
        <h2 className="font-semibold text-white">Network</h2>
        <p className="mt-2 text-sm leading-6 text-slate-400">
          Announcing lets a device on this network find the server without being told its address. It
          never grants access: a device that finds this server still pairs and is still approved here.
        </p>

        <ul className="mt-5 flex flex-col gap-2">
          {settings
            .filter((setting) => setting.group === 'network')
            .map((setting) => (
              <li
                key={setting.key}
                className="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-md border border-white/10 bg-[#111111] px-4 py-3"
              >
                <span className="min-w-0 flex-1">
                  <span className="block text-sm text-white">{setting.summary}</span>
                  {setting.editable ? null : (
                    <span className="block text-xs text-neutral-500">
                      Set by the deployment ({setting.env_var}); change it there.
                    </span>
                  )}
                </span>
                <button
                  type="button"
                  role="switch"
                  aria-checked={setting.value}
                  aria-label={setting.summary}
                  disabled={!setting.editable || busyID === setting.key}
                  onClick={() =>
                    void act(setting.key, () =>
                      updateSetting(token, setting.key, String(!setting.value))
                    )
                  }
                  className={`relative h-6 w-11 shrink-0 rounded-full transition disabled:cursor-not-allowed disabled:opacity-40 ${
                    setting.value ? 'bg-[#e5a00d]' : 'bg-neutral-700'
                  }`}
                >
                  <span
                    className={`absolute top-0.5 size-5 rounded-full bg-white transition-all ${
                      setting.value ? 'left-[1.375rem]' : 'left-0.5'
                    }`}
                  />
                </button>
              </li>
            ))}
        </ul>
      </section>

      <ProviderSettings
        clientID={clientID}
        clientSecret={clientSecret}
        busy={busyID === 'igdb'}
        onConnect={() => {
          setProviderError('');
          setConnecting(true);
        }}
        onDisconnect={() => {
          setProviderError('');
          setDisconnecting(true);
        }}
      />

      {connecting ? (
        <ProviderDialog
          configured={Boolean(clientSecret?.configured)}
          saving={busyID === 'igdb'}
          error={providerError}
          clientID={clientID?.text ?? ''}
          onCancel={() => setConnecting(false)}
          onSave={(values) => void saveProvider(values)}
        />
      ) : null}

      {disconnecting ? (
        <DeleteDialog
          title="Disconnect IGDB?"
          description="Titles and cover art will come from Hasheous alone until you connect it again. Your library keeps everything IGDB has already supplied."
          deleting={busyID === 'igdb'}
          error={providerError}
          confirmLabel="Disconnect"
          busyLabel="Disconnecting…"
          onCancel={() => setDisconnecting(false)}
          onConfirm={() => void saveProvider({ clientID: '', clientSecret: '' })}
        />
      ) : null}
    </div>
  );
}
