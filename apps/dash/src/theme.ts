/** Design tokens mirrored by Tailwind's `@theme` in `index.css`. */

export const color = {
  /** The page behind everything. */
  bg: '#0e0f12',
  /** Cards, rows, menus, and dialogs. */
  surface: '#15171c',
  /** The expanded navigation surface. */
  rail: '#1c1e23',
  /** Separates adjacent surfaces. */
  outline: 'rgb(255 255 255 / 0.12)',
  /** Marks the current section, revision, or setting. */
  accent: '#d0a24f',
  /** Body text and filled controls. */
  text: '#ededed',
  /** Subtitles, captions, and inactive controls. */
  muted: 'rgb(237 237 237 / 0.6)',
  /** Destructive actions and failures. */
  danger: '#ff6b6b',
} as const;

/** Shared radii for standalone controls and connected groups. */
export const radius = {
  xs: '5px',
  sm: '8px',
  md: '12px',
  lg: '20px',
  full: '9999px',
} as const;

/** The hairline gap between adjacent rows of a group. */
export const groupGap = '2px';

export const duration = {
  /** Hover and immediate feedback. */
  fast: 120,
  /** Movement in response to an action. */
  normal: 200,
  /** Enter and exit transitions. */
  slow: 300,
} as const;

/** Returns corner classes for an item's position in a connected group. */
export function groupItemRadii(index: number, count: number) {
  const top = index === 0 ? 'rounded-t-lg' : 'rounded-t-xs';
  const bottom = index === count - 1 ? 'rounded-b-lg' : 'rounded-b-xs';
  return `${top} ${bottom}`;
}
