import type { InputHTMLAttributes } from 'react';

/**
 * Text inputs are filled rather than outlined: a border would compete with the
 * hairlines that separate surfaces, where a fill reads as "this is yours to
 * type in" without adding another line to the page. Focus brightens the fill,
 * so the focused field is the brightest thing in the form.
 */
export const fieldClass =
  'min-w-0 rounded-md bg-text/8 px-3.5 py-2.5 text-sm text-text outline-none transition duration-120 placeholder:text-muted focus:bg-text/15';

type FieldProps = InputHTMLAttributes<HTMLInputElement> & { label: string };

/** A labelled field. The label sits above, small and muted, as on a settings row. */
export function Field({ label, className = '', ...props }: FieldProps) {
  return (
    <label className="flex flex-col gap-1.5 text-xs text-muted">
      {label}
      <input className={`${fieldClass} ${className}`} {...props} />
    </label>
  );
}
