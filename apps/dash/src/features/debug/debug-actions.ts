import {
  createOmniSave,
  createRevision,
  fixGameMatch,
  searchGameMatches,
} from '../../lib/omnisave-api.js';

const debugGames = [
  { slug: 'chrono-trigger', label: 'Chrono Trigger' },
  { slug: 'donkey-kong-country', label: 'Donkey Kong Country' },
  { slug: 'earthbound', label: 'EarthBound' },
  { slug: 'f-zero', label: 'F-Zero' },
  { slug: 'super-mario-world', label: 'Super Mario World' },
  { slug: 'super-metroid', label: 'Super Metroid' },
  {
    slug: 'the-legend-of-zelda',
    label: 'The Legend of Zelda: A Link to the Past',
    query: 'Link to the Past',
  },
];

const debugPlatform = 'Super Nintendo Entertainment System';

function debugMetadata(label: string, platform?: string) {
  return {
    label,
    ...(platform ? { platform } : {}),
    source: 'dashboard-debug',
  };
}

export async function createRandomTestOmniSave(token: string, existingLabels: string[]) {
  const sequence = Date.now().toString(36);
  const unusedGames = debugGames.filter((game) => !existingLabels.includes(game.label));
  const choices = unusedGames.length > 0 ? unusedGames : debugGames;
  const game = choices[Math.floor(Math.random() * choices.length)] ?? debugGames[0];

  const created = await createOmniSave(token, {
    gameID: `${game.slug}-${sequence}`,
    slot: 'slot-1',
    metadata: debugMetadata(game.label, debugPlatform),
  });
  const catalog = searchGameMatches(
    token,
    created.game_id,
    game.query ?? game.label,
    debugPlatform
  ).then((candidates) => {
    const candidate = candidates[0];
    if (!candidate) throw new Error(`Hasheous did not find ${game.label}.`);
    return fixGameMatch(token, created.game_id, candidate.selection_token);
  });

  return { save: created, catalog };
}

export function createTestSave(
  token: string,
  game: { id: string; label: string; platform?: string },
  slot: string
) {
  return createOmniSave(token, {
    gameID: game.id,
    slot,
    metadata: debugMetadata(game.label, game.platform),
  });
}

export function createTestRevision(token: string, omniSaveID: string, parentID?: string) {
  const sequence = Date.now().toString(36);
  const payload = new Blob([`OmniSave debug payload ${sequence}\n`], {
    type: 'application/octet-stream',
  });

  return createRevision(
    token,
    omniSaveID,
    {
      parentIDs: parentID ? [parentID] : [],
      format: 'application/vnd.omnisave.raw-save.v1',
      metadata: {
        label: `Debug revision ${sequence}`,
        source: 'dashboard-debug',
      },
    },
    payload,
    `debug-${sequence}.sav`
  );
}
