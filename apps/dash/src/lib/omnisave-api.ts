export type OmniSave = {
  id: string;
  game_id: string;
  display_name: string;
  head_revision_id: string | null;
  forked_from?: {
    omnisave_id: string;
    revision_id: string;
  };
  created_at: string;
  metadata?: Record<string, string>;
};

export type Revision = {
  id: string;
  omnisave_id: string;
  parent_id: string | null;
  created_at: string;
  files: Array<{
    path: string;
    artifact: Artifact;
  }>;
  metadata?: Record<string, string>;
};

export type Artifact = {
  format: string;
  sha256: string;
  size: number;
};

export class HeadConflictError extends Error {
  expectedHeadID: string | null;
  actualHeadID: string | null;

  constructor(input: { expected_head_id: string | null; actual_head_id: string | null }) {
    super(`The save moved to ${input.actual_head_id?.slice(0, 8) ?? 'no revision'}.`);
    this.name = 'HeadConflictError';
    this.expectedHeadID = input.expected_head_id;
    this.actualHeadID = input.actual_head_id;
  }
}

type ErrorResponse = {
  error?: string;
  expected_head_id?: string | null;
  actual_head_id?: string | null;
};

type CommitFile = {
  path: string;
  artifact: {
    format: string;
    sha256: string;
    size: number;
  };
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
    const body = (await response.json().catch(() => null)) as ErrorResponse | null;
    if (response.status === 409 && body?.error === 'head_conflict') {
      throw new HeadConflictError({
        expected_head_id: body.expected_head_id ?? null,
        actual_head_id: body.actual_head_id ?? null,
      });
    }
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
    displayName?: string;
    metadata?: Record<string, string>;
  }
) {
  return request<OmniSave>('/api/v1/omnisaves', token, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      game_id: input.gameID,
      display_name: input.displayName,
      metadata: input.metadata,
    }),
  });
}

export function listRevisions(token: string, omniSaveID: string, signal?: AbortSignal) {
  return request<Revision[]>(`/api/v1/omnisaves/${omniSaveID}/revisions`, token, { signal });
}

export function commitRevision(
  token: string,
  omniSaveID: string,
  input: {
    expectedHeadID: string | null;
    upserts?: CommitFile[];
    deletes?: string[];
    metadata?: Record<string, string>;
  }
) {
  return request<Revision>(`/api/v1/omnisaves/${omniSaveID}/revisions`, token, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      expected_head_id: input.expectedHeadID,
      upserts: input.upserts,
      deletes: input.deletes,
      metadata: input.metadata,
    }),
  });
}

export function forkOmniSave(
  token: string,
  omniSaveID: string,
  input: { revisionID: string; displayName?: string; metadata?: Record<string, string> }
) {
  return request<{ omnisave: OmniSave; revision: Revision }>(
    `/api/v1/omnisaves/${omniSaveID}/forks`,
    token,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        revision_id: input.revisionID,
        display_name: input.displayName,
        metadata: input.metadata,
      }),
    }
  );
}

export async function uploadArtifact(token: string, payload: Blob): Promise<Artifact> {
  const digest = await crypto.subtle.digest('SHA-256', await payload.arrayBuffer());
  const sha256 = Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, '0')
  ).join('');
  const format = payload.type || 'application/octet-stream';
  await request<void>(`/api/v1/artifacts/${sha256}`, token, {
    method: 'PUT',
    headers: { 'Content-Type': format },
    body: payload,
  });
  return { format, sha256, size: payload.size };
}
