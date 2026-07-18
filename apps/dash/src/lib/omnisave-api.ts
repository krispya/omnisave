export type Omnisave = {
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
  platform_company?: string;
  publisher?: string;
  description?: string;
  metadata_source: string;
  identifiers: GameIdentifier[];
  fingerprints: GameFingerprint[];
  metadata?: Record<string, unknown>;
  media: GameMedia[];
  provenance: GameProvenance[];
  refreshed_at: string;
};

export type GameProvenance = {
  device_id: string;
  device_name: string;
  adapter?: string;
  installed: boolean;
  first_tracked_at: string;
  last_seen_at: string;
  untracked_at?: string;
};

export type GameIdentifier = {
  namespace: string;
  value: string;
};

export type GameFingerprint = {
  platform: string;
  algorithm: 'crc32' | 'md5' | 'sha1' | 'sha256';
  value: string;
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

export function listOmnisaves(token: string, signal?: AbortSignal) {
  return request<Omnisave[]>('/api/v1/omnisaves', token, { signal });
}

export function listGames(token: string, signal?: AbortSignal) {
  return request<CatalogGame[]>('/api/v1/games', token, { signal });
}

export function loadGameMedia(token: string, mediaURL: string, signal?: AbortSignal) {
  return requestBlob(mediaURL, token, signal);
}

export function resolveGame(
  token: string,
  input: {
    identifiers?: GameIdentifier[];
    fingerprints?: GameFingerprint[];
    titleHint?: string;
    platformHint?: string;
  }
) {
  return request<{ game: CatalogGame; status: 'existing' | 'created' }>(
    '/api/v1/games/resolve',
    token,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        identifiers: input.identifiers,
        fingerprints: input.fingerprints,
        title_hint: input.titleHint,
        platform_hint: input.platformHint,
      }),
    }
  );
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

export function deleteOmnisave(token: string, omnisaveID: string) {
  return request<void>(`/api/v1/omnisaves/${omnisaveID}`, token, { method: 'DELETE' });
}

export function deleteGame(token: string, gameID: string) {
  return request<void>(`/api/v1/games/${encodeURIComponent(gameID)}`, token, { method: 'DELETE' });
}

export function updateOmnisaveDisplayName(token: string, omnisaveID: string, displayName: string) {
  return request<Omnisave>(`/api/v1/omnisaves/${omnisaveID}`, token, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ display_name: displayName }),
  });
}

export function createOmnisave(
  token: string,
  input: {
    gameID: string;
    displayName?: string;
    metadata?: Record<string, string>;
  }
) {
  return request<Omnisave>('/api/v1/omnisaves', token, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      game_id: input.gameID,
      display_name: input.displayName,
      metadata: input.metadata,
    }),
  });
}

export function listRevisions(token: string, omnisaveID: string, signal?: AbortSignal) {
  return request<Revision[]>(`/api/v1/omnisaves/${omnisaveID}/revisions`, token, { signal });
}

export function commitRevision(
  token: string,
  omnisaveID: string,
  input: {
    expectedHeadID: string | null;
    upserts?: CommitFile[];
    deletes?: string[];
    metadata?: Record<string, string>;
  }
) {
  return request<Revision>(`/api/v1/omnisaves/${omnisaveID}/revisions`, token, {
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

export function forkOmnisave(
  token: string,
  omnisaveID: string,
  input: { revisionID: string; displayName?: string; metadata?: Record<string, string> }
) {
  return request<{ omnisave: Omnisave; revision: Revision }>(
    `/api/v1/omnisaves/${omnisaveID}/forks`,
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
