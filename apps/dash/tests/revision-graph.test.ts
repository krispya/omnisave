import { describe, expect, it } from 'vitest';
import type { Omnisave, Revision } from '../src/lib/omnisave-api.js';
import {
  buildRevisionGraph,
  type RevisionGraph,
} from '../src/features/omnisave/build-revision-graph.js';

function save(id: string, currentRevisionID: string, createdAt: string): Omnisave {
  return {
    id,
    game_id: 'game-1',
    display_name: id,
    current_revision_id: currentRevisionID,
    current_revision_created_at: createdAt,
    latest_revision_created_at: createdAt,
    created_at: createdAt,
  };
}

function revision(
  id: string,
  creatorSaveID: string,
  parentID: string | null,
  createdAt: string
): Revision {
  return {
    id,
    omnisave_id: creatorSaveID,
    display_name: '',
    parent_id: parentID,
    created_at: createdAt,
    files: [],
  };
}

function node(graph: RevisionGraph, id: string) {
  const found = graph.nodes.find((candidate) => candidate.revision.id === id);
  if (!found) throw new Error(`node ${id} missing from graph`);
  return found;
}

/** Every unblocked same-lane edge must run past no other node in its lane. */
function expectVerticalEdgesUnobstructed(graph: RevisionGraph) {
  for (const edge of graph.edges) {
    if (edge.from.lane !== edge.to.lane || edge.blocked) continue;
    const low = Math.min(edge.from.row, edge.to.row);
    const high = Math.max(edge.from.row, edge.to.row);
    const between = graph.nodes.filter(
      (candidate) => candidate.lane === edge.from.lane && candidate.row > low && candidate.row < high
    );
    expect(between.map((candidate) => candidate.revision.id)).toEqual([]);
  }
}

describe('revision graph', () => {
  it('draws shared ancestry once and gives each save an independent current pointer', () => {
    const root = revision('root', 'desktop', null, '2026-01-01T00:00:00Z');
    const desktopTip = revision('desktop-tip', 'desktop', 'root', '2026-01-02T00:00:00Z');
    const deckTip = revision('deck-tip', 'steam-deck', 'root', '2026-01-03T00:00:00Z');
    const desktop = save('desktop', desktopTip.id, desktopTip.created_at);
    const deck = save('steam-deck', deckTip.id, deckTip.created_at);

    const graph = buildRevisionGraph(
      [desktop, deck],
      new Map([
        [desktop.id, [root, desktopTip]],
        [deck.id, [root, deckTip]],
      ])
    );

    expect(graph.nodes.map((node) => node.revision.id).sort()).toEqual([
      'deck-tip',
      'desktop-tip',
      'root',
    ]);
    expect(graph.laneCount).toBe(2);
    expect(node(graph, 'root').memberSaveIDs.sort()).toEqual(['desktop', 'steam-deck']);
    expect(node(graph, 'desktop-tip').currentFor.map((lane) => lane.save.id)).toEqual(['desktop']);
    expect(node(graph, 'deck-tip').currentFor.map((lane) => lane.save.id)).toEqual(['steam-deck']);
    expect(graph.edges.find((edge) => edge.from.revision.id === 'deck-tip')?.kind).toBe('fork');
    expectVerticalEdgesUnobstructed(graph);
  });

  it('moves the branch a rewind left behind into its own lane inside the save', () => {
    // root -> abandoned -> abandoned-tip, then a rewind to root and a new commit.
    const root = revision('root', 'solo', null, '2026-01-01T00:00:00Z');
    const abandoned = revision('abandoned', 'solo', 'root', '2026-01-02T00:00:00Z');
    const abandonedTip = revision('abandoned-tip', 'solo', 'abandoned', '2026-01-03T00:00:00Z');
    const newTip = revision('new-tip', 'solo', 'root', '2026-01-04T00:00:00Z');
    const solo = save('solo', newTip.id, newTip.created_at);

    const graph = buildRevisionGraph(
      [solo],
      new Map([[solo.id, [root, abandoned, abandonedTip, newTip]]])
    );

    // One save label, two drawn lanes: the branch never fabricates a save column.
    expect(graph.lanes.map((lane) => lane.save.id)).toEqual(['solo']);
    expect(graph.laneCount).toBe(2);
    // The newest child keeps the primary lane; the abandoned path moves aside.
    expect(node(graph, 'root').lane).toBe(0);
    expect(node(graph, 'new-tip').lane).toBe(0);
    expect(node(graph, 'abandoned').lane).toBe(1);
    expect(node(graph, 'abandoned-tip').lane).toBe(1);
    // Sibling children of one parent never share a lane.
    expect(node(graph, 'new-tip').lane).not.toBe(node(graph, 'abandoned').lane);
    // Both edges out of the branch point stay 'revision': branching is within the save.
    for (const edge of graph.edges) expect(edge.kind).toBe('revision');
    expectVerticalEdgesUnobstructed(graph);
  });

  it('records both saves on a fork point that is current for each of them', () => {
    const root = revision('root', 'alpha', null, '2026-01-01T00:00:00Z');
    const tip = revision('tip', 'alpha', 'root', '2026-01-02T00:00:00Z');
    // Alpha rewound to the fork point; beta forked there and never committed.
    const alpha = save('alpha', root.id, root.created_at);
    const beta = save('beta', root.id, root.created_at);

    const graph = buildRevisionGraph(
      [alpha, beta],
      new Map([
        [alpha.id, [root, tip]],
        [beta.id, [root]],
      ])
    );

    expect(node(graph, 'root').currentFor.map((lane) => lane.save.id)).toEqual(['alpha', 'beta']);
    expect(node(graph, 'root').memberSaveIDs.sort()).toEqual(['alpha', 'beta']);
    expect(node(graph, 'tip').currentFor).toEqual([]);
    expectVerticalEdgesUnobstructed(graph);
  });

  it('lays a revision from a deleted creator into a surviving member save lane', () => {
    const root = revision('root', 'ghost', null, '2026-01-01T00:00:00Z');
    const tip = revision('tip', 'survivor', 'root', '2026-01-02T00:00:00Z');
    const survivor = save('survivor', tip.id, tip.created_at);

    const graph = buildRevisionGraph([survivor], new Map([[survivor.id, [root, tip]]]));

    expect(node(graph, 'root').save.id).toBe('survivor');
    expect(node(graph, 'root').lane).toBe(node(graph, 'tip').lane);
    expect(graph.edges).toHaveLength(1);
    expect(graph.edges[0]?.kind).toBe('revision');
  });

  it('orders rows newest first and numbers them by list position', () => {
    const root = revision('root', 'desktop', null, '2026-01-01T00:00:00Z');
    const middle = revision('middle', 'desktop', 'root', '2026-01-02T00:00:00Z');
    const deckTip = revision('deck-tip', 'steam-deck', 'middle', '2026-01-03T00:00:00Z');
    const desktopTip = revision('desktop-tip', 'desktop', 'middle', '2026-01-04T00:00:00Z');
    const desktop = save('desktop', desktopTip.id, desktopTip.created_at);
    const deck = save('steam-deck', deckTip.id, deckTip.created_at);

    const graph = buildRevisionGraph(
      [desktop, deck],
      new Map([
        [desktop.id, [root, middle, desktopTip]],
        [deck.id, [root, middle, deckTip]],
      ])
    );

    expect(graph.nodes.map((node) => node.revision.id)).toEqual([
      'desktop-tip',
      'deck-tip',
      'middle',
      'root',
    ]);
    for (const [index, graphNode] of graph.nodes.entries()) expect(graphNode.row).toBe(index);
    expectVerticalEdgesUnobstructed(graph);
  });

  it('graphs a single-revision save as one node without edges', () => {
    const only = revision('only', 'solo', null, '2026-01-01T00:00:00Z');
    const solo = save('solo', only.id, only.created_at);

    const graph = buildRevisionGraph([solo], new Map([[solo.id, [only]]]));

    expect(graph.laneCount).toBe(1);
    expect(graph.edges).toEqual([]);
    expect(node(graph, 'only').row).toBe(0);
    expect(node(graph, 'only').lane).toBe(0);
    expect(node(graph, 'only').currentFor.map((lane) => lane.save.id)).toEqual(['solo']);
  });

  it('marks a same-lane edge blocked when clock skew slips a node between its rows', () => {
    // The middle commit claims a later time than its own child, so by-time rows
    // interleave the chain and the long edge must route around the child.
    const root = revision('root', 'solo', null, '2026-01-01T00:00:00Z');
    const middle = revision('middle', 'solo', 'root', '2026-01-03T00:00:00Z');
    const tip = revision('tip', 'solo', 'middle', '2026-01-02T00:00:00Z');
    const solo = save('solo', tip.id, tip.created_at);

    const graph = buildRevisionGraph([solo], new Map([[solo.id, [root, middle, tip]]]));

    expect(graph.nodes.map((node) => node.revision.id)).toEqual(['middle', 'tip', 'root']);
    const long = graph.edges.find((edge) => edge.from.revision.id === 'middle');
    expect(long?.blocked).toBe(true);
    const short = graph.edges.find((edge) => edge.from.revision.id === 'tip');
    expect(short?.blocked).toBe(false);
    expectVerticalEdgesUnobstructed(graph);
  });
});
