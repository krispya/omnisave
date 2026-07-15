import { useEffect, useRef } from 'react';

export function useDismissibleDetails() {
  const details = useRef<HTMLDetailsElement>(null);

  useEffect(() => {
    if (!details.current) return;
    const element = details.current;

    function dismiss(event: PointerEvent) {
      if (element.open && !element.contains(event.target as Node)) {
        element.removeAttribute('open');
      }
    }

    function updateDismissListener() {
      if (element.open) {
        document.addEventListener('pointerdown', dismiss);
      } else {
        document.removeEventListener('pointerdown', dismiss);
      }
    }

    element.addEventListener('toggle', updateDismissListener);
    return () => {
      element.removeEventListener('toggle', updateDismissListener);
      document.removeEventListener('pointerdown', dismiss);
    };
  }, []);

  return details;
}
