/**
 * The dash's design system.
 *
 * The palette is monochrome on purpose: the foreground *is* the accent. Nothing
 * carries chroma except a single danger tone, so the eye has one thing to
 * follow — the thing being read — rather than competing with decoration. Depth
 * comes from a hairline outline and a radius, never from a shadow.
 *
 * These values are mirrored into Tailwind's `@theme` in `index.css`, which is
 * what generates the `bg-surface` / `text-muted` / `rounded-lg` utilities the
 * components actually use. This module is the written record of the system and
 * the home for the parts a stylesheet cannot express — the group geometry
 * below. Change a value here and in `index.css` together.
 */

export const color = {
  /** The page behind everything. */
  bg: '#0e0f12',
  /** Anything raised off the page: cards, rows, menus, dialogs. */
  surface: '#15171c',
  /**
   * The open navigation menu. Closed, the rail has no surface of its own — the
   * icons sit on the page, and the menu only becomes a panel once it is
   * carrying words that need a ground to sit on.
   */
  rail: '#1c1e23',
  /** The hairline that separates surfaces, in place of a shadow. */
  outline: 'rgb(255 255 255 / 0.12)',
  /**
   * Marks what is current: the section being read, the revision a save points
   * at, a setting that is on. Muted on purpose — it has to survive being the
   * only color on the page without becoming the loudest thing on it, and
   * everything it touches is a small mark rather than a filled area.
   *
   * It is not a second foreground. A filled button is still [color.text]; if
   * this starts appearing on things that are merely important rather than
   * current, it stops meaning anything.
   */
  accent: '#d0a24f',
  /** Foreground: body text, and the fill of a filled control. */
  text: '#ededed',
  /** Supporting text: subtitles, captions, the inactive half of a control. */
  muted: 'rgb(237 237 237 / 0.6)',
  /** The one chromatic tone, reserved for destruction and failure. */
  danger: '#ff6b6b',
} as const;

/**
 * Radii carry meaning rather than taste. `lg` is the outer corner of a group,
 * `xs` the seam between two rows inside one, and the contrast between them is
 * what makes a stack of rows read as a single object.
 */
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
  /** Hovers and other feedback that must not be perceptible as animation. */
  fast: 120,
  /** The default: anything that moves in response to a click. */
  normal: 200,
  /** Something arriving or leaving, where the motion is the explanation. */
  slow: 300,
} as const;

/**
 * Corner classes for item [index] of a [count]-item group: large radii on the
 * group's outer corners, small radii on the seams between adjacent items.
 *
 * The shape is derived from position, so a conditional row has to be left out
 * of the list entirely rather than rendered as nothing — a row that renders
 * empty still occupies a slot and corrupts the group's corners.
 */
export function groupItemRadii(index: number, count: number) {
  const top = index === 0 ? 'rounded-t-lg' : 'rounded-t-xs';
  const bottom = index === count - 1 ? 'rounded-b-lg' : 'rounded-b-xs';
  return `${top} ${bottom}`;
}
