export function formatDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return 'Unknown date';
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(date);
}

/** Revisions minutes apart are common, so history needs the clock time as well. */
export function formatDateTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return 'Unknown date';
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(date);
}

/** Compact stamp for history rows: the clock today, the day this year, the year before that. */
export function formatHistoryStamp(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return 'unknown';

  const now = new Date();
  if (date.toDateString() === now.toDateString()) {
    return new Intl.DateTimeFormat(undefined, { timeStyle: 'short' }).format(date);
  }
  if (date.getFullYear() === now.getFullYear()) {
    return new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric' }).format(date);
  }
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(date);
}

export function formatBytes(size: number) {
  const units = ['B', 'KB', 'MB', 'GB'];
  let value = size;
  let index = 0;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index += 1;
  }
  return `${index === 0 ? value : value.toFixed(1)} ${units[index]}`;
}

export function formatRelativeDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return 'unknown';
  const days = Math.floor((Date.now() - date.getTime()) / 86_400_000);
  if (days <= 0) return 'today';
  if (days === 1) return 'yesterday';
  if (days < 30) return `${days} days ago`;
  return formatDate(value);
}
