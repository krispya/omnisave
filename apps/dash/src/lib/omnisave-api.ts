export type OmniSave = {
  id: string;
  game_id: string;
  slot: string;
  display_name: string;
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

export type GameMedia = {
  id: string;
  kind: 'cover' | 'screenshot';
  position: number;
  format: string;
  size: number;
  url: string;
  attribution?: string;
};

export type CatalogGame = {
  id: string;
  title: string;
  sort_title?: string;
  platform?: string;
  publisher?: string;
  description?: string;
  provider: string;
  provider_id: string;
  metadata?: Record<string, unknown>;
  media: GameMedia[];
  refreshed_at: string;
};

export type GameMatchCandidate = {
  provider: string;
  provider_id: string;
  title: string;
  edition?: string;
  platform?: string;
  publisher?: string;
  year?: string;
  region?: string;
  language?: string;
  selection_token: string;
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

  if (response.status === 204) return undefined as T;

  return response.json() as Promise<T>;
}

async function requestBlob(path: string, token: string, signal?: AbortSignal) {
  const response = await fetch(`${apiBaseURL}${path}`, {
    signal,
    headers: { Authorization: `Bearer ${token}` },
  });

  if (!response.ok) throw new Error(`Request failed (${response.status}).`);
  return response.blob();
}

export function listOmniSaves(token: string, signal?: AbortSignal) {
  return request<OmniSave[]>('/api/v1/omnisaves', token, { signal });
}

export function listGames(token: string, signal?: AbortSignal) {
  return request<CatalogGame[]>('/api/v1/games', token, { signal });
}

export function loadGameMedia(token: string, mediaURL: string, signal?: AbortSignal) {
  return requestBlob(mediaURL, token, signal);
}

export function identifyGame(
  token: string,
  input: {
    gameID?: string;
    platform: string;
    crc32?: string;
    md5?: string;
    sha1?: string;
    sha256?: string;
  }
) {
  return request<CatalogGame>('/api/v1/games/identify', token, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      game_id: input.gameID,
      fingerprint: {
        platform: input.platform,
        crc32: input.crc32,
        md5: input.md5,
        sha1: input.sha1,
        sha256: input.sha256,
      },
    }),
  });
}

export function searchGameMatches(
  token: string,
  gameID: string,
  query: string,
  platform?: string,
  signal?: AbortSignal
) {
  const parameters = new URLSearchParams({ q: query, limit: '25' });
  if (platform) parameters.set('platform', platform);
  return request<GameMatchCandidate[]>(
    `/api/v1/games/${encodeURIComponent(gameID)}/match-candidates?${parameters}`,
    token,
    { signal }
  );
}

export function fixGameMatch(token: string, gameID: string, selectionToken: string) {
  return request<CatalogGame>(`/api/v1/games/${encodeURIComponent(gameID)}/match`, token, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ selection_token: selectionToken }),
  });
}

export function deleteOmniSave(token: string, omniSaveID: string) {
  return request<void>(`/api/v1/omnisaves/${omniSaveID}`, token, { method: 'DELETE' });
}

export function updateOmniSaveDisplayName(token: string, omniSaveID: string, displayName: string) {
  return request<OmniSave>(`/api/v1/omnisaves/${omniSaveID}`, token, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ display_name: displayName }),
  });
}

export function createOmniSave(
  token: string,
  input: {
    gameID: string;
    slot: string;
    displayName?: string;
    metadata?: Record<string, string>;
  }
) {
  return request<OmniSave>('/api/v1/omnisaves', token, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      game_id: input.gameID,
      slot: input.slot,
      display_name: input.displayName,
      metadata: input.metadata,
    }),
  });
}

export function listRevisions(token: string, omniSaveID: string, signal?: AbortSignal) {
  return request<Revision[]>(`/api/v1/omnisaves/${omniSaveID}/revisions`, token, { signal });
}

export function createRevision(
  token: string,
  omniSaveID: string,
  input: { parentIDs: string[]; format: string; metadata?: Record<string, string> },
  payload: Blob,
  filename: string
) {
  const form = new FormData();
  form.append(
    'revision',
    JSON.stringify({
      parent_ids: input.parentIDs,
      format: input.format,
      metadata: input.metadata,
    })
  );
  form.append('payload', payload, filename);

  return request<Revision>(`/api/v1/omnisaves/${omniSaveID}/revisions`, token, {
    method: 'POST',
    body: form,
  });
}
