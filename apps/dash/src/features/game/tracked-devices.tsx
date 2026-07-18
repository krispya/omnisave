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

// Active devices first (installed before not installed), untracked last;
// most recently seen wins within a group.
function provenanceRank(record: GameProvenance) {
  if (record.untracked_at) return 2;
  return record.installed ? 0 : 1;
}

function DeviceCard({ record }: { record: GameProvenance }) {
  const untracked = Boolean(record.untracked_at);
  const status = record.untracked_at
    ? `Untracked ${formatDate(record.untracked_at)}`
    : record.installed
      ? 'Installed'
      : 'Not installed';

  return (
    <div
      title={`Tracked since ${formatDate(record.first_tracked_at)} · Last seen ${formatDate(record.last_seen_at)}`}
      className={`flex min-w-56 items-center gap-3 rounded-md border border-white/5 bg-[#1a1a1a] py-3 pr-5 pl-3.5 ${
        untracked ? 'opacity-60' : ''
      }`}
    >
      <div
        className={`grid size-9 shrink-0 place-items-center rounded bg-white/5 ${
          untracked ? 'text-slate-500' : 'text-[#e5a00d]'
        }`}
      >
        <DeviceIcon />
      </div>
      <div className="min-w-0">
        <p className="flex items-center gap-2 text-sm font-medium text-white">
          <span className="truncate">{record.device_name}</span>
          <span
            className={`size-1.5 shrink-0 rounded-full ${
              untracked ? 'bg-slate-600' : record.installed ? 'bg-emerald-400' : 'bg-slate-500'
            }`}
            aria-hidden="true"
          />
        </p>
        <p className="mt-0.5 truncate text-xs text-slate-500">
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
      <h3 className="mb-4 text-sm font-semibold text-white">Tracked on</h3>
      {records.length === 0 ? (
        <p className="rounded-md border border-dashed border-white/10 bg-white/[0.02] px-4 py-3 text-xs text-slate-500">
          No devices are tracking this game yet.
        </p>
      ) : (
        <div className="flex flex-wrap gap-3">
          {records.map((record) => (
            <DeviceCard key={record.device_id} record={record} />
          ))}
        </div>
      )}
    </section>
  );
}
