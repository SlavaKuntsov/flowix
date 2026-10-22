"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect } from "react";
import ThemeToggle from "@/components/ThemeToggle";
import { useAuth } from "@/store/auth";

function PlayMark() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden className="translate-x-[1px]">
      <path d="M8 5.14v13.72a1 1 0 0 0 1.54.84l10.3-6.86a1 1 0 0 0 0-1.68L9.54 4.3A1 1 0 0 0 8 5.14z" />
    </svg>
  );
}

const LINKS = [
  { href: "/", label: "Home" },
  { href: "/upload", label: "Upload" },
];

function NavLink({ href, label }: { href: string; label: string }) {
  const pathname = usePathname();
  const active = href === "/" ? pathname === "/" : pathname.startsWith(href);
  return (
    <Link
      href={href}
      aria-current={active ? "page" : undefined}
      className={`rounded-lg px-3 py-2 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/45 ${
        active ? "bg-surface-muted text-content" : "text-content-muted hover:bg-surface-muted hover:text-content"
      }`}
    >
      {label}
    </Link>
  );
}

export default function Header() {
  const { email, hydrated, init, logout } = useAuth();

  useEffect(() => {
    init();
  }, [init]);

  return (
    <header className="sticky top-0 z-40 border-b border-line bg-bg/80 backdrop-blur-md">
      <div className="mx-auto flex h-16 max-w-6xl items-center gap-2 px-4">
        <Link
          href="/"
          className="mr-1 flex items-center gap-2.5 rounded-lg text-lg font-bold tracking-tight text-content focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/45"
        >
          <span className="flex size-8 items-center justify-center rounded-xl bg-accent text-accent-fg shadow-[0_1px_2px_rgb(0_0_0/0.2)]">
            <PlayMark />
          </span>
          Flowix
        </Link>

        <nav className="ml-1 hidden items-center gap-1 sm:flex">
          {LINKS.map((l) => (
            <NavLink key={l.href} {...l} />
          ))}
        </nav>

        <div className="ml-auto flex items-center gap-1.5">
          <ThemeToggle />
          {hydrated && email && <span className="mx-1 hidden h-6 w-px bg-line sm:block" />}
          {!hydrated ? (
            <span className="size-8 animate-pulse rounded-full bg-surface-muted" aria-hidden />
          ) : email ? (
            <>
              <span className="hidden max-w-[14rem] truncate text-sm text-content-muted sm:inline">{email}</span>
              <button onClick={logout} className="btn btn-ghost">
                Logout
              </button>
            </>
          ) : (
            <>
              <Link href="/login" className="btn btn-ghost">
                Login
              </Link>
              <Link href="/register" className="btn btn-primary">
                Sign up
              </Link>
            </>
          )}
        </div>
      </div>
    </header>
  );
}
