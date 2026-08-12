import type { AnchorHTMLAttributes, ReactNode } from 'react';
import { handledByRouter, navigate, routePath, type Route } from '../lib/route.js';

type RouteLinkProps = Omit<AnchorHTMLAttributes<HTMLAnchorElement>, 'href'> & {
  to: Route;
  children: ReactNode;
};

/** A real anchor that handles ordinary Dash navigation without reloading. */
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
