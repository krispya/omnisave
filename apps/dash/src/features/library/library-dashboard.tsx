import { use, useEffect, useMemo, useState } from 'react';
import {
  deleteGame,
  deleteOmnisave,
  forkOmnisave,
  restoreRevision,
  CurrentRevisionConflictError,
  updateOmnisaveDisplayName,
  type CatalogGame,
  type Omnisave,
  type Revision,
} from '../../lib/omnisave-api.js';
import { DeleteGameDialog, DeleteGameSavesDialog } from '../game/delete-game-dialog.js';
import { GameDetail } from '../game/game-detail.js';
import type { GameSummary } from '../game/game-summary.js';
import { DeleteSaveDialog } from '../omnisave/delete-save-dialog.js';
import {
  downloadAllRevisionsToDisk,
  downloadRevisionToDisk,
  downloadSaveToDisk,
} from '../omnisave/save-archive.js';
import { buildLibrary } from './build-library.js';
import { FixMatchDialog } from './fix-match-dialog.js';
import { GameLibrary } from './game-library.js';
import { LibrarySortControl, sortLibrary, storedLibrarySort } from './library-sort.js';
import { NowPlaying } from './now-playing.js';
import type { LibraryResource, LibrarySnapshot } from './use-library.js';

type DeleteTarget =
  | { type: 'game'; game: GameSummary }
  | { type: 'game-saves'; game: GameSummary }
  | { type: 'save'; game: GameSummary; save: Omnisave; name: string };

function upsertCatalogGame(catalog: CatalogGame[], game: CatalogGame) {
  return catalog.some((candidate) => candidate.id === game.id)
    ? catalog.map((candidate) => (candidate.id === game.id ? game : candidate))
    : [...catalog, game];
}

type LibraryDashboardProps = {
  token: string;
  resource: LibraryResource;
  /** A reload is rendering behind what is shown. */
  libraryPending: boolean;
  /** The Game the route opens, or empty for the Library grid. */
  selectedGameID: string;
  onCloseGame: () => void;
  onReload: () => Promise<LibrarySnapshot>;
  /** Shows a snapshot this view already updated, without a reload. */
  onReplace: (snapshot: LibrarySnapshot) => void;
  /** Reports each snapshot once it is on screen. */
  onSnapshot: (snapshot: LibrarySnapshot) => void;
};

/** The Library grid, or one Game's detail when the route names it. Suspends until the Library loads. */
export function LibraryDashboard({
  token,
  resource,
  libraryPending,
  selectedGameID,
  onCloseGame,
  onReload,
  onReplace,
  onSnapshot,
}: LibraryDashboardProps) {
  const snapshot = use(resource.promise);
  const { catalog, saves } = snapshot;
  // The open save belongs to the game being read, not to the address bar.
  const [selectedSaveID, setSelectedSaveID] = useState('');
  const [error, setError] = useState('');
  const [revisionError, setRevisionError] = useState('');
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget>();
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState('');
  const [fixMatchTarget, setFixMatchTarget] = useState<GameSummary>();
  const [librarySort, setLibrarySort] = useState(storedLibrarySort);

  const games = useMemo(() => buildLibrary(catalog, saves), [catalog, saves]);
  // The sort rearranges only the grid; Playing now keeps its own order.
  const sortedGames = useMemo(() => sortLibrary(games, librarySort), [games, librarySort]);
  const selectedGame = useMemo(
    () => games.find((game) => game.id === selectedGameID),
    [games, selectedGameID]
  );
  const selectedSave = useMemo(
    () => selectedGame?.saves.find((save) => save.id === selectedSaveID),
    [selectedGame, selectedSaveID]
  );

  useEffect(() => onSnapshot(snapshot), [onSnapshot, snapshot]);

  // History and revision-action errors belong to the selected save.
  useEffect(() => setRevisionError(''), [selectedSaveID]);

  // A link can name a game that is gone, and a save can be deleted while it is open.
  useEffect(() => {
    if (selectedGameID && !games.some((game) => game.id === selectedGameID)) onCloseGame();
    else if (selectedSaveID && !saves.some((save) => save.id === selectedSaveID)) {
      setSelectedSaveID('');
    }
  }, [games, onCloseGame, saves, selectedGameID, selectedSaveID]);

  async function downloadSave(save: Omnisave, name: string) {
    if (!token) return;
    setError('');
    try {
      await downloadSaveToDisk(token, save, name);
    } catch (downloadError) {
      setError(
        downloadError instanceof Error ? downloadError.message : 'Could not download this save.'
      );
    }
  }

  async function downloadRevision(save: Omnisave, name: string, revision: Revision) {
    if (!token) return;
    setRevisionError('');
    try {
      await downloadRevisionToDisk(token, save, name, revision);
    } catch (downloadError) {
      setRevisionError(
        downloadError instanceof Error ? downloadError.message : 'Could not download this revision.'
      );
    }
  }

  async function downloadAllRevisions(save: Omnisave, name: string) {
    if (!token) return;
    setError('');
    try {
      await downloadAllRevisionsToDisk(token, save, name);
    } catch (downloadError) {
      setError(
        downloadError instanceof Error
          ? downloadError.message
          : 'Could not download this save history.'
      );
    }
  }

  async function renameSave(save: Omnisave, displayName: string) {
    if (!token) return;
    const updated = await updateOmnisaveDisplayName(token, save.id, displayName);
    onReplace({
      catalog,
      saves: saves.map((candidate) => (candidate.id === updated.id ? updated : candidate)),
      error: '',
    });
  }

  async function restoreSaveRevision(save: Omnisave, revision: Revision) {
    if (!token) return;
    try {
      await restoreRevision(token, save.id, revision.id, save.current_revision_id);
      await onReload();
      setSelectedSaveID(save.id);
    } catch (restoreError) {
      if (restoreError instanceof CurrentRevisionConflictError) await onReload();
      throw restoreError;
    }
  }

  async function forkSaveAtRevision(save: Omnisave, revision: Revision, displayName: string) {
    if (!token) return;
    const result = await forkOmnisave(token, save.id, {
      revisionID: revision.id,
      displayName,
    });
    await onReload();
    setSelectedSaveID(result.omnisave.id);
  }

  function requestDeleteGame(game: GameSummary) {
    setDeleteError('');
    setDeleteTarget({ type: 'game', game });
  }

  function requestDeleteGameSaves(game: GameSummary) {
    setDeleteError('');
    setDeleteTarget({ type: 'game-saves', game });
  }

  function requestDeleteSave(save: Omnisave, name: string) {
    if (!selectedGame) return;
    setDeleteError('');
    setDeleteTarget({ type: 'save', game: selectedGame, save, name });
  }

  function cancelDelete() {
    if (deleting) return;
    setDeleteTarget(undefined);
    setDeleteError('');
  }

  async function confirmDelete() {
    if (!token || !deleteTarget) return;

    setDeleting(true);
    setDeleteError('');
    try {
      if (deleteTarget.type === 'game') {
        await deleteGame(token, deleteTarget.game.id);
        if (selectedGameID === deleteTarget.game.id) onCloseGame();
      } else {
        const savesToDelete =
          deleteTarget.type === 'game-saves' ? deleteTarget.game.saves : [deleteTarget.save];
        for (const save of savesToDelete) {
          await deleteOmnisave(token, save.id);
        }

        if (deleteTarget.type === 'save' && selectedSaveID === deleteTarget.save.id) {
          const nextSave = deleteTarget.game.saves.find((save) => save.id !== deleteTarget.save.id);
          setSelectedSaveID(nextSave?.id ?? '');
        }
      }
      setDeleteTarget(undefined);
      await onReload();
    } catch (deleteFailure) {
      setDeleteError(
        deleteFailure instanceof Error
          ? deleteFailure.message
          : deleteTarget.type === 'game'
            ? 'Could not delete this game.'
            : deleteTarget.type === 'game-saves'
              ? 'Could not delete these saves.'
              : 'Could not delete this save.'
      );
    } finally {
      setDeleting(false);
    }
  }

  const visibleError = error || snapshot.error;

  return (
    <>
      {/* The top bar names this section and the rail leads back out, so
          neither the Library nor an open game draws a header or a back link
          of its own. */}
      {visibleError ? (
        <div
          role="alert"
          className="mt-5 rounded-md border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger"
        >
          {visibleError}
        </div>
      ) : null}

      {selectedGame ? (
        <GameDetail
          game={selectedGame}
          token={token}
          selectedSave={selectedSave}
          revisionError={revisionError}
          onSelectSave={(save) => setSelectedSaveID(save?.id ?? '')}
          onDownloadSave={(save, name) => void downloadSave(save, name)}
          onDownloadAllRevisions={(save, name) => void downloadAllRevisions(save, name)}
          onDownloadRevision={(save, name, revision) => void downloadRevision(save, name, revision)}
          onRequestDelete={requestDeleteSave}
          onRenameSave={renameSave}
          onRestoreRevision={restoreSaveRevision}
          onForkRevision={forkSaveAtRevision}
        />
      ) : (
        <div aria-busy={libraryPending}>
          <NowPlaying games={games} token={token} />

          <section aria-label="Game library">
            {games.length > 0 ? (
              <div className="mb-5 flex items-center gap-4">
                <LibrarySortControl sort={librarySort} onSortChange={setLibrarySort} />
                <span
                  className="rounded-md bg-text/8 px-2.5 py-1 text-sm font-medium text-muted"
                  aria-label={`${games.length} ${games.length === 1 ? 'game' : 'games'}`}
                >
                  {games.length}
                </span>
              </div>
            ) : null}
            <GameLibrary
              games={sortedGames}
              token={token}
              onRequestFixMatch={setFixMatchTarget}
              onRequestDeleteSaves={requestDeleteGameSaves}
              onRequestDeleteGame={requestDeleteGame}
            />
          </section>
        </div>
      )}

      {deleteTarget?.type === 'game' ? (
        <DeleteGameDialog
          game={deleteTarget.game}
          deleting={deleting}
          error={deleteError}
          onCancel={cancelDelete}
          onConfirm={() => void confirmDelete()}
        />
      ) : deleteTarget?.type === 'game-saves' ? (
        <DeleteGameSavesDialog
          game={deleteTarget.game}
          deleting={deleting}
          error={deleteError}
          onCancel={cancelDelete}
          onConfirm={() => void confirmDelete()}
        />
      ) : deleteTarget?.type === 'save' ? (
        <DeleteSaveDialog
          name={deleteTarget.name}
          deleting={deleting}
          error={deleteError}
          onCancel={cancelDelete}
          onConfirm={() => void confirmDelete()}
        />
      ) : null}
      {fixMatchTarget ? (
        <FixMatchDialog
          game={fixMatchTarget}
          token={token}
          onCancel={() => setFixMatchTarget(undefined)}
          onMatched={(game) => {
            onReplace({
              catalog: catalog ? upsertCatalogGame(catalog, game) : null,
              saves,
              error: '',
            });
            setFixMatchTarget(undefined);
          }}
        />
      ) : null}
    </>
  );
}
