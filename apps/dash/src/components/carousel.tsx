import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { Icon } from './icon.js';

/** A horizontally scrolling row with page controls and native touch scrolling. */
export function Carousel({
  className = 'gap-4',
  children,
}: {
  /** Spacing between items, as a gap utility. */
  className?: string;
  children: ReactNode;
}) {
  const scroller = useRef<HTMLDivElement>(null);
  const [reach, setReach] = useState({ back: false, forward: false });

  const measure = useCallback(() => {
    const node = scroller.current;
    if (!node) return;
    const back = node.scrollLeft > 1;
    const forward = node.scrollLeft + node.clientWidth < node.scrollWidth - 1;
    setReach((current) =>
      current.back === back && current.forward === forward ? current : { back, forward }
    );
  }, []);

  // Re-measure after content changes move either end of the row.
  useEffect(() => {
    measure();
  });

  useEffect(() => {
    const node = scroller.current;
    if (!node) return;
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    return () => observer.disconnect();
  }, [measure]);

  function page(direction: 1 | -1) {
    const node = scroller.current;
    if (!node) return;
    node.scrollBy({ left: direction * node.clientWidth * 0.9, behavior: 'smooth' });
  }

  return (
    <div className="group relative">
      <div
        ref={scroller}
        onScroll={measure}
        className={`-m-1 flex snap-x overflow-x-auto p-1 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden ${className}`}
      >
        {children}
      </div>
      {reach.back ? <Paddle side="back" onPage={() => page(-1)} /> : null}
      {reach.forward ? <Paddle side="forward" onPage={() => page(1)} /> : null}
    </div>
  );
}

function Paddle({ side, onPage }: { side: 'back' | 'forward'; onPage: () => void }) {
  return (
    <button
      type="button"
      onClick={onPage}
      aria-label={side === 'back' ? 'Scroll back' : 'Scroll forward'}
      className={`absolute top-1/2 z-10 grid size-9 -translate-y-1/2 cursor-pointer place-items-center rounded-full border border-outline bg-rail text-text/70 opacity-0 transition duration-120 group-hover:opacity-100 hover:text-text focus-visible:opacity-100 ${
        side === 'back' ? 'left-1' : 'right-1'
      }`}
    >
      <Icon name={side === 'back' ? 'chevron-left' : 'chevron-right'} className="size-5" />
    </button>
  );
}
