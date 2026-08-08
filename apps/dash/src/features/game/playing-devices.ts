import type { CatalogGame, DevicePresence } from '../../lib/omnisave-api.js';

/**
 * Stitches a fresh presence picture into the catalog. Every provenance
 * record's playing flags are recomputed from the reports, so a device absent
 * from the picture — stopped, crashed, or aged out — reads as not playing.
 *
 * There is deliberately no clock in here: the server owns the credibility
 * window and announces devices.changed when a report ages out, so what it
 * served most recently is the truth until it says otherwise.
 */
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
