import type { InputHTMLAttributes } from 'react';

/** A labelled text field. */
export const fieldClass =
  'min-w-0 rounded-md bg-text/8 px-3.5 py-2.5 text-sm text-text outline-none transition duration-120 placeholder:text-muted focus:bg-text/15';

type FieldProps = InputHTMLAttributes<HTMLInputElement> & { label: string };

/** A labelled field with optional validation text. */
export function Field({ label, className = '', ...props }: FieldProps) {
  return (
    <label className="flex flex-col gap-1.5 text-xs text-muted">
      {label}
      <input className={`${fieldClass} ${className}`} {...props} />
    </label>
  );
}
