import type { Omnisave, Revision } from '../../lib/omnisave-api.js';
import { buildRevisionGraph, type GraphNode } from './build-revision-graph.js';

/** One revision row's share of the rail: its dot plus every line crossing it. */
export type RailRow = {
  node: GraphNode;
  /** The dot's lane continues upward to a child drawn above. */
  lineUp: boolean;
  /** The dot's lane continues downward toward its parent. */
  lineDown: boolean;
  /** Lanes whose vertical line passes over this row without a dot here. */
  through: number[];
  /** Branch lanes whose line ends here, curving into this row's dot. */
  curvesIn: number[];
};

export type RevisionRail = {
  rows: RailRow[];
  laneCount: number;
};

/** Lays newest-first revision history onto trunk and rewind-branch lanes. */
export function buildRevisionRail(save: Omnisave, revisions: Revision[]): RevisionRail {
  const graph = buildRevisionGraph([save], new Map([[save.id, revisions]]));
  const rows: RailRow[] = graph.nodes.map((node) => ({
    node,
    lineUp: false,
    lineDown: false,
    through: [],
    curvesIn: [],
  }));
  const rowByID = new Map(graph.nodes.map((node) => [node.revision.id, node.row]));

  for (const edge of graph.edges) {
    const child = edge.from;
    const parentRow = rowByID.get(edge.to.revision.id);
    if (parentRow === undefined) continue;
    const childRow = child.row;

    rows[childRow].lineDown = true;
    if (child.lane === edge.to.lane) rows[parentRow].lineUp = true;
    else rows[parentRow].curvesIn.push(child.lane);

    // The child's lane runs down to its parent, over every row between them.
    for (let row = childRow + 1; row < parentRow; row += 1) rows[row].through.push(child.lane);
  }

  return { rows, laneCount: Math.max(graph.laneCount, 1) };
}
