import type { Omnisave } from '../../lib/omnisave-api.js';
import { defaultSaveName, displaySaveName } from './save-name.js';

export function SaveTree({
  saves,
  selectedSave,
  onSelectSave,
}: {
  saves: Omnisave[];
  selectedSave?: Omnisave;
  onSelectSave: (save: Omnisave) => void;
}) {
  const ids = new Set(saves.map((save) => save.id));
  const children = new Map<string, Omnisave[]>();
  for (const save of saves) {
    const parentID = save.forked_from?.omnisave_id;
    if (!parentID || !ids.has(parentID)) continue;
    children.set(parentID, [...(children.get(parentID) ?? []), save]);
  }
  const roots = saves.filter((save) => !save.forked_from || !ids.has(save.forked_from.omnisave_id));
  const rows: Array<{ save: Omnisave; depth: number }> = [];
  const visited = new Set<string>();
  function visit(save: Omnisave, depth: number) {
    if (visited.has(save.id)) return;
    visited.add(save.id);
    rows.push({ save, depth });
    for (const child of children.get(save.id) ?? []) visit(child, depth + 1);
  }
  for (const root of roots) visit(root, 0);
  for (const save of saves) visit(save, 0);

  return (
    <div className="overflow-hidden rounded-md border border-white/5 bg-[#181818]">
      {rows.map(({ save, depth }) => {
        const saveIndex = saves.findIndex((candidate) => candidate.id === save.id);
        const name = displaySaveName(save, defaultSaveName(Math.max(saveIndex, 0)));
        const forkCount = children.get(save.id)?.length ?? 0;
        return (
          <button
            key={save.id}
            type="button"
            onClick={() => onSelectSave(save)}
            className={`relative flex w-full items-center gap-3 border-b border-white/5 py-3 pr-3 text-left transition last:border-0 ${
              save.id === selectedSave?.id ? 'bg-[#e5a00d]/10' : 'hover:bg-white/[0.035]'
            }`}
            style={{ paddingLeft: `${12 + depth * 24}px` }}
          >
            {depth > 0 ? (
              <span className="font-mono text-sm text-slate-600" aria-hidden="true">
                └─
              </span>
            ) : (
              <span className="size-2 rounded-full bg-[#e5a00d]" aria-hidden="true" />
            )}
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm font-medium text-white">{name}</span>
              <span className="mt-0.5 block truncate font-mono text-[10px] text-slate-600">
                {save.head_revision_id?.slice(0, 8) ?? 'No revisions'}
              </span>
            </span>
            {forkCount > 0 ? (
              <span className="text-[10px] text-slate-500">
                {forkCount} {forkCount === 1 ? 'fork' : 'forks'}
              </span>
            ) : null}
          </button>
        );
      })}
    </div>
  );
}
