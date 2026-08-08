import { useState } from 'react';
import { Icon, type IconName } from '../components/icon.js';
import { RouteLink } from '../components/route-link.js';
import type { Route } from '../lib/route.js';
import { destinations } from './navigation.js';

const expandedStorageKey = 'omnisave.sidebar-expanded';

/**
 * Whether the rail stays open. Someone who wants labels wants them every visit,
 * so the choice outlives the tab; anyone who has never made it gets the narrow
 * rail, which is the one that leaves the most room for the Library.
 */
function storedExpanded() {
  try {
    return localStorage.getItem(expandedStorageKey) === 'true';
  } catch {
    return false;
  }
}

/**
 * A rail item. The section being read is marked by color and a bar at the
 * menu's edge rather than by a filled pill: at this size a fill would be the
 * largest shape in the chrome, and what is being said is small — you are here.
 */
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

/**
 * The destinations beside the content, opened and closed by the one control
 * above them.
 *
 * Closed, the rail has no surface: icons sit directly on the page, and the
 * chrome costs the Library nothing but the width of a glyph. Opening gives it
 * a panel and moves the content over rather than covering it — the menu is
 * part of the layout, and something that appears over what you were reading is
 * a different, more interruptive thing.
 */
export function NavigationRail({ route }: { route: Route }) {
  const [expanded, setExpanded] = useState(storedExpanded);

  function toggle() {
    const next = !expanded;
    setExpanded(next);
    try {
      localStorage.setItem(expandedStorageKey, String(next));
    } catch {
      // A browser that refuses storage still gets the rail, just not the memory.
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
        className={`mt-2 flex flex-col overflow-hidden rounded-lg py-2 transition-colors duration-200 ${
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

/**
 * Said only when something is wrong.
 *
 * Being connected is the ordinary case and does not need reporting — a status
 * light that reads "Connected" every second of every session teaches people to
 * stop seeing it, which is exactly when it needs to be seen. The stream
 * reconnects on its own, so this explains the wait rather than offering a
 * button that would only do what is already happening.
 *
 * It spans the whole window, above the menu as well as the content, because it
 * is about the app rather than about the section being read; and it sticks,
 * because a warning that scrolls away has stopped warning anyone. It is opaque
 * for the same reason — content passing underneath must not show through it.
 */
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
