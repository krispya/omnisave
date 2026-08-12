import type { Omnisave, Revision } from '../../lib/omnisave-api.js';
import { defaultSaveName, displaySaveName } from './save-name.js';

/** One independently current Omnisave's lane in the combined game graph. */
export type GraphLane = {
  save: Omnisave;
  name: string;
  /** The save's primary lane; its rewind branches occupy unnamed lanes to the right. */
  lane: number;
};

/** One immutable revision node. Shared fork ancestry appears exactly once. */
export type GraphNode = {
  revision: Revision;
  /** The Omnisave that created this node, or the first surviving member. */
  save: Omnisave;
  saveName: string;
  lane: number;
  row: number;
  memberSaveIDs: string[];
  currentFor: GraphLane[];
};

export type GraphEdge = {
  from: GraphNode;
  to: GraphNode;
  /** A cross-save edge is the first unique commit made through a fork. */
  kind: 'revision' | 'fork';
  /** A straight line in this shared lane would pass under an unrelated node. */
  blocked: boolean;
};

export type RevisionGraph = {
  lanes: GraphLane[];
  /** Lanes drawn in total: each save's primary lane plus its branch lanes. */
  laneCount: number;
  /** Newest first; `row` is the index into this list. */
  nodes: GraphNode[];
  edges: GraphEdge[];
};

/** A maximal parent-to-newest-child path within one save, drawn in one lane. */
type Chain = {
  members: GraphNode[];
  /** The same-save node the chain branches away from, if any. */
  sprout?: GraphNode;
};

function timestamp(value: string) {
  const time = new Date(value).getTime();
  return Number.isNaN(time) ? 0 : time;
}

/** Rows a chain's lane must keep clear: its nodes plus the run down to its sprout. */
function chainSpan(chain: Chain): [number, number] {
  const rows = chain.members.map((member) => member.row);
  return [Math.min(...rows), chain.sprout ? chain.sprout.row : Math.max(...rows)];
}

/** Splits one save into consecutive commit chains that can share a vertical lane. */
function buildChains(roots: GraphNode[], childrenByID: Map<string, GraphNode[]>): Chain[] {
  const chains: Chain[] = [];
  // The oldest root anchors the primary chain; roots are newest first.
  const pending = [...roots]
    .reverse()
    .map((root): { start: GraphNode; sprout?: GraphNode } => ({ start: root }));

  for (const next of pending) {
    const members: GraphNode[] = [];
    let node: GraphNode | undefined = next.start;
    while (node) {
      members.push(node);
      const children: GraphNode[] = childrenByID.get(node.revision.id) ?? [];
      for (const branch of children.slice(1)) pending.push({ start: branch, sprout: node });
      node = children[0];
    }
    chains.push({ members, sprout: next.sprout });
  }
  return chains;
}

/** Packs branch chains into as few lanes as their row spans allow, primary first. */
function assignChainLanes(chains: Chain[], baseLane: number) {
  const branches = chains.slice(1).sort((left, right) => chainSpan(left)[0] - chainSpan(right)[0]);
  // Per branch lane, the last occupied row; a chain fits below only strictly past it.
  const lanesEnd: number[] = [];
  for (const member of chains[0]?.members ?? []) member.lane = baseLane;
  for (const chain of branches) {
    const [start, end] = chainSpan(chain);
    let slot = lanesEnd.findIndex((occupiedUntil) => occupiedUntil < start);
    if (slot === -1) slot = lanesEnd.push(Number.NEGATIVE_INFINITY) - 1;
    lanesEnd[slot] = Math.max(lanesEnd[slot] ?? end, end);
    for (const member of chain.members) member.lane = baseLane + 1 + slot;
  }
  return 1 + lanesEnd.length;
}

/**
 * Lays a game's deduplicated revisions onto save lanes and rewind-branch lanes.
 * Each same-lane edge connects consecutive commits.
 */
export function buildRevisionGraph(
  saves: Omnisave[],
  revisionsBySave: Map<string, Revision[]>
): RevisionGraph {
  const lanes: GraphLane[] = [];
  const laneBySave = new Map<string, GraphLane>();
  for (const [index, save] of saves.entries()) {
    if ((revisionsBySave.get(save.id) ?? []).length === 0) continue;
    const lane = { save, name: displaySaveName(save, defaultSaveName(index)), lane: lanes.length };
    lanes.push(lane);
    laneBySave.set(save.id, lane);
  }

  const nodesByID = new Map<string, GraphNode>();
  const saveOrder = new Map<string, number>();
  let sequence = 0;
  const sequenceByID = new Map<string, number>();
  for (const save of saves) {
    for (const revision of revisionsBySave.get(save.id) ?? []) {
      const existing = nodesByID.get(revision.id);
      if (existing) {
        if (!existing.memberSaveIDs.includes(save.id)) existing.memberSaveIDs.push(save.id);
        continue;
      }
      const creator = laneBySave.get(revision.omnisave_id) ?? laneBySave.get(save.id);
      if (!creator) continue;
      nodesByID.set(revision.id, {
        revision,
        save: creator.save,
        saveName: creator.name,
        lane: creator.lane,
        row: 0,
        memberSaveIDs: [save.id],
        currentFor: [],
      });
      saveOrder.set(revision.id, creator.lane);
      sequenceByID.set(revision.id, sequence++);
    }
  }

  for (const lane of lanes) {
    if (!lane.save.current_revision_id) continue;
    nodesByID.get(lane.save.current_revision_id)?.currentFor.push(lane);
  }

  const nodes = [...nodesByID.values()].sort(
    (left, right) =>
      timestamp(right.revision.created_at) - timestamp(left.revision.created_at) ||
      (saveOrder.get(right.revision.id) ?? 0) - (saveOrder.get(left.revision.id) ?? 0) ||
      (sequenceByID.get(right.revision.id) ?? 0) - (sequenceByID.get(left.revision.id) ?? 0)
  );
  for (const [row, node] of nodes.entries()) node.row = row;

  // Group each save's nodes into newest-first chains.
  const childrenByID = new Map<string, GraphNode[]>();
  const rootsBySave = new Map<string, GraphNode[]>();
  for (const node of nodes) {
    const parent = node.revision.parent_id ? nodesByID.get(node.revision.parent_id) : undefined;
    if (parent && parent.save.id === node.save.id) {
      childrenByID.set(parent.revision.id, [...(childrenByID.get(parent.revision.id) ?? []), node]);
    } else {
      rootsBySave.set(node.save.id, [...(rootsBySave.get(node.save.id) ?? []), node]);
    }
  }

  let laneCount = 0;
  for (const lane of lanes) {
    const chains = buildChains(rootsBySave.get(lane.save.id) ?? [], childrenByID);
    lane.lane = laneCount;
    laneCount += assignChainLanes(chains, laneCount);
  }

  const rowsByLane = new Map<number, number[]>();
  for (const node of nodes) {
    rowsByLane.set(node.lane, [...(rowsByLane.get(node.lane) ?? []), node.row]);
  }

  function blocked(from: GraphNode, to: GraphNode) {
    if (from.lane !== to.lane) return false;
    const [low, high] = from.row < to.row ? [from.row, to.row] : [to.row, from.row];
    return (rowsByLane.get(from.lane) ?? []).some((row) => row > low && row < high);
  }

  const edges: GraphEdge[] = [];
  for (const node of nodes) {
    const parent = node.revision.parent_id ? nodesByID.get(node.revision.parent_id) : undefined;
    if (parent) {
      edges.push({
        from: node,
        to: parent,
        kind: node.save.id === parent.save.id ? 'revision' : 'fork',
        blocked: blocked(node, parent),
      });
    }
  }

  return { lanes, laneCount, nodes, edges };
}
