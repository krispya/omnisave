import type { ButtonHTMLAttributes } from 'react';

/** Visual button variants for primary, secondary, dismissive, and destructive actions. */
export type ButtonVariant = 'filled' | 'outline' | 'plain' | 'danger';

const variants: Record<ButtonVariant, string> = {
  filled: 'bg-text text-bg hover:bg-white',
  outline: 'border border-outline text-text hover:bg-text/8',
  plain: 'text-muted hover:bg-text/8 hover:text-text',
  danger: 'border border-danger/40 text-danger hover:bg-danger/10',
};

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant;
  /** Pill by default, matching every other control; square for icon buttons. */
  shape?: 'pill' | 'square';
};

export function Button({
  variant = 'outline',
  shape = 'pill',
  className = '',
  type = 'button',
  ...props
}: ButtonProps) {
  return (
    <button
      type={type}
      className={`shrink-0 px-4 py-2 text-sm font-medium transition duration-120 disabled:cursor-not-allowed disabled:opacity-40 ${
        shape === 'pill' ? 'rounded-full' : 'rounded-sm'
      } ${variants[variant]} ${className}`}
      {...props}
    />
  );
}
