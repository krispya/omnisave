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

  if (response.status === 204) return undefined as T;

  return response.json() as Promise<T>;
}

export function listOmniSaves(token: string, signal?: AbortSignal) {
  return request<OmniSave[]>('/api/v1/omnisaves', token, { signal });
}

export function deleteOmniSave(token: string, omniSaveID: string) {
  return request<void>(`/api/v1/omnisaves/${omniSaveID}`, token, { method: 'DELETE' });
}

export function createOmniSave(
  token: string,
  input: { gameID: string; slot: string; metadata?: Record<string, string> }
) {
  return request<OmniSave>('/api/v1/omnisaves', token, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      game_id: input.gameID,
      slot: input.slot,
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
