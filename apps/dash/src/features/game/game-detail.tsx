import { useState } from 'react';
import { Button } from '../../components/button.js';
import {
  deleteRevision,
  labelRevision,
  updateRevisionDisplayName,
  type Omnisave,
  type Revision,
} from '../../lib/omnisave-api.js';
import { DeleteRevisionDialog } from '../omnisave/delete-revision-dialog.js';
import { GameArtwork, GameMediaImage } from './game-artwork.js';
import { playingOn, type GameSummary } from './game-summary.js';
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
  const [deleteRevisionTarget, setDeleteRevisionTarget] = useState<{
    save: Omnisave;
    revision: Revision;
  }>();
  const [deletingRevision, setDeletingRevision] = useState(false);
  const [deleteRevisionError, setDeleteRevisionError] = useState('');
  const [labelRevisionError, setLabelRevisionError] = useState('');
  const history = useSaveRevisions(token, selectedSave);
  // Re-derive the action after reloads so retries use the latest current pointer.
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

  async function runRevisionLabeler(revision: Revision) {
    setLabelRevisionError('');
    try {
      const updated = await labelRevision(
        token,
        selectedSave?.id ?? revision.omnisave_id,
        revision.id
      );
      history.replaceRevision(updated);
    } catch (error) {
      setLabelRevisionError(
        error instanceof Error ? error.message : 'Could not run the labeler on this revision.'
      );
    }
  }

  // Selection controls both the open history and its revision fetch.
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

  function requestDeleteRevision(save: Omnisave, revision: Revision) {
    setDeleteRevisionError('');
    setDeleteRevisionTarget({ save, revision });
  }

  async function confirmDeleteRevision() {
    if (!deleteRevisionTarget) return;
    setDeletingRevision(true);
    setDeleteRevisionError('');
    try {
      await deleteRevision(token, deleteRevisionTarget.save.id, deleteRevisionTarget.revision.id);
      history.removeRevision(deleteRevisionTarget.revision.id);
      setDeleteRevisionTarget(undefined);
    } catch (error) {
      setDeleteRevisionError(
        error instanceof Error ? error.message : 'Could not delete this revision.'
      );
    } finally {
      setDeletingRevision(false);
    }
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
    <div>
      {/* The identity block: the poster and, beside it, everything that says
          what this game is — name, facts, the Details door, the devices
          tracking it. Nothing here launches anything; the Dash manages saves
          rather than play, so this is a header, not a landing pad. */}
      <section className="flex flex-col gap-6 sm:flex-row sm:gap-8">
        {/* self-start keeps the flex row from stretching this column to the
            metadata column's height — a stretched height would override the
            poster's aspect ratio, which only holds while one dimension stays
            free. */}
        <div className="w-40 shrink-0 self-start sm:w-44 lg:w-52">
          <GameArtwork
            game={game}
            token={token}
            className="aspect-[3/4] w-full ring-1 ring-outline"
          />
          <div className="mt-4">
            <Button variant="outline" className="w-full" onClick={() => setDetailsOpen(true)}>
              Details
            </Button>
          </div>
        </div>
        <div className="min-w-0 flex-1">
          {game.platform ? (
            <p className="text-xs font-semibold tracking-[0.16em] text-muted uppercase">
              {game.platform}
            </p>
          ) : null}
          <h2 className="mt-2 text-3xl font-semibold tracking-tight text-text sm:text-4xl">
            {game.label}
          </h2>
          {facts.length > 0 ? (
            <p className="mt-3 flex flex-wrap items-center gap-x-2.5 gap-y-1 text-sm text-muted">
              {facts.map((fact, index) => (
                <span key={fact} className="flex items-center gap-x-2.5">
                  {index > 0 ? (
                    <span className="text-text/25" aria-hidden="true">
                      ·
                    </span>
                  ) : null}
                  {fact}
                </span>
              ))}
            </p>
          ) : null}
          <TrackedDevices provenance={game.provenance} />
        </div>
      </section>

      {/* The saves take the full width below: they are what this page is for,
          and the revision history inside them is the widest thing on it. */}
      <section className="mt-10" aria-label={`Saves for ${game.label}`}>
        <h3 className="mb-4 text-xs font-semibold tracking-wide text-muted uppercase">Saves</h3>

        {game.saves.length === 0 ? (
          <div className="rounded-lg border border-dashed border-outline px-6 py-14 text-center">
            <p className="text-sm font-medium text-text">This game has no saves</p>
            <p className="mx-auto mt-2 max-w-xs text-xs leading-5 text-muted">
              It stays in your library either way. Bind a local save from a client to start tracking
              one.
            </p>
          </div>
        ) : (
          <SaveList
            saves={game.saves}
            selectedSave={selectedSave}
            expandedSaveID={selectedSave?.id ?? ''}
            revisions={history.revisions}
            achievements={history.achievements}
            loadingRevisions={history.loading}
            revisionError={revisionError || labelRevisionError || history.error}
            labelerAvailable={game.labelerAvailable}
            focus={focus}
            onToggleSave={toggleSave}
            onPrefetchSave={(save) => prefetchSaveRevisions(token, save)}
            onDownloadSave={onDownloadSave}
            onDownloadRevision={onDownloadRevision}
            onRequestDelete={onRequestDelete}
            onRenameSave={onRenameSave}
            onRenameRevision={renameRevision}
            onRunLabeler={runRevisionLabeler}
            onOpenSave={openSaveAtRevision}
            onRequestRestore={requestRestore}
            onRequestFork={requestFork}
            onRequestDeleteRevision={requestDeleteRevision}
          />
        )}
      </section>

      {game.media.some((media) => media.kind === 'screenshot') ? (
        <section className="mt-10" aria-label={`Screenshots for ${game.label}`}>
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            {game.media
              .filter((media) => media.kind === 'screenshot')
              .map((media, index) => (
                <div
                  key={media.id}
                  className="aspect-video overflow-hidden rounded-sm bg-text/5 ring-1 ring-outline"
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

      {detailsOpen ? <GameDetailsDialog game={game} onClose={() => setDetailsOpen(false)} /> : null}
      {deleteRevisionTarget ? (
        <DeleteRevisionDialog
          name={
            deleteRevisionTarget.revision.display_name || deleteRevisionTarget.revision.id.slice(0, 8)
          }
          deleting={deletingRevision}
          error={deleteRevisionError}
          onCancel={() => {
            if (!deletingRevision) setDeleteRevisionTarget(undefined);
          }}
          onConfirm={() => void confirmDeleteRevision()}
        />
      ) : null}
      {activeRevisionAction ? (
        <RevisionActionDialog
          // Remounting on a new target resets the dialog's prefilled name.
          key={`${activeRevisionAction.kind}:${activeRevisionAction.save.id}:${activeRevisionAction.revision.id}`}
          action={activeRevisionAction}
          saveName={actionSaveName}
          playingDevices={playingOn(game).map((record) => record.device_name)}
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
