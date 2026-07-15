export type OmniSave = {
  id: string;
  game_id: string;
  slot: string;
  created_at: string;
  metadata?: Record<string, string>;
};

export type Revision = {
  id: string;
  omnisave_id: string;
  parent_ids: string[] | null;
  created_at: string;
  artifact: {
    format: string;
    sha256: string;
    size: number;
  };
  metadata?: Record<string, string>;
};

const apiBaseURL = import.meta.env.VITE_API_BASE_URL ?? '';

async function request<T>(path: string, token: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  headers.set('Authorization', `Bearer ${token}`);

  const response = await fetch(`${apiBaseURL}${path}`, {
    ...init,
    headers,
  });

  if (!response.ok) {
    const message =
      response.status === 401
        ? 'The API token was not accepted.'
        : `Request failed (${response.status}).`;
    throw new Error(message);
  }

  return response.json() as Promise<T>;
}

export function listOmniSaves(token: string, signal?: AbortSignal) {
  return request<OmniSave[]>('/api/v1/omnisaves', token, { signal });
}

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

function createOmniSave(
  token: string,
  input: { gameID: string; slot: string; label: string; platform?: string }
) {
  return request<OmniSave>('/api/v1/omnisaves', token, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      game_id: input.gameID,
      slot: input.slot,
      metadata: {
        label: input.label,
        ...(input.platform ? { platform: input.platform } : {}),
        source: 'dashboard-debug',
      },
    }),
  });
}

export function createRandomTestOmniSave(token: string, existingLabels: string[]) {
  const sequence = Date.now().toString(36);
  const unusedGames = debugGames.filter((game) => !existingLabels.includes(game.label));
  const choices = unusedGames.length > 0 ? unusedGames : debugGames;
  const game = choices[Math.floor(Math.random() * choices.length)] ?? debugGames[0];

  return createOmniSave(token, {
    gameID: `${game.slug}-${sequence}`,
    slot: 'default',
    label: game.label,
    platform: game.platform,
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
    label: game.label,
    platform: game.platform,
  });
}

export function listRevisions(token: string, omniSaveID: string, signal?: AbortSignal) {
  return request<Revision[]>(`/api/v1/omnisaves/${omniSaveID}/revisions`, token, { signal });
}

export function createTestRevision(token: string, omniSaveID: string, parentID?: string) {
  const sequence = Date.now().toString(36);
  const form = new FormData();
  form.append(
    'revision',
    JSON.stringify({
      parent_ids: parentID ? [parentID] : [],
      format: 'application/vnd.omnisave.raw-save.v1',
      metadata: {
        label: `Debug revision ${sequence}`,
        source: 'dashboard-debug',
      },
    })
  );
  form.append(
    'payload',
    new Blob([`OmniSave debug payload ${sequence}\n`], { type: 'application/octet-stream' }),
    `debug-${sequence}.sav`
  );

  return request<Revision>(`/api/v1/omnisaves/${omniSaveID}/revisions`, token, {
    method: 'POST',
    body: form,
  });
}
