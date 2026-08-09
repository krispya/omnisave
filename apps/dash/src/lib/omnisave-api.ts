export type Omnisave = {
  id: string;
  game_id: string;
  display_name: string;
  current_revision_id: string | null;
  forked_from?: {
    omnisave_id: string;
    revision_id: string;
  };
  created_at: string;
  /** Original creation time of the snapshot selected as current. */
  current_revision_created_at: string;
  metadata?: Record<string, string>;
};

export type Revision = {
  id: string;
  omnisave_id: string;
  display_name: string;
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

export class CurrentRevisionConflictError extends Error {
  expectedCurrentRevisionID: string | null;
  actualCurrentRevisionID: string | null;

  constructor(input: {
    expected_current_revision_id: string | null;
    actual_current_revision_id: string | null;
  }) {
    super(`The save moved to ${input.actual_current_revision_id?.slice(0, 8) ?? 'no revision'}.`);
    this.name = 'CurrentRevisionConflictError';
    this.expectedCurrentRevisionID = input.expected_current_revision_id;
    this.actualCurrentRevisionID = input.actual_current_revision_id;
  }
}

type ErrorResponse = {
  error?: string;
  reason?: string;
  expected_current_revision_id?: string | null;
  actual_current_revision_id?: string | null;
};

/** Why the server refused to delete a revision, in the dialog's voice. */
function revisionInUseMessage(reason?: string) {
  switch (reason) {
    case 'current':
      return 'This revision is current. Restore another revision first.';
    case 'children':
      return 'Later revisions build on this one. Delete them first.';
    case 'fork_origin':
      return 'A fork begins at this revision.';
    default:
      return 'This revision is still needed.';
  }
}

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
  /**
   * `cover` is the portrait box art; `artwork` and `screenshot` are the
   * landscape images. Which provider supplies which varies — IGDB has artwork,
   * Hasheous has screenshots — so anything wanting a wide image should take
   * either rather than insisting on one.
   */
  kind: 'cover' | 'artwork' | 'screenshot';
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
  playing?: boolean;
  playing_reported_at?: string;
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

export type ServerEvent = {
  id: string;
  type: string;
  data: string;
};

/**
 * The server does not recognize this browser's credential — it was revoked, or
 * it belongs to a server that no longer exists behind this address. Callers
 * treat it as "start over" rather than as an error to display, because there
 * is nothing the reader can do about it except sign in again.
 */
export class UnauthorizedError extends Error {
  constructor() {
    super('This browser is no longer signed in.');
    this.name = 'UnauthorizedError';
  }
}

export class EventStreamAuthError extends UnauthorizedError {
  constructor() {
    super();
    this.name = 'EventStreamAuthError';
  }
}

export function createServerEventParser(onEvent: (event: ServerEvent) => void, initialEventID = '') {
  let buffer = '';
  let lastEventID = initialEventID;
  let eventType = '';
  let data = '';

  function processLine(line: string) {
    if (line === '') {
      if (data) {
        onEvent({
          id: lastEventID,
          type: eventType || 'message',
          data: data.endsWith('\n') ? data.slice(0, -1) : data,
        });
      }
      eventType = '';
      data = '';
      return;
    }
    if (line.startsWith(':')) return;

    const separator = line.indexOf(':');
    const field = separator === -1 ? line : line.slice(0, separator);
    let value = separator === -1 ? '' : line.slice(separator + 1);
    if (value.startsWith(' ')) value = value.slice(1);
    if (field === 'id' && !value.includes('\0')) lastEventID = value;
    else if (field === 'event') eventType = value;
    else if (field === 'data') data += `${value}\n`;
  }

  return {
    push(chunk: string) {
      buffer += chunk;
      let newline = buffer.indexOf('\n');
      while (newline !== -1) {
        const line = buffer.slice(0, newline).replace(/\r$/, '');
        buffer = buffer.slice(newline + 1);
        processLine(line);
        newline = buffer.indexOf('\n');
      }
    },
    finish() {
      if (buffer) processLine(buffer.replace(/\r$/, ''));
      processLine('');
      buffer = '';
    },
    lastEventID() {
      return lastEventID;
    },
  };
}

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
    if (response.status === 409 && body?.error === 'current_revision_conflict') {
      throw new CurrentRevisionConflictError({
        expected_current_revision_id: body.expected_current_revision_id ?? null,
        actual_current_revision_id: body.actual_current_revision_id ?? null,
      });
    }
    if (response.status === 409 && body?.error === 'revision_in_use') {
      throw new Error(revisionInUseMessage(body.reason));
    }
    if (response.status === 401) throw new UnauthorizedError();
    throw new Error(`Request failed (${response.status}).`);
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

export async function streamServerEvents(
  token: string,
  options: {
    signal: AbortSignal;
    lastEventID?: string;
    onOpen: () => void;
    onEvent: (event: ServerEvent) => void;
  }
) {
  const headers = new Headers({
    Accept: 'text/event-stream',
    Authorization: `Bearer ${token}`,
  });
  if (options.lastEventID) headers.set('Last-Event-ID', options.lastEventID);

  const response = await fetch(`${apiBaseURL}/api/v1/events`, {
    signal: options.signal,
    headers,
  });
  if (response.status === 401) throw new EventStreamAuthError();
  if (!response.ok || !response.body) {
    throw new Error(`Event stream failed (${response.status}).`);
  }

  options.onOpen();
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  const parser = createServerEventParser(options.onEvent, options.lastEventID);

  for (;;) {
    const { done, value } = await reader.read();
    parser.push(decoder.decode(value, { stream: !done }));
    if (done) {
      parser.finish();
      return parser.lastEventID();
    }
  }
}

export function listOmnisaves(token: string, signal?: AbortSignal) {
  return request<Omnisave[]>('/api/v1/omnisaves', token, { signal });
}

export function listGames(token: string, signal?: AbortSignal) {
  return request<CatalogGame[]>('/api/v1/games', token, { signal });
}

/** One device's live playing report, as served by the presence listing. */
export type DevicePresence = {
  device_id: string;
  playing_game_ids: string[];
  reported_at: string;
};

export function listPresence(token: string, signal?: AbortSignal) {
  return request<{ devices: DevicePresence[] }>('/api/v1/presence', token, { signal });
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

export function downloadOmnisaveArchive(token: string, omnisaveID: string) {
  return requestBlob(`/api/v1/omnisaves/${omnisaveID}/archive`, token);
}

export function downloadRevisionArchive(token: string, omnisaveID: string, revisionID: string) {
  return requestBlob(`/api/v1/omnisaves/${omnisaveID}/revisions/${revisionID}/archive`, token);
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

export function updateRevisionDisplayName(
  token: string,
  omnisaveID: string,
  revisionID: string,
  displayName: string
) {
  return request<Revision>(`/api/v1/omnisaves/${omnisaveID}/revisions/${revisionID}`, token, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ display_name: displayName }),
  });
}

export function deleteRevision(token: string, omnisaveID: string, revisionID: string) {
  return request<void>(`/api/v1/omnisaves/${omnisaveID}/revisions/${revisionID}`, token, {
    method: 'DELETE',
  });
}

export function commitRevision(
  token: string,
  omnisaveID: string,
  input: {
    expectedCurrentRevisionID: string | null;
    upserts?: CommitFile[];
    deletes?: string[];
    metadata?: Record<string, string>;
  }
) {
  return request<Revision>(`/api/v1/omnisaves/${omnisaveID}/revisions`, token, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      expected_current_revision_id: input.expectedCurrentRevisionID,
      upserts: input.upserts,
      deletes: input.deletes,
      metadata: input.metadata,
    }),
  });
}

export function restoreRevision(
  token: string,
  omnisaveID: string,
  revisionID: string,
  expectedCurrentRevisionID: string | null
) {
  return request<Omnisave>(`/api/v1/omnisaves/${omnisaveID}/current-revision`, token, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      revision_id: revisionID,
      expected_current_revision_id: expectedCurrentRevisionID,
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

/**
 * Access: the credentials this server has issued and the pairing requests that
 * mint them. The Dash is not privileged here — it holds an issued credential
 * like any Device, and can be revoked like one (ADR-007).
 */

export type PairingRequest = {
  id: string;
  /**
   * What the owner matches against the Device's screen. The name and identity
   * in a request are minted by the client and the address is worth what its
   * network path is worth, so this is the only part that ties a request to the
   * Device that sent it.
   */
  code: string;
  device_id: string;
  device_name: string;
  platform?: string;
  source_address: string;
  status: 'pending' | 'approved' | 'denied';
  created_at: string;
  expires_at: string;
};

export type Credential = {
  id: string;
  kind: 'device' | 'dash';
  device_id?: string;
  device_name: string;
  created_at: string;
  last_used_at?: string;
  revoked_at?: string;
};

export type IssuedCredential = {
  credential: Credential;
  token: string;
};

export type OwnerSetting = {
  key: string;
  group: string;
  summary: string;
  kind: 'toggle' | 'text' | 'secret';
  /** Toggles only. */
  value: boolean;
  /** Text only. A secret's value is never sent — see `configured`. */
  text: string;
  configured: boolean;
  source: 'default' | 'owner' | 'deployment';
  editable: boolean;
  env_var: string;
};

/**
 * Whether this server has never been claimed, and can be claimed from here.
 * Asked without a credential, because a browser that has one never asks.
 */
export type ServerAccess = { claimable: boolean; pinSet: boolean };

export async function serverAccess(signal?: AbortSignal): Promise<ServerAccess> {
  const response = await fetch(`${apiBaseURL}/api/v1/claim`, { signal });
  if (!response.ok) return { claimable: false, pinSet: false };
  const status = (await response.json()) as { claimable?: boolean; pin_set?: boolean };
  return { claimable: status.claimable === true, pinSet: status.pin_set === true };
}

/** Claims an unclaimed server, setting the PIN and minting this credential. */
export async function claimServer(name: string, pin: string) {
  return firstCredential('/api/v1/claim', { name, pin }, (status) =>
    status === 409
      ? 'This server has already been claimed. Sign in with its PIN instead.'
      : 'That PIN is not four digits.'
  );
}

/** Signs in with the owner PIN, which mints this browser's own credential. */
export async function signIn(name: string, pin: string) {
  return firstCredential('/api/v1/session', { name, pin }, (status, retryAfter) =>
    status === 429
      ? `Too many attempts. Try again in ${retryAfter ?? 60} seconds.`
      : 'That is not the PIN.'
  );
}

/** Changes the owner PIN. Only something already signed in may. */
export function setPIN(token: string, pin: string) {
  return request<void>('/api/v1/pin', token, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ pin }),
  });
}

async function firstCredential(
  path: string,
  body: { name: string; pin: string },
  describe: (status: number, retryAfter?: number) => string
) {
  const response = await fetch(`${apiBaseURL}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!response.ok) {
    const details = (await response.json().catch(() => null)) as { retry_after?: number } | null;
    throw new Error(describe(response.status, details?.retry_after));
  }
  return (await response.json()) as IssuedCredential;
}

/** Trades the owner token for a credential of this browser's own. */
export function exchangeOwnerToken(ownerToken: string, name: string) {
  return request<IssuedCredential>('/api/v1/credentials/exchange', ownerToken, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  });
}

export function listPairingRequests(token: string, signal?: AbortSignal) {
  return request<PairingRequest[]>('/api/v1/pairing/requests', token, { signal });
}

export function approvePairingRequest(token: string, id: string) {
  return request<void>(`/api/v1/pairing/requests/${encodeURIComponent(id)}/approve`, token, {
    method: 'POST',
  });
}

export function denyPairingRequest(token: string, id: string) {
  return request<void>(`/api/v1/pairing/requests/${encodeURIComponent(id)}/deny`, token, {
    method: 'POST',
  });
}

export function listCredentials(token: string, signal?: AbortSignal) {
  return request<Credential[]>('/api/v1/credentials', token, { signal });
}

export function revokeCredential(token: string, id: string) {
  return request<void>(`/api/v1/credentials/${encodeURIComponent(id)}`, token, {
    method: 'DELETE',
  });
}

export function listSettings(token: string, signal?: AbortSignal) {
  return request<OwnerSetting[]>('/api/v1/settings', token, { signal });
}

/** Stores one owner setting. Everything travels as text, secrets included. */
export function updateSetting(token: string, key: string, value: string) {
  return request<OwnerSetting>(`/api/v1/settings/${encodeURIComponent(key)}`, token, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ value }),
  });
}
