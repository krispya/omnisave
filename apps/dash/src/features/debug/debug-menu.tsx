import type { OmniSave } from '../../lib/omnisave-api.js';
import { useDismissibleDetails } from '../../lib/use-dismissible-details.js';
import type { GameSummary } from '../games/game-library.js';
import { formatSlotName } from '../games/slot-name.js';

type DebugAction = 'game' | 'save' | 'revision' | null;

type DebugMenuProps = {
  game?: GameSummary;
  selectedSave?: OmniSave;
  action: DebugAction;
  revisionHistoryAvailable: boolean;
  onAddRandomGame: () => void;
  onAddSave: () => void;
  onAddRevision: () => void;
};

export function DebugMenu({
  game,
  selectedSave,
  action,
  revisionHistoryAvailable,
  onAddRandomGame,
  onAddSave,
  onAddRevision,
}: DebugMenuProps) {
  const menu = useDismissibleDetails();

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
              description={`Add another slot to ${game.label}`}
              disabled={action !== null}
              onClick={() => run(onAddSave)}
            />
            <DebugItem
              label={action === 'revision' ? 'Adding revision…' : 'New revision'}
              description={
                selectedSave ? `Extend ${formatSlotName(selectedSave.slot)}` : 'Select a save first'
              }
              disabled={!selectedSave || !revisionHistoryAvailable || action !== null}
              onClick={() => run(onAddRevision)}
            />
          </>
        ) : (
          <DebugItem
            label={action === 'game' ? 'Creating OmniSave…' : 'New OmniSave'}
            description="Create a random game with its first save"
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
