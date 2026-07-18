import type { CatalogGame, Omnisave } from '../../lib/omnisave-api.js';
import type { GameSummary } from '../game/game-summary.js';

const collator = new Intl.Collator(undefined, { sensitivity: 'base', numeric: true });

// The server's Library is the source of truth: every Game appears whether or
// not it has saves. Saves whose game the server does not list (games endpoints
// disabled, or out of sync) still render, described by their own metadata.
export function buildLibrary(catalog: CatalogGame[] | null, saves: Omnisave[]): GameSummary[] {
  const savesByGame = new Map<string, Omnisave[]>();
  for (const save of saves) {
    savesByGame.set(save.game_id, [...(savesByGame.get(save.game_id) ?? []), save]);
  }

  const library: GameSummary[] = (catalog ?? []).map((game) => ({
    id: game.id,
    label: game.title,
    sortKey: game.sort_title?.trim() || game.title,
    platform: game.platform,
    platformCompany: game.platform_company,
    publisher: game.publisher,
    description: game.description,
    metadataSource: game.metadata_source,
    identifiers: game.identifiers ?? [],
    fingerprints: game.fingerprints ?? [],
    metadata: game.metadata,
    refreshedAt: game.refreshed_at,
    media: game.media,
    provenance: game.provenance ?? [],
    saves: savesByGame.get(game.id) ?? [],
    inLibrary: true,
  }));

  const known = new Set(library.map((game) => game.id));
  for (const [gameID, gameSaves] of savesByGame) {
    if (known.has(gameID)) continue;
    const label = gameSaves[0]?.metadata?.label ?? gameID;
    library.push({
      id: gameID,
      label,
      sortKey: label,
      platform: gameSaves[0]?.metadata?.platform,
      identifiers: [],
      fingerprints: [],
      media: [],
      provenance: [],
      saves: gameSaves,
      inLibrary: false,
    });
  }

  return library.sort((left, right) => collator.compare(left.sortKey, right.sortKey));
}
