import type { Omnisave, Revision } from '../../lib/omnisave-api.js';
import { revisionMove, type RevisionMove } from './revision-navigation.js';

/** A pointer move or fork awaiting confirmation in the revision action dialog. */
export type RevisionAction =
  | { kind: 'restore'; save: Omnisave; revision: Revision; move: RevisionMove }
  | { kind: 'fork'; save: Omnisave; revision: Revision };

/** The save as the freshest library snapshot knows it, or the captured copy if it is gone. */
export function resolveFreshSave(saves: Omnisave[], save: Omnisave): Omnisave {
  return saves.find((candidate) => candidate.id === save.id) ?? save;
}

/**
 * Re-derives a pending action from the freshest library data. A conflicting
 * restore reloads the library while the dialog stays open, so the retry has to
 * send the reloaded current pointer — not the one captured when the dialog
 * opened — and the dialog's verb has to describe the move from there.
 */
export function refreshRevisionAction(
  action: RevisionAction,
  saves: Omnisave[],
  revisions: Revision[]
): RevisionAction {
  const save = resolveFreshSave(saves, action.save);
  if (action.kind === 'fork') return save === action.save ? action : { ...action, save };
  return {
    ...action,
    save,
    move: revisionMove(revisions, save.current_revision_id, action.revision.id),
  };
}
