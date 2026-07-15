import { createOmniSave, createRevision } from '../../lib/omnisave-api.js';

const debugGames = [
  { slug: 'chrono-trigger', label: 'Chrono Trigger', platform: 'SNES' },
  { slug: 'final-fantasy-vi', label: 'Final Fantasy VI', platform: 'SNES' },
  { slug: 'metroid-fusion', label: 'Metroid Fusion', platform: 'Game Boy Advance' },
  { slug: 'pokemon-emerald', label: 'Pokémon Emerald', platform: 'Game Boy Advance' },
  { slug: 'super-mario-64', label: 'Super Mario 64', platform: 'Nintendo 64' },
  {
    slug: 'symphony-of-the-night',
    label: 'Castlevania: Symphony of the Night',
    platform: 'PlayStation',
  },
  {
    slug: 'the-minish-cap',
    label: 'The Legend of Zelda: The Minish Cap',
    platform: 'Game Boy Advance',
  },
  { slug: 'earthbound', label: 'EarthBound', platform: 'SNES' },
];

function debugMetadata(label: string, platform?: string) {
  return {
    label,
    ...(platform ? { platform } : {}),
    source: 'dashboard-debug',
  };
}

export function createRandomTestOmniSave(token: string, existingLabels: string[]) {
  const sequence = Date.now().toString(36);
  const unusedGames = debugGames.filter((game) => !existingLabels.includes(game.label));
  const choices = unusedGames.length > 0 ? unusedGames : debugGames;
  const game = choices[Math.floor(Math.random() * choices.length)] ?? debugGames[0];

  return createOmniSave(token, {
    gameID: `${game.slug}-${sequence}`,
    slot: 'default',
    metadata: debugMetadata(game.label, game.platform),
  });
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
