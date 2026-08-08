import type { IconName } from '../components/icon.js';
import type { Route } from '../lib/route.js';

/**
 * Where the dash can go, declared once.
 *
 * The shell renders this list two ways — a rail beside the content on wide
 * screens, a bar beneath it on narrow ones — so a new destination is a line
 * here rather than an edit in two layouts.
 */
export type Destination = {
  id: string;
  label: string;
  icon: IconName;
  route: Route;
  /**
   * Which routes count as being here. A game is somewhere inside the Library,
   * so reading one keeps the Library lit rather than lighting nothing.
   */
  covers: (route: Route) => boolean;
};

export const destinations: Destination[] = [
  {
    id: 'library',
    label: 'Library',
    icon: 'library',
    route: { name: 'library' },
    covers: (route) => route.name === 'library' || route.name === 'game',
  },
  {
    id: 'settings',
    label: 'Server',
    icon: 'settings',
    route: { name: 'settings' },
    covers: (route) => route.name === 'settings',
  },
];
