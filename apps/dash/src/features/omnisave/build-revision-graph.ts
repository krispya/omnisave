import type { Omnisave, Revision } from '../../lib/omnisave-api.js';
import { defaultSaveName, displaySaveName } from './save-name.js';

/** One save's column in the graph. Saves without revisions never claim a lane. */
export type GraphLane = {
  save: Omnisave;
  name: string;
  lane: number;
};

/** One revision, placed at the crossing of its save's lane and its position in time. */
export type GraphNode = {
  revision: Revision;
  save: Omnisave;
  saveName: string;
  lane: number;
  row: number;
  isHead: boolean;
};

/**
 * A line between two revisions, drawn from the newer end to the older one. `fork` edges
 * cross lanes: they are the moment one save was started from another save's revision.
 */
export type GraphEdge = {
  from: GraphNode;
  to: GraphNode;
  kind: 'revision' | 'fork';
};

export type RevisionGraph = {
  lanes: GraphLane[];
  /** Newest first; `row` is the index into this list. */
  nodes: GraphNode[];
  edges: GraphEdge[];
};

function timestamp(value: string) {
  const time = new Date(value).getTime();
  return Number.isNaN(time) ? 0 : time;
}

/**
 * Lays every revision of a game's saves onto one graph: a lane per save, a row per
 * revision newest first, and edges for both parent links and fork points.
 */
export function buildRevisionGraph(
  saves: Omnisave[],
  revisionsBySave: Map<string, Revision[]>
): RevisionGraph {
  const lanes: GraphLane[] = [];
  const nodes: GraphNode[] = [];
  // Revisions arrive oldest first, which is the tiebreak when timestamps collide.
  const sequence = new Map<string, number>();

  for (const [index, save] of saves.entries()) {
    const revisions = revisionsBySave.get(save.id) ?? [];
    if (revisions.length === 0) continue;

    const lane = lanes.length;
    lanes.push({ save, name: displaySaveName(save, defaultSaveName(index)), lane });
    for (const [position, revision] of revisions.entries()) {
      sequence.set(revision.id, position);
      nodes.push({
        revision,
        save,
        saveName: lanes[lane].name,
        lane,
        row: 0,
        isHead: revision.id === save.head_revision_id,
      });
    }
  }

  nodes.sort(
    (left, right) =>
      timestamp(right.revision.created_at) - timestamp(left.revision.created_at) ||
      right.lane - left.lane ||
      (sequence.get(right.revision.id) ?? 0) - (sequence.get(left.revision.id) ?? 0)
  );
  for (const [row, node] of nodes.entries()) node.row = row;

  const byRevision = new Map(nodes.map((node) => [node.revision.id, node]));
  const edges: GraphEdge[] = [];
  for (const node of nodes) {
    const parent = node.revision.parent_id ? byRevision.get(node.revision.parent_id) : undefined;
    if (parent) edges.push({ from: node, to: parent, kind: 'revision' });
  }
  for (const { save } of lanes) {
    const origin = save.forked_from;
    if (!origin) continue;

    const source = byRevision.get(origin.revision_id);
    // A fork branches at its own first revision, the copy it was created with.
    const root = (revisionsBySave.get(save.id) ?? []).find((revision) => !revision.parent_id);
    const from = root ? byRevision.get(root.id) : undefined;
    if (source && from) edges.push({ from, to: source, kind: 'fork' });
  }

  return { lanes, nodes, edges };
}
