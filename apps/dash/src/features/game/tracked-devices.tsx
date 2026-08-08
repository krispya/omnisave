import { Carousel } from '../../components/carousel.js';
import type { GameProvenance } from '../../lib/omnisave-api.js';
import { formatDate, formatRelativeDate } from '../../lib/format.js';

function DeviceIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      className="size-5"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M17.32 5H6.68a4 4 0 0 0-3.98 3.59C2.6 9.42 2 14.46 2 16a3 3 0 0 0 3 3c1 0 1.5-.5 2-1l1.41-1.41A2 2 0 0 1 9.83 16h4.34a2 2 0 0 1 1.42.59L17 18c.5.5 1 1 2 1a3 3 0 0 0 3-3c0-1.54-.6-6.58-.68-7.26A4 4 0 0 0 17.32 5Z" />
      <path d="M6.5 11h4M8.5 9v4M15 12h.01M18 10h.01" />
    </svg>
  );
}

function PlayIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-5" fill="currentColor" aria-label="Playing now">
      <path d="M8 5.14v13.72c0 .8.87 1.3 1.56.88l11.05-6.86a1.03 1.03 0 0 0 0-1.76L9.56 4.26A1.03 1.03 0 0 0 8 5.14Z" />
    </svg>
  );
}

// Active devices first (installed before not installed), untracked last;
// most recently seen wins within a group.
function provenanceRank(record: GameProvenance) {
  if (record.untracked_at) return 2;
  return record.installed ? 0 : 1;
}

function DeviceCard({ record }: { record: GameProvenance }) {
  // The server is the only clock: it announces devices.changed when a report
  // ages out, so a served playing flag is credible as long as we hold it.
  const untracked = Boolean(record.untracked_at);
  const playing = !untracked && record.playing === true;
  const status = record.untracked_at
    ? `Untracked ${formatDate(record.untracked_at)}`
    : playing
      ? 'Playing now'
      : record.installed
        ? 'Installed'
        : 'Not installed';

  return (
    <div
      title={`Tracked since ${formatDate(record.first_tracked_at)} · Last seen ${formatDate(record.last_seen_at)}`}
      className={`flex min-w-56 shrink-0 snap-start items-center gap-3 rounded-lg border bg-surface py-3 pr-5 pl-3.5 ${
        playing ? 'border-accent/50' : 'border-outline'
      } ${untracked ? 'opacity-60' : ''}`}
    >
      <div
        className={`grid size-9 shrink-0 place-items-center rounded-sm bg-text/8 ${
          untracked ? 'text-muted' : playing ? 'text-accent' : 'text-text/70'
        }`}
      >
        {playing ? <PlayIcon /> : <DeviceIcon />}
      </div>
      <div className="min-w-0">
        <p className="flex items-center gap-2 text-sm font-medium text-text">
          <span className="truncate">{record.device_name}</span>
          <span
            className={`size-1.5 shrink-0 rounded-full ${
              untracked ? 'bg-text/25' : record.installed ? 'bg-accent' : 'bg-text/40'
            }`}
            aria-hidden="true"
          />
        </p>
        <p className="mt-0.5 truncate text-xs text-muted">
          {record.adapter ? `${record.adapter} · ` : ''}
          {status}
          {untracked ? '' : ` · seen ${formatRelativeDate(record.last_seen_at)}`}
        </p>
      </div>
    </div>
  );
}

export function TrackedDevices({ provenance }: { provenance: GameProvenance[] }) {
  const records = [...provenance].sort(
    (left, right) =>
      provenanceRank(left) - provenanceRank(right) ||
      Date.parse(right.last_seen_at) - Date.parse(left.last_seen_at)
  );

  return (
    <section className="mt-6" aria-label="Devices tracking this game">
      <h3 className="mb-4 text-xs font-semibold tracking-wide text-muted uppercase">Tracked on</h3>
      {records.length === 0 ? (
        <p className="rounded-lg border border-dashed border-outline px-4 py-3 text-xs text-muted">
          No devices are tracking this game yet.
        </p>
      ) : (
        // One row, paged rather than wrapped: beside the artwork the cards
        // have little width to share, and wrapping would push the saves down
        // for every extra device.
        <Carousel className="gap-3">
          {records.map((record) => (
            <DeviceCard key={record.device_id} record={record} />
          ))}
        </Carousel>
      )}
    </section>
  );
}
