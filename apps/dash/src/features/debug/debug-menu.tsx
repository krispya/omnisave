import type { Omnisave } from '../../lib/omnisave-api.js';
import { useDismissibleDetails } from '../../lib/use-dismissible-details.js';
import type { GameSummary } from '../game/game-summary.js';
import { defaultSaveName, displaySaveName } from '../omnisave/save-name.js';

type DebugAction = 'game' | 'save' | 'revision' | 'fork' | null;

type DebugMenuProps = {
  game?: GameSummary;
  selectedSave?: Omnisave;
  action: DebugAction;
  revisionHistoryAvailable: boolean;
  canFork: boolean;
  onAddRandomGame: () => void;
  onAddSave: () => void;
  onAddRevision: () => void;
  onForkSave: () => void;
};

export function DebugMenu({
  game,
  selectedSave,
  action,
  revisionHistoryAvailable,
  canFork,
  onAddRandomGame,
  onAddSave,
  onAddRevision,
  onForkSave,
}: DebugMenuProps) {
  const menu = useDismissibleDetails();
  const selectedSaveIndex = selectedSave
    ? (game?.saves.findIndex((save) => save.id === selectedSave.id) ?? -1)
    : -1;
  const selectedSaveName = selectedSave
    ? displaySaveName(selectedSave, defaultSaveName(Math.max(selectedSaveIndex, 0)))
    : '';

  function run(callback: () => void) {
    menu.current?.removeAttribute('open');
    callback();
  }

  return (
    <details ref={menu} className="relative">
      <summary className="cursor-pointer list-none rounded-md bg-[#e5a00d] px-3.5 py-2 text-sm font-semibold text-black transition marker:content-none hover:bg-[#f2b51d]">
        Debug <span aria-hidden="true">⌄</span>
      </summary>
      <div className="absolute right-0 z-20 mt-2 w-64 overflow-hidden rounded-lg border border-white/10 bg-[#202020] p-1 shadow-2xl shadow-black/50">
        {game ? (
          <>
            <DebugItem
              label={action === 'save' ? 'Adding save…' : 'New save'}
              description={`Add a save to ${game.label}`}
              disabled={action !== null}
              onClick={() => run(onAddSave)}
            />
            <DebugItem
              label={action === 'revision' ? 'Committing…' : 'Commit revision'}
              description={selectedSave ? `Advance ${selectedSaveName}` : 'Select a save first'}
              disabled={!selectedSave || !revisionHistoryAvailable || action !== null}
              onClick={() => run(onAddRevision)}
            />
            <DebugItem
              label={action === 'fork' ? 'Forking save…' : 'Fork save'}
              description="Create another playable save from this head"
              disabled={!selectedSave || !canFork || !revisionHistoryAvailable || action !== null}
              onClick={() => run(onForkSave)}
            />
          </>
        ) : (
          <DebugItem
            label={action === 'game' ? 'Creating game…' : 'New game'}
            description="Add a random game to your library with its first save"
            disabled={action !== null}
            onClick={() => run(onAddRandomGame)}
          />
        )}
      </div>
    </details>
  );
}

function DebugItem({
  label,
  description,
  disabled,
  onClick,
}: {
  label: string;
  description: string;
  disabled: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className="w-full rounded-lg px-3 py-2.5 text-left transition hover:bg-white/5 disabled:cursor-not-allowed disabled:opacity-40"
    >
      <span className="block text-sm font-medium text-white">{label}</span>
      <span className="mt-0.5 block truncate text-xs text-slate-500">{description}</span>
    </button>
  );
}
