import {
  commitRevision,
  createOmnisave,
  fixGameMatch,
  forkOmnisave,
  resolveGame,
  searchGameMatches,
  uploadArtifact,
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

export async function createRandomTestOmnisave(token: string, existingLabels: string[]) {
  const unusedGames = debugGames.filter((game) => !existingLabels.includes(game.label));
  const choices = unusedGames.length > 0 ? unusedGames : debugGames;
  const game = choices[Math.floor(Math.random() * choices.length)] ?? debugGames[0];

  const resolution = await resolveGame(token, {
    identifiers: [{ namespace: 'debug.slug', value: game.slug }],
    titleHint: game.label,
    platformHint: debugPlatform,
  });
  const created = await createOmnisave(token, {
    gameID: resolution.game.id,
    metadata: debugMetadata(game.label, debugPlatform),
  });
  const catalog = searchGameMatches(
    token,
    created.game_id,
    game.query ?? game.label,
    debugPlatform
  ).then((candidates) => {
    const candidate = candidates[0];
    if (!candidate) throw new Error(`Could not find a match for ${game.label}.`);
    return fixGameMatch(token, created.game_id, candidate.selection_token);
  });

  return { save: created, game: resolution.game, catalog };
}

export function createTestSave(
  token: string,
  game: { id: string; label: string; platform?: string }
) {
  return createOmnisave(token, {
    gameID: game.id,
    metadata: debugMetadata(game.label, game.platform),
  });
}

async function debugManifest(token: string, label: string) {
  const format = 'application/vnd.omnisave.raw-save.v1';
  const [progress, settings] = await Promise.all([
    uploadArtifact(token, new Blob([`Omnisave ${label}\n`], { type: format })),
    uploadArtifact(token, new Blob(['{"debug":true,"version":1}\n'], { type: 'application/json' })),
  ]);
  return [
    { path: 'progress.sav', artifact: progress },
    { path: 'settings.json', artifact: settings },
  ];
}

export async function createTestRevision(
  token: string,
  omnisaveID: string,
  expectedCurrentRevisionID: string | null
) {
  const sequence = Date.now().toString(36);
  const manifest = await debugManifest(token, `revision ${sequence}`);
  return commitRevision(token, omnisaveID, {
    expectedCurrentRevisionID,
    upserts: expectedCurrentRevisionID ? manifest.slice(0, 1) : manifest,
    metadata: {
      label: `Revision ${sequence}`,
      source: 'dashboard-debug',
    },
  });
}

export function forkTestSave(
  token: string,
  omnisaveID: string,
  revisionID: string,
  sourceName: string
) {
  const sequence = Date.now().toString(36);
  return forkOmnisave(token, omnisaveID, {
    revisionID,
    displayName: `${sourceName} fork`,
    metadata: {
      source: 'dashboard-debug',
      fork_sequence: sequence,
    },
  });
}
