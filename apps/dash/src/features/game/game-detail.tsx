import { useState } from 'react';
import { updateRevisionDisplayName, type Omnisave, type Revision } from '../../lib/omnisave-api.js';
import { GameArtwork, GameMediaImage } from './game-artwork.js';
import type { GameSummary } from './game-summary.js';
import { GameDetailsDialog } from './details-dialog.js';
import type { RevisionFocus } from '../omnisave/revision-log.js';
import { SaveList } from '../omnisave/save-list.js';
import { prefetchSaveRevisions, useSaveRevisions } from '../omnisave/use-save-revisions.js';
import { TrackedDevices } from './tracked-devices.js';
import { RevisionActionDialog } from '../omnisave/revision-action-dialog.js';
import { refreshRevisionAction, type RevisionAction } from '../omnisave/revision-action.js';
import { revisionMove } from '../omnisave/revision-navigation.js';
import { defaultSaveName, displaySaveName } from '../omnisave/save-name.js';

function gameReleaseYear(game: GameSummary) {
  const value = game.metadata?.['release_year'];
  return typeof value === 'string' || typeof value === 'number' ? String(value) : undefined;
}

function gameGenres(game: GameSummary) {
  const value = game.metadata?.['genres'];
  if (Array.isArray(value)) {
    return value.filter((genre): genre is string => typeof genre === 'string');
  }
  return typeof value === 'string' ? [value] : [];
}

type GameDetailProps = {
  game: GameSummary;
  token: string;
  selectedSave?: Omnisave;
  /** Reports what the last revision action did, alongside anything loading turns up. */
  revisionError: string;
  /** Selecting a save opens its history; selecting none closes it. */
  onSelectSave: (save?: Omnisave) => void;
  onDownloadSave: (save: Omnisave, name: string) => void;
  onDownloadRevision: (save: Omnisave, name: string, revision: Revision) => void;
  onRequestDelete: (save: Omnisave, name: string) => void;
  onRenameSave: (save: Omnisave, displayName: string) => Promise<void>;
  onRestoreRevision: (save: Omnisave, revision: Revision) => Promise<void>;
  onForkRevision: (save: Omnisave, revision: Revision, displayName: string) => Promise<void>;
};

export function GameDetail({
  game,
  token,
  selectedSave,
  revisionError,
  onSelectSave,
  onDownloadSave,
  onDownloadRevision,
  onRequestDelete,
  onRenameSave,
  onRestoreRevision,
  onForkRevision,
}: GameDetailProps) {
  const [detailsOpen, setDetailsOpen] = useState(false);
  // Set only when a fork edge was followed, so the log can reveal where it landed.
  const [focus, setFocus] = useState<RevisionFocus>();
  const [revisionAction, setRevisionAction] = useState<RevisionAction>();
  const [revisionActionBusy, setRevisionActionBusy] = useState(false);
  const [revisionActionError, setRevisionActionError] = useState('');
  const history = useSaveRevisions(token, selectedSave);
  // A conflicting restore reloads the library under the open dialog, so the action
  // is re-read from the fresh game every render: a retry then sends the reloaded
  // current pointer and the dialog's verb describes the move from where it is now.
  const activeRevisionAction = revisionAction
    ? refreshRevisionAction(revisionAction, game.saves, history.revisions)
    : undefined;
  const actionSaveIndex = activeRevisionAction
    ? game.saves.findIndex((save) => save.id === activeRevisionAction.save.id)
    : -1;
  const actionSaveName = activeRevisionAction
    ? displaySaveName(
        activeRevisionAction.save,
        actionSaveIndex >= 0 ? defaultSaveName(actionSaveIndex) : 'Save'
      )
    : '';
  const releaseYear = gameReleaseYear(game);
  const genres = gameGenres(game);
  const facts = [
    releaseYear,
    game.publisher,
    genres.length > 0 ? genres.join(', ') : undefined,
  ].filter((fact): fact is string => Boolean(fact));

  async function renameRevision(revision: Revision, displayName: string) {
    const updated = await updateRevisionDisplayName(
      token,
      selectedSave?.id ?? revision.omnisave_id,
      revision.id,
      displayName
    );
    history.replaceRevision(updated);
  }

  // The open save is the selected one, so opening a save closes whichever was open and
  // becomes what the Debug menu and the revision fetch are pointed at.
  function toggleSave(save: Omnisave) {
    setFocus(undefined);
    onSelectSave(save.id === selectedSave?.id ? undefined : save);
  }

  // Following a fork edge opens the save it leads to on that revision.
  function openSaveAtRevision(save: Omnisave, revisionID?: string) {
    setFocus({ saveID: save.id, revisionID });
    onSelectSave(save);
  }

  function requestRestore(save: Omnisave, revision: Revision) {
    setRevisionActionError('');
    setRevisionAction({
      kind: 'restore',
      save,
      revision,
      move: revisionMove(history.revisions, save.current_revision_id, revision.id),
    });
  }

  function requestFork(save: Omnisave, revision: Revision) {
    setRevisionActionError('');
    setRevisionAction({ kind: 'fork', save, revision });
  }

  async function confirmRestore() {
    if (activeRevisionAction?.kind !== 'restore') return;
    setRevisionActionBusy(true);
    setRevisionActionError('');
    try {
      await onRestoreRevision(activeRevisionAction.save, activeRevisionAction.revision);
      setRevisionAction(undefined);
    } catch (error) {
      setRevisionActionError(
        error instanceof Error ? error.message : 'Could not restore this revision.'
      );
    } finally {
      setRevisionActionBusy(false);
    }
  }

  async function confirmFork(displayName: string) {
    if (activeRevisionAction?.kind !== 'fork') return;
    setRevisionActionBusy(true);
    setRevisionActionError('');
    try {
      await onForkRevision(activeRevisionAction.save, activeRevisionAction.revision, displayName);
      setRevisionAction(undefined);
    } catch (error) {
      setRevisionActionError(error instanceof Error ? error.message : 'Could not fork this save.');
    } finally {
      setRevisionActionBusy(false);
    }
  }

  return (
    <div className="mt-8">
      <section className="flex items-end gap-6 border-b border-white/5 pb-6">
        <GameArtwork
          game={game}
          token={token}
          className="aspect-[3/4] w-28 shrink-0 shadow-lg sm:w-36"
        />
        <div className="min-w-0 pb-1">
          {game.platform ? (
            <p className="text-xs font-semibold tracking-[0.16em] text-[#e5a00d] uppercase">
              {game.platform}
            </p>
          ) : null}
          <h2 className="mt-2 text-3xl font-semibold tracking-tight text-white sm:text-4xl">
            {game.label}
          </h2>
          <p className="mt-3 flex flex-wrap items-center gap-x-2.5 gap-y-1 text-sm text-slate-400">
            {facts.map((fact, index) => (
              <span key={fact} className="flex items-center gap-x-2.5">
                {index > 0 ? (
                  <span className="text-slate-600" aria-hidden="true">
                    ·
                  </span>
                ) : null}
                {fact}
              </span>
            ))}
          </p>
          <div className="mt-4">
            <button
              type="button"
              onClick={() => setDetailsOpen(true)}
              className="rounded-md bg-white/5 px-3.5 py-2 text-xs font-medium text-neutral-300 transition hover:bg-white/10 hover:text-white"
            >
              Details
            </button>
          </div>
        </div>
      </section>

      {game.media.some((media) => media.kind === 'screenshot') ? (
        <section className="mt-6" aria-label={`Screenshots for ${game.label}`}>
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            {game.media
              .filter((media) => media.kind === 'screenshot')
              .map((media, index) => (
                <div
                  key={media.id}
                  className="aspect-video overflow-hidden rounded-md bg-white/5 ring-1 ring-white/10"
                >
                  <GameMediaImage
                    token={token}
                    media={media}
                    alt={`${game.label} screenshot ${index + 1}`}
                    className="size-full object-cover"
                  />
                </div>
              ))}
          </div>
        </section>
      ) : null}

      <TrackedDevices provenance={game.provenance} />

      <section className="mt-6" aria-label={`Saves for ${game.label}`}>
        <div className="mb-4 flex items-center justify-between">
          <h3 className="text-sm font-semibold text-white">Saves</h3>
        </div>

        {game.saves.length === 0 ? (
          <div className="rounded-md border border-dashed border-white/10 bg-white/[0.02] px-6 py-14 text-center">
            <p className="text-sm font-medium text-white">This game has no saves</p>
            <p className="mx-auto mt-2 max-w-xs text-xs leading-5 text-slate-500">
              It stays in your library either way. Add one with Debug → New save, or bind a local save
              from a client.
            </p>
          </div>
        ) : (
          <SaveList
            saves={game.saves}
            selectedSave={selectedSave}
            expandedSaveID={selectedSave?.id ?? ''}
            revisions={history.revisions}
            loadingRevisions={history.loading}
            revisionError={revisionError || history.error}
            focus={focus}
            onToggleSave={toggleSave}
            onPrefetchSave={(save) => prefetchSaveRevisions(token, save)}
            onDownloadSave={onDownloadSave}
            onDownloadRevision={onDownloadRevision}
            onRequestDelete={onRequestDelete}
            onRenameSave={onRenameSave}
            onRenameRevision={renameRevision}
            onOpenSave={openSaveAtRevision}
            onRequestRestore={requestRestore}
            onRequestFork={requestFork}
          />
        )}
      </section>

      {detailsOpen ? <GameDetailsDialog game={game} onClose={() => setDetailsOpen(false)} /> : null}
      {activeRevisionAction ? (
        <RevisionActionDialog
          // Remounting on a new target resets the dialog's prefilled name.
          key={`${activeRevisionAction.kind}:${activeRevisionAction.save.id}:${activeRevisionAction.revision.id}`}
          action={activeRevisionAction}
          saveName={actionSaveName}
          busy={revisionActionBusy}
          error={revisionActionError}
          onCancel={() => {
            if (!revisionActionBusy) setRevisionAction(undefined);
          }}
          onRestore={() => void confirmRestore()}
          onFork={(displayName) => void confirmFork(displayName)}
        />
      ) : null}
    </div>
  );
}
