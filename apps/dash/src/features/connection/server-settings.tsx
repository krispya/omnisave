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
import { ActionRow, SwitchRow } from '../../components/setting-row.js';
import { GroupNote, SettingsGroup } from '../../components/settings-group.js';

type ServerSettingsProps = {
  token: string;
  /** The credential this browser holds, so the list can point at itself. */
  credentialID: string;
  /** Pending requests, watched by the shell so they can interrupt anywhere. */
  requests: PairingRequest[];
  onAnswered: () => Promise<unknown>;
  /** Forgets this browser's credential without revoking it on the server. */
  onDisconnect: () => void;
};

/**
 * Everything about the server rather than about the Library: who is asking to
 * connect, what already holds a credential, and whether the server announces
 * itself on the local network.
 *
 * Pending requests arrive and expire while someone is looking at this page, so
 * it follows the server's event stream rather than waiting for a reload.
 */
export function ServerSettings({
  token,
  credentialID,
  requests,
  onAnswered,
  onDisconnect,
}: ServerSettingsProps) {
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

  const networkSettings = settings.filter((setting) => setting.group === 'network');

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-8">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight text-text">Server</h1>
        <p className="mt-1.5 text-sm text-muted">
          Connecting devices, the credentials they hold, and how this server is found.
        </p>
      </div>

      {error ? (
        <p
          role="alert"
          className="rounded-md border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger"
        >
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

      {networkSettings.length === 0 ? null : (
        <div>
          <SettingsGroup title="Network">
            {networkSettings.map((setting) => (
              <SwitchRow
                key={setting.key}
                icon="network"
                title={setting.summary}
                subtitle={
                  setting.editable
                    ? setting.value
                      ? 'On'
                      : 'Off'
                    : `Set by the deployment (${setting.env_var}); change it there.`
                }
                label={setting.summary}
                checked={setting.value}
                disabled={!setting.editable || busyID === setting.key}
                onChange={() =>
                  void act(setting.key, () =>
                    updateSetting(token, setting.key, String(!setting.value))
                  )
                }
              />
            ))}
          </SettingsGroup>
          <GroupNote>
            Announcing lets a device on this network find the server without being told its address.
            It never grants access: a device that finds this server still pairs and is still approved
            here.
          </GroupNote>
        </div>
      )}

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

      <div>
        <SettingsGroup title="This browser">
          <ActionRow
            icon="devices"
            title="Disconnect"
            subtitle="Sign out here and keep the credential"
            onClick={onDisconnect}
          />
        </SettingsGroup>
        <GroupNote>
          Disconnecting forgets this browser&rsquo;s credential locally; the server still accepts it,
          so signing in again does not need approving again. Revoke it above to withdraw it for good.
        </GroupNote>
      </div>

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
