import type { Achievement } from '../../lib/omnisave-api.js';

/**
 * Groups achievements by the revision each one landed on, in unlock order.
 * Unlocks still waiting for a revision are left out: they have no row to sit
 * on until the next commit claims them.
 */
export function achievementsByRevision(achievements: Achievement[]): Map<string, Achievement[]> {
  const byRevision = new Map<string, Achievement[]>();
  const ordered = [...achievements].sort((left, right) =>
    left.unlocked_at === right.unlocked_at
      ? left.id.localeCompare(right.id)
      : left.unlocked_at.localeCompare(right.unlocked_at)
  );
  for (const achievement of ordered) {
    if (!achievement.revision_id) continue;
    const marks = byRevision.get(achievement.revision_id);
    if (marks) marks.push(achievement);
    else byRevision.set(achievement.revision_id, [achievement]);
  }
  return byRevision;
}
