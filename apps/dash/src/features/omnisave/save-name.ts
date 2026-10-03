import type { Omnisave } from '../../lib/omnisave-api.js';

export function defaultSaveName(index: number) {
  return `Save ${index + 1}`;
}

export function displaySaveName(save: Pick<Omnisave, 'display_name'>, fallback: string) {
  return save.display_name?.trim() || fallback;
}

/** Identify what a history protects, independently of its editable name. */
export function saveScopeLabel(save: Pick<Omnisave, 'scope'>) {
  if (!save.scope?.kind && !save.scope?.adapter) return 'Whole save';
  if (save.scope?.kind === 'slot' && save.scope.adapter) return 'Save slot';
  return 'Unknown save scope';
}
