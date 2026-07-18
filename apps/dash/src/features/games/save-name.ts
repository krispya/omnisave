import type { Omnisave } from '../../lib/omnisave-api.js';

export function defaultSaveName(index: number) {
  return `Save ${index + 1}`;
}

export function displaySaveName(save: Pick<Omnisave, 'display_name'>, fallback: string) {
  return save.display_name?.trim() || fallback;
}
