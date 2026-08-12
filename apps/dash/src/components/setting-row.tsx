import type { ReactNode } from 'react';
import { Icon, type IconName } from './icon.js';

/** A settings row with an icon, current value, and optional trailing control. */
type RowProps = {
  icon?: IconName;
  title: ReactNode;
  subtitle?: ReactNode;
  trailing?: ReactNode;
};

function RowBody({ icon, title, subtitle, trailing }: RowProps) {
  return (
    <>
      {icon ? <Icon name={icon} className="size-5 shrink-0 text-muted" /> : null}
      <span className="min-w-0 flex-1">
        <span className="block truncate text-[13px] text-text">{title}</span>
        {subtitle ? <span className="block truncate text-xs text-muted">{subtitle}</span> : null}
      </span>
      {trailing}
    </>
  );
}

const rowShell = 'flex w-full items-center gap-4 px-4 py-3 text-left';

/** A row that only reports. */
export function SettingRow(props: RowProps) {
  return (
    <div className={rowShell}>
      <RowBody {...props} />
    </div>
  );
}

/** A row that is itself the control: the whole row is the click target. */
export function ActionRow({
  onClick,
  disabled,
  ...props
}: RowProps & { onClick: () => void; disabled?: boolean }) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={`${rowShell} transition duration-120 hover:bg-text/5 disabled:cursor-not-allowed disabled:opacity-40`}
    >
      <RowBody
        {...props}
        trailing={
          props.trailing ?? <Icon name="chevron-right" className="size-5 shrink-0 text-muted" />
        }
      />
    </button>
  );
}

/** A settings row whose trailing control is a switch. */
export function SwitchRow({
  checked,
  onChange,
  disabled,
  label,
  ...props
}: RowProps & {
  checked: boolean;
  onChange: () => void;
  disabled?: boolean;
  /** Names the switch for a reader who reaches it without seeing the row. */
  label: string;
}) {
  return (
    <div className={rowShell}>
      <RowBody
        {...props}
        trailing={
          <button
            type="button"
            role="switch"
            aria-checked={checked}
            aria-label={label}
            disabled={disabled}
            onClick={onChange}
            className={`relative h-6 w-11 shrink-0 rounded-full transition duration-200 disabled:cursor-not-allowed disabled:opacity-40 ${
              checked ? 'bg-accent' : 'bg-text/20'
            }`}
          >
            <span
              className={`absolute top-1 size-4 rounded-full transition-all duration-200 ${
                checked ? 'left-[1.5rem] bg-bg' : 'left-1 bg-text/60'
              }`}
            />
          </button>
        }
      />
    </div>
  );
}
