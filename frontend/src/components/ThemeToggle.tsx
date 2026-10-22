"use client";
import { useEffect, useState } from "react";
import { useTheme } from "next-themes";

function SunIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" />
    </svg>
  );
}

function MoonIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
    </svg>
  );
}

/**
 * Theme switch. Both icons stay in the DOM and cross-fade (opacity + scale +
 * blur) so enter and exit both animate, with no motion dependency.
 */
export default function ThemeToggle() {
  const { resolvedTheme, setTheme } = useTheme();
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);

  const isDark = resolvedTheme === "dark";
  const fade = "transition-[opacity,filter,scale] duration-300 ease-[cubic-bezier(0.2,0,0,1)]";
  const shown = "scale-100 opacity-100 blur-0";
  const hidden = "scale-[0.25] opacity-0 blur-[4px]";

  return (
    <button
      type="button"
      onClick={() => setTheme(isDark ? "light" : "dark")}
      aria-label="Toggle theme"
      title="Toggle theme"
      className="relative flex size-10 items-center justify-center rounded-full text-content-muted transition-colors hover:bg-surface-muted hover:text-content focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/45 focus-visible:ring-offset-2 focus-visible:ring-offset-bg"
    >
      <span className="relative size-[18px]">
        <span className={`absolute inset-0 flex items-center justify-center ${fade} ${!mounted || isDark ? hidden : shown}`}>
          <SunIcon />
        </span>
        <span className={`absolute inset-0 flex items-center justify-center ${fade} ${mounted && isDark ? shown : hidden}`}>
          <MoonIcon />
        </span>
      </span>
    </button>
  );
}
