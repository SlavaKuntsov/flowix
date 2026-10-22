"use client";
import { usePathname } from "next/navigation";
import { useEffect, useLayoutEffect, useRef } from "react";

// useLayoutEffect warns during SSR; fall back to useEffect on the server.
const useIsomorphicLayoutEffect = typeof window !== "undefined" ? useLayoutEffect : useEffect;

/**
 * Reset scroll to the top on forward navigations. Without this, opening a video
 * from far down the feed leaves the new page scrolled to the bottom (the short
 * loading shell gets clamped to the bottom of its own height).
 *
 * Back/forward (popstate) is left to the browser's own scroll restoration.
 */
export default function ScrollToTop() {
  const pathname = usePathname();
  const popped = useRef(false);

  useEffect(() => {
    const onPop = () => {
      popped.current = true;
    };
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);

  // Layout effect so the jump happens before paint — no flash of the bottom.
  useIsomorphicLayoutEffect(() => {
    if (popped.current) {
      popped.current = false;
      return;
    }
    window.scrollTo({ top: 0, left: 0, behavior: "auto" });
  }, [pathname]);

  return null;
}
