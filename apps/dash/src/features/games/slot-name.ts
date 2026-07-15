import type { OmniSave } from '../../lib/omnisave-api.js';

function slotNumber(slot: string) {
  if (slot === 'default') return 1;
  const match = /^slot-(\d+)$/.exec(slot);
  return match ? Number(match[1]) : undefined;
}

export function formatSlotName(slot: string) {
  const number = slotNumber(slot);
  return number ? `Slot ${number}` : slot;
}

export function displaySlotName(save: Pick<OmniSave, 'slot' | 'display_name'>) {
  return save.display_name?.trim() || formatSlotName(save.slot);
}

export function nextSlotName(saves: Pick<OmniSave, 'slot'>[]) {
  const usedNumbers = new Set(saves.map((save) => slotNumber(save.slot)));
  let number = 1;
  while (usedNumbers.has(number)) number += 1;
  return `slot-${number}`;
}
