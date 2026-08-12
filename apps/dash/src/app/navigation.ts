import type { IconName } from '../components/icon.js';
import type { Route } from '../lib/route.js';

/** Dash destinations shared by wide and narrow navigation. */
export type Destination = {
  id: string;
  label: string;
  icon: IconName;
  route: Route;
  /** Routes that mark this destination as active. */
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
