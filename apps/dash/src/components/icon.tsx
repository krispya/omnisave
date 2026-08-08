/**
 * The glyphs the dash draws, as one small set rather than a dependency.
 *
 * Every icon is a solid 24×24 shape painted in `currentColor`, so a row's icon
 * takes its color from the row and a disabled control dims its icon along with
 * its text. Filled rather than outlined: at the sizes these are drawn, a solid
 * shape holds together where a hairline outline dissolves.
 *
 * Paths are filled even-odd, so an inner subpath — the hole in a gear, the dot
 * in a tag — reads as a hole rather than filling solid.
 */

export type IconName =
  | 'menu'
  | 'arrow-left'
  | 'chevron-left'
  | 'chevron-right'
  | 'library'
  | 'settings'
  | 'devices'
  | 'network'
  | 'tag';

const paths: Record<IconName, string> = {
  menu: 'M3.5 6.25h17a1 1 0 0 1 0 2h-17a1 1 0 0 1 0-2Zm0 4.75h17a1 1 0 0 1 0 2h-17a1 1 0 0 1 0-2Zm0 4.75h17a1 1 0 0 1 0 2h-17a1 1 0 0 1 0-2Z',
  'arrow-left':
    'M11.03 4.97a1 1 0 0 1 0 1.41L6.91 10.5H20a1 1 0 0 1 0 2H6.91l4.12 4.12a1 1 0 1 1-1.41 1.41l-5.83-5.82a1 1 0 0 1 0-1.42l5.83-5.82a1 1 0 0 1 1.41 0Z',
  'chevron-left':
    'M14.7 6.7a1 1 0 0 1 0 1.42L10.82 12l3.88 3.88a1 1 0 1 1-1.42 1.42l-4.58-4.6a1 1 0 0 1 0-1.4l4.58-4.6a1 1 0 0 1 1.42 0Z',
  'chevron-right':
    'M9.3 6.7a1 1 0 0 0 0 1.42L13.18 12 9.3 15.88a1 1 0 1 0 1.42 1.42l4.58-4.6a1 1 0 0 0 0-1.4l-4.58-4.6a1 1 0 0 0-1.42 0Z',
  // Four covers on a shelf. Two bars would have read as a pause button, which
  // is the one thing a media app's menu must not accidentally say.
  library:
    'M4 3h5a1 1 0 0 1 1 1v6a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1Zm11 0h5a1 1 0 0 1 1 1v6a1 1 0 0 1-1 1h-5a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1ZM4 13h5a1 1 0 0 1 1 1v6a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1v-6a1 1 0 0 1 1-1Zm11 0h5a1 1 0 0 1 1 1v6a1 1 0 0 1-1 1h-5a1 1 0 0 1-1-1v-6a1 1 0 0 1 1-1Z',
  settings:
    'M19.14 12.94a7.1 7.1 0 0 0 0-1.88l2.03-1.58a.49.49 0 0 0 .12-.61l-1.92-3.32a.49.49 0 0 0-.59-.22l-2.39.96a7.03 7.03 0 0 0-1.62-.94l-.36-2.54a.48.48 0 0 0-.48-.41h-3.84a.48.48 0 0 0-.47.41l-.36 2.54c-.59.24-1.13.57-1.62.94l-2.39-.96a.49.49 0 0 0-.59.22L2.74 8.87a.49.49 0 0 0 .12.61l2.03 1.58a7.1 7.1 0 0 0 0 1.88l-2.03 1.58a.49.49 0 0 0-.12.61l1.92 3.32c.12.22.37.3.59.22l2.39-.96c.5.38 1.03.7 1.62.94l.36 2.54c.04.24.23.41.47.41h3.84c.24 0 .44-.17.48-.41l.36-2.54c.59-.24 1.12-.56 1.62-.94l2.39.96c.22.08.47 0 .59-.22l1.92-3.32a.49.49 0 0 0-.12-.61l-2.03-1.58ZM12 15.6a3.6 3.6 0 1 1 0-7.2 3.6 3.6 0 0 1 0 7.2Z',
  devices:
    'M3 5h13a1 1 0 0 1 1 1v8a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1Zm-1 12h15v2H2v-2Zm17-9h3a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1h-3a1 1 0 0 1-1-1V9a1 1 0 0 1 1-1Z',
  network:
    'M12 21l3.6-4.5a5.74 5.74 0 0 0-7.2 0L12 21Zm0-12c-2.65 0-5.05.99-6.88 2.61l1.3 1.63A8.44 8.44 0 0 1 12 11c2.13 0 4.09.85 5.58 2.24l1.3-1.63A10.42 10.42 0 0 0 12 9Zm0-6C7.95 3 4.21 4.34 1.2 6.6l1.29 1.62A15.4 15.4 0 0 1 12 5c3.57 0 6.86 1.2 9.51 3.22L22.8 6.6C19.79 4.34 16.05 3 12 3Z',
  tag: 'M21.41 11.58l-9-9A2 2 0 0 0 11 2H4a2 2 0 0 0-2 2v7c0 .53.21 1.04.59 1.42l9 9c.36.36.86.58 1.41.58s1.05-.22 1.41-.59l7-7c.37-.36.59-.86.59-1.41s-.23-1.06-.59-1.42ZM5.5 7A1.5 1.5 0 1 1 7 5.5 1.5 1.5 0 0 1 5.5 7Z',
};

export function Icon({ name, className = 'size-5' }: { name: IconName; className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="currentColor"
      fillRule="evenodd"
      className={className}
      aria-hidden="true"
    >
      <path d={paths[name]} />
    </svg>
  );
}
