import { Children, type ReactNode } from 'react';
import { groupItemRadii } from '../theme.js';

/**
 * A section of settings drawn as a connected group: each row gets its own
 * surface, separated by hairline gaps, with large radii on the group's outer
 * corners and small ones on the seams between rows. A stack of rows reads as a
 * single object, and where one section ends and the next begins is legible
 * without a divider.
 *
 * Corners are computed from position, so a conditional row has to be left out
 * of `children` entirely — `{shown ? <Row/> : null}` is right, a row that
 * renders nothing still takes a slot and rounds the wrong corner.
 */
export function SettingsGroup({ title, children }: { title?: string; children: ReactNode }) {
  const rows = Children.toArray(children);
  if (rows.length === 0) return null;

  return (
    <section>
      {title ? (
        <h2 className="px-1 pb-2 text-xs font-semibold tracking-wide text-muted uppercase">
          {title}
        </h2>
      ) : null}
      <div className="flex flex-col gap-0.5">
        {rows.map((row, index) => (
          // Position is the identity here: row N is the Nth slot of the group,
          // and that is exactly what decides its shape.
          <div
            key={index}
            className={`overflow-hidden bg-surface ${groupItemRadii(index, rows.length)}`}
          >
            {row}
          </div>
        ))}
      </div>
    </section>
  );
}

/** Explanatory text under a group, for what a row's title cannot carry alone. */
export function GroupNote({ children }: { children: ReactNode }) {
  return <p className="px-1 pt-2 text-xs leading-5 text-muted">{children}</p>;
}
