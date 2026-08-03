import type { Omnisave, Revision } from '../../lib/omnisave-api.js';
import { buildRevisionGraph, type GraphEdge, type GraphNode } from './build-revision-graph.js';
import { formatDateTime } from '../../lib/format.js';

// The rows are a plain list; the graph is an overlay drawn from these fixed metrics.
const rowHeight = 44;
const laneWidth = 20;
const laneInset = 14;
const nodeRadius = 4.5;

type RevisionGraphProps = {
  saves: Omnisave[];
  revisionsBySave: Map<string, Revision[]>;
  selectedSave?: Omnisave;
  loading: boolean;
  error: string;
  onOpenSave: (save: Omnisave, revisionID?: string) => void;
};

function laneX(lane: number) {
  return laneInset + lane * laneWidth;
}

function rowY(row: number) {
  return row * rowHeight + rowHeight / 2;
}

function shortID(id: string) {
  return id.slice(0, 8);
}

function edgePath(edge: GraphEdge) {
  const startX = laneX(edge.from.lane);
  const startY = rowY(edge.from.row);
  const endX = laneX(edge.to.lane);
  const endY = rowY(edge.to.row);
  if (startX === endX) return `M${startX} ${startY} L${endX} ${endY}`;

  const middle = (startY + endY) / 2;
  return `M${startX} ${startY} C ${startX} ${middle}, ${endX} ${middle}, ${endX} ${endY}`;
}

function edgeStroke(edge: GraphEdge, selectedSaveID?: string) {
  if (edge.kind === 'fork') return 'rgb(229 160 13 / 0.45)';
  return edge.from.save.id === selectedSaveID ? 'rgb(229 160 13 / 0.55)' : 'rgb(255 255 255 / 0.14)';
}

function nodeStroke(node: GraphNode, selectedSaveID?: string) {
  return node.save.id === selectedSaveID ? '#e5a00d' : '#525252';
}

export function RevisionGraph({
  saves,
  revisionsBySave,
  selectedSave,
  loading,
  error,
  onOpenSave,
}: RevisionGraphProps) {
  const { lanes, nodes, edges } = buildRevisionGraph(saves, revisionsBySave);
  const gutter = laneInset * 2 + Math.max(lanes.length - 1, 0) * laneWidth;

  return (
    <div className="rounded-lg border border-white/5 bg-[#181818]">
      <div className="flex items-center justify-between gap-4 border-b border-white/5 px-5 py-4">
        <div className="min-w-0">
          <p className="text-sm font-medium text-white">Revision graph</p>
          <p className="mt-1 text-xs text-slate-500">
            Every revision of this game's saves. Forks branch to their own lane.
          </p>
        </div>
        <span className="shrink-0 text-xs text-neutral-500">
          {nodes.length} {nodes.length === 1 ? 'revision' : 'revisions'} · {lanes.length}{' '}
          {lanes.length === 1 ? 'save' : 'saves'}
        </span>
      </div>

      {error ? (
        <div
          role="alert"
          className="m-5 rounded-md border border-red-400/20 bg-red-400/10 px-4 py-3 text-sm text-red-200"
        >
          {error}
        </div>
      ) : null}

      {loading ? (
        <div className="space-y-2 p-5" aria-label="Loading the revision graph">
          {Array.from({ length: 4 }, (_, index) => (
            <div key={index} className="h-8 animate-pulse rounded bg-white/5" />
          ))}
        </div>
      ) : nodes.length === 0 && !error ? (
        <div className="py-14 text-center">
          <h4 className="text-sm font-medium text-white">No revisions yet</h4>
          <p className="mx-auto mt-2 max-w-xs text-xs leading-5 text-slate-500">
            These saves have no history to graph. Use the Debug menu to add the first revision.
          </p>
        </div>
      ) : (
        <div className="relative overflow-x-auto py-2">
          <svg
            className="pointer-events-none absolute top-2 left-0 z-10"
            width={gutter}
            height={nodes.length * rowHeight}
            aria-hidden="true"
          >
            {edges.map((edge) => (
              <path
                key={`${edge.from.revision.id}-${edge.to.revision.id}`}
                d={edgePath(edge)}
                fill="none"
                strokeWidth={1.5}
                stroke={edgeStroke(edge, selectedSave?.id)}
                strokeDasharray={edge.kind === 'fork' ? '3 3' : undefined}
              />
            ))}
            {nodes.map((node) => (
              <circle
                key={node.revision.id}
                cx={laneX(node.lane)}
                cy={rowY(node.row)}
                r={nodeRadius}
                strokeWidth={2}
                stroke={nodeStroke(node, selectedSave?.id)}
                fill={node.isHead ? nodeStroke(node, selectedSave?.id) : '#181818'}
              />
            ))}
          </svg>

          <ol>
            {nodes.map((node) => {
              const selected = node.save.id === selectedSave?.id;
              const revisionName = node.revision.display_name || shortID(node.revision.id);
              return (
                <li key={node.revision.id}>
                  <button
                    type="button"
                    aria-pressed={selected}
                    aria-label={`Open ${node.saveName} at revision ${revisionName}`}
                    onClick={() => onOpenSave(node.save, node.revision.id)}
                    style={{ height: rowHeight, paddingLeft: gutter + 4 }}
                    className={`flex w-full items-center gap-3 pr-5 text-left transition ${
                      selected ? 'bg-[#e5a00d]/[0.06]' : 'hover:bg-white/[0.035]'
                    }`}
                  >
                    <span
                      className={`max-w-48 shrink-0 truncate font-mono text-xs ${
                        selected ? 'text-slate-200' : 'text-slate-400'
                      }`}
                    >
                      {revisionName}
                    </span>
                    <span
                      className={`min-w-0 flex-1 truncate text-xs ${
                        selected ? 'text-[#e5a00d]' : 'text-slate-500'
                      }`}
                    >
                      {node.saveName}
                    </span>
                    {node.isHead ? (
                      <span
                        className={`shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium ${
                          selected ? 'bg-[#e5a00d]/15 text-[#e5a00d]' : 'bg-white/5 text-neutral-400'
                        }`}
                      >
                        head
                      </span>
                    ) : null}
                    <time className="shrink-0 text-[10px] text-slate-600">
                      {formatDateTime(node.revision.created_at)}
                    </time>
                  </button>
                </li>
              );
            })}
          </ol>
        </div>
      )}
    </div>
  );
}
