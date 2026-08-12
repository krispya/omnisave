import type { CatalogGame, DevicePresence } from '../../lib/omnisave-api.js';

/** Replaces catalog playing flags with the server's latest presence snapshot. */
export function applyPresence(catalog: CatalogGame[], devices: DevicePresence[]): CatalogGame[] {
  const reports = new Map(devices.map((device) => [device.device_id, device]));
  return catalog.map((game) => ({
    ...game,
    provenance: game.provenance.map((record) => {
      const report = reports.get(record.device_id);
      if (report?.playing_game_ids.includes(game.id)) {
        return { ...record, playing: true, playing_reported_at: report.reported_at };
      }
      return { ...record, playing: undefined, playing_reported_at: undefined };
    }),
  }));
}
