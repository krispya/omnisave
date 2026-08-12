import { Children, type ReactNode } from 'react';
import { groupItemRadii } from '../theme.js';

/** A connected group whose row corners are derived from child position. */
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
          // Row position determines its connected corners.
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
