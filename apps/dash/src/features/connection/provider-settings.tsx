import { Button } from '../../components/button.js';
import { SettingRow } from '../../components/setting-row.js';
import { GroupNote, SettingsGroup } from '../../components/settings-group.js';
import type { OwnerSetting } from '../../lib/omnisave-api.js';

type ProviderSettingsProps = {
  clientID?: OwnerSetting;
  clientSecret?: OwnerSetting;
  busy: boolean;
  onConnect: () => void;
  onDisconnect: () => void;
};

/**
 * The metadata providers this server can use, and what each one needs.
 *
 * IGDB needs credentials from the owner's own Twitch account (ADR-011), so it
 * reads as a thing to connect rather than as a pair of fields — connected or
 * not is the state worth seeing at a glance, and the credentials themselves
 * are a detail behind one click.
 */
export function ProviderSettings({
  clientID,
  clientSecret,
  busy,
  onConnect,
  onDisconnect,
}: ProviderSettingsProps) {
  const pinned = clientID?.editable === false || clientSecret?.editable === false;
  const connected = Boolean(clientID?.configured && clientSecret?.configured);

  return (
    <div>
      <SettingsGroup title="Game metadata">
        <SettingRow
          icon="tag"
          title="IGDB"
          subtitle={
            pinned
              ? `Set by the deployment (${clientID?.env_var})`
              : connected
                ? `Connected · client ${clientID?.text}`
                : 'Not connected · titles and art come from Hasheous alone'
          }
          trailing={
            pinned ? null : connected ? (
              <span className="flex shrink-0 gap-2">
                <Button disabled={busy} onClick={onConnect}>
                  Edit
                </Button>
                <Button variant="danger" disabled={busy} onClick={onDisconnect}>
                  Disconnect
                </Button>
              </span>
            ) : (
              <Button variant="filled" disabled={busy} onClick={onConnect}>
                Connect
              </Button>
            )
          }
        />
      </SettingsGroup>
      <GroupNote>
        Where titles, platforms, and cover art come from when a game is added to your library.
      </GroupNote>
    </div>
  );
}
