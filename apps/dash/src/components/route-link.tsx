import type { AnchorHTMLAttributes, ReactNode } from 'react';
import { handledByRouter, navigate, routePath, type Route } from '../lib/route.js';

type RouteLinkProps = Omit<AnchorHTMLAttributes<HTMLAnchorElement>, 'href'> & {
  to: Route;
  children: ReactNode;
};

/**
 * A link to a Dash route. It is a real anchor, so it can be middle-clicked, opened in a
 * new tab, and copied; an ordinary click is handled in place instead of reloading the app.
 */
export function RouteLink({ to, children, onClick, ...props }: RouteLinkProps) {
  return (
    <a
      href={routePath(to)}
      onClick={(event) => {
        onClick?.(event);
        if (!handledByRouter(event)) return;
        event.preventDefault();
        navigate(to);
      }}
      {...props}
    >
      {children}
    </a>
  );
}
