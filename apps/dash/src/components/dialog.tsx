import { useEffect, useId, type ReactNode } from 'react';

/**
 * The surface every dialog is drawn on.
 *
 * Escape dismisses, but not while the dialog's own action is in flight — a
 * stray keypress should not abandon a write whose outcome is not known yet.
 */
export function Dialog({
  title,
  description,
  busy = false,
  size = 'md',
  onDismiss,
  children,
}: {
  title: ReactNode;
  description?: ReactNode;
  /** Blocks dismissal while an action this dialog started is still running. */
  busy?: boolean;
  /** `lg` is for dialogs that show a record rather than ask a question. */
  size?: 'md' | 'lg';
  onDismiss: () => void;
  children: ReactNode;
}) {
  const titleID = useId();

  useEffect(() => {
    function closeOnEscape(event: KeyboardEvent) {
      if (event.key === 'Escape' && !busy) onDismiss();
    }

    window.addEventListener('keydown', closeOnEscape);
    return () => window.removeEventListener('keydown', closeOnEscape);
  }, [busy, onDismiss]);

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/70 px-5" role="presentation">
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleID}
        className={`w-full rounded-lg border border-outline bg-surface p-6 ${
          size === 'lg' ? 'max-w-2xl' : 'max-w-md'
        }`}
      >
        <h2 id={titleID} className="text-lg font-semibold text-text">
          {title}
        </h2>
        {description ? <p className="mt-2.5 text-sm leading-6 text-muted">{description}</p> : null}
        {children}
      </section>
    </div>
  );
}

/** What went wrong with the action this dialog is asking for. */
export function DialogError({ children }: { children: ReactNode }) {
  return (
    <p
      role="alert"
      className="mt-4 rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger"
    >
      {children}
    </p>
  );
}

/** The row of buttons a dialog ends on, with the affirmative one last. */
export function DialogActions({ children }: { children: ReactNode }) {
  return <div className="mt-6 flex justify-end gap-3">{children}</div>;
}
