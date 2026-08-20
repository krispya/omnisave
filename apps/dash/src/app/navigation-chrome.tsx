import { useState } from 'react';
import { Icon, type IconName } from '../components/icon.js';
import { RouteLink } from '../components/route-link.js';
import type { Route } from '../lib/route.js';
import { destinations } from './navigation.js';

const expandedStorageKey = 'omnisave.sidebar-expanded';

/** Persists whether the navigation rail is expanded. */
function storedExpanded() {
  try {
    return localStorage.getItem(expandedStorageKey) === 'true';
  } catch {
    return false;
  }
}

/** A navigation rail destination with an active marker. */
function RailItem({
  icon,
  label,
  expanded,
  active = false,
}: {
  icon: IconName;
  label: string;
  expanded: boolean;
  active?: boolean;
}) {
  return (
    <span className="relative block">
      {active ? (
        <span
          className="absolute inset-y-1.5 left-0 w-[3px] rounded-r-full bg-accent"
          aria-hidden="true"
        />
      ) : null}
      <span
        className={`flex items-center gap-4 rounded-md px-3 py-3 text-sm whitespace-nowrap transition-colors duration-120 ${
          active ? 'text-accent' : 'text-text/65 hover:bg-text/6 hover:text-text'
        }`}
      >
        <Icon name={icon} className="size-6 shrink-0" />
        <span
          className={`transition-opacity duration-120 ${expanded ? 'opacity-100' : 'opacity-0'}`}
          aria-hidden={expanded ? undefined : true}
        >
          {label}
        </span>
      </span>
    </span>
  );
}

/** Expandable navigation rail beside the page content. */
export function NavigationRail({ route }: { route: Route }) {
  const [expanded, setExpanded] = useState(storedExpanded);

  function toggle() {
    const next = !expanded;
    setExpanded(next);
    try {
      localStorage.setItem(expandedStorageKey, String(next));
    } catch {
      // Storage failures only disable persistence.
    }
  }

  return (
    <nav
      aria-label="Sections"
      className={`hidden shrink-0 flex-col px-3 py-3 transition-[width] duration-200 md:flex ${
        expanded ? 'w-64' : 'w-[4.5rem]'
      }`}
    >
      {/* Above the menu rather than in it: the control that opens a panel is
          not one of the things inside the panel, and it keeps its place when
          the panel underneath it appears and disappears. */}
      <button
        type="button"
        onClick={toggle}
        aria-expanded={expanded}
        className="cursor-pointer self-start rounded-md p-3 text-text/65 transition-colors duration-120 hover:bg-text/6 hover:text-text"
      >
        <Icon name="menu" className="size-6" />
        <span className="sr-only">{expanded ? 'Collapse the menu' : 'Expand the menu'}</span>
      </button>

      <div
        className={`flex flex-col overflow-hidden rounded-lg py-1 transition-colors duration-200 ${
          expanded ? 'bg-rail' : ''
        }`}
      >
        {destinations.map((destination) => (
          <RouteLink
            key={destination.id}
            to={destination.route}
            aria-current={destination.covers(route) ? 'page' : undefined}
          >
            <RailItem
              icon={destination.icon}
              label={destination.label}
              expanded={expanded}
              active={destination.covers(route)}
            />
          </RouteLink>
        ))}
      </div>
    </nav>
  );
}

/** The same destinations beneath the content, for screens without that width. */
export function NavigationBar({ route }: { route: Route }) {
  return (
    <nav
      aria-label="Sections"
      className="sticky bottom-0 z-10 flex border-t border-outline bg-rail px-2 py-1.5 md:hidden"
    >
      {destinations.map((destination) => (
        <RouteLink
          key={destination.id}
          to={destination.route}
          aria-current={destination.covers(route) ? 'page' : undefined}
          className={`flex flex-1 flex-col items-center gap-1.5 rounded-md px-2 py-2 text-[11px] transition-colors duration-120 ${
            destination.covers(route) ? 'text-accent' : 'text-text/65 hover:text-text'
          }`}
        >
          <Icon name={destination.icon} className="size-6" />
          {destination.label}
        </RouteLink>
      ))}
    </nav>
  );
}

/** Displays the current section and app-wide controls above the content. */
export function TopBar({
  title,
  back,
  pendingCount,
  onOpenRequests,
}: {
  /** The current section's title. */
  title: string;
  /** Parent route shown in place of the section title. */
  back?: Route;
  /** Devices asking to connect right now, marked with a dot on the control. */
  pendingCount: number;
  onOpenRequests: () => void;
}) {
  return (
    <header className="flex items-center justify-between gap-4 pt-3 pr-3 pl-5 sm:pr-6 sm:pl-8 lg:pr-8 lg:pl-10">
      {back ? (
        <RouteLink
          to={back}
          aria-label={`Back to ${title}`}
          className="-ml-2.5 rounded-md p-2.5 text-text/65 transition-colors duration-120 hover:bg-text/6 hover:text-text"
        >
          <Icon name="arrow-left" className="size-6" />
        </RouteLink>
      ) : (
        <h1 className="text-2xl font-semibold tracking-tight text-text">{title}</h1>
      )}
      <button
        type="button"
        onClick={onOpenRequests}
        title="Asking to connect"
        aria-label={
          pendingCount > 0 ? `Asking to connect, ${pendingCount} waiting` : 'Asking to connect'
        }
        className="relative cursor-pointer rounded-md p-2.5 text-text/65 transition-colors duration-120 hover:bg-text/6 hover:text-text"
      >
        <Icon name="devices" className="size-6" />
        {pendingCount > 0 ? (
          <span
            className="absolute top-1.5 right-1.5 size-2 rounded-full bg-accent"
            aria-hidden="true"
          />
        ) : null}
      </button>
    </header>
  );
}

/** A window-wide connection warning shown while automatic reconnection runs. */
export function ConnectionBanner({ lost }: { lost: boolean }) {
  if (!lost) return null;

  return (
    <div role="status" className="sticky top-0 z-50 bg-bg">
      <div className="flex items-center gap-3 border-b border-danger/30 bg-danger/12 px-5 py-2.5 text-sm text-danger sm:px-8">
        <span className="size-1.5 shrink-0 animate-pulse rounded-full bg-danger" aria-hidden="true" />
        Lost contact with the server. Reconnecting, and this page may be out of date.
      </div>
    </div>
  );
}
