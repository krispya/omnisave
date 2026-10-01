/** The browser stores only its issued credential, never the owner token. */
const credentialStorageKey = 'omnisave.credential';

/** This browser's own credential; an empty token means it is signed out. */
export type StoredCredential = { id: string; token: string };

/** The credential kept from the last visit, or a signed-out one. */
export function storedCredential(): StoredCredential {
  try {
    const stored = localStorage.getItem(credentialStorageKey);
    if (!stored) return { id: '', token: '' };
    const parsed = JSON.parse(stored) as Partial<StoredCredential>;
    return { id: parsed.id ?? '', token: parsed.token ?? '' };
  } catch {
    return { id: '', token: '' };
  }
}

/** Keeps a newly issued credential for later visits. Throws if storage refuses it. */
export function storeCredential(credential: StoredCredential) {
  localStorage.setItem(credentialStorageKey, JSON.stringify(credential));
}

/** Forgets the kept credential locally without revoking it on the server. */
export function forgetStoredCredential() {
  localStorage.removeItem(credentialStorageKey);
}
