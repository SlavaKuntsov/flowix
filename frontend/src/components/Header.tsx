"use client";
import Link from "next/link";
import { useEffect } from "react";
import { useAuth } from "@/store/auth";

export default function Header() {
  const { email, hydrated, init, logout } = useAuth();

  useEffect(() => {
    init();
  }, [init]);

  return (
    <header className="sticky top-0 z-10 border-b bg-white/80 backdrop-blur">
      <div className="mx-auto flex max-w-6xl items-center justify-between px-4 py-3">
        <Link href="/" className="flex items-center gap-2 text-xl font-bold tracking-tight">
          <span className="rounded bg-red-500 px-2 py-1 text-white">▶</span> Flowix
        </Link>
        <nav className="flex items-center gap-3">
          <Link href="/upload" className="rounded-full bg-black px-4 py-2 text-sm font-medium text-white hover:bg-zinc-800">
            Upload
          </Link>
          {!hydrated ? (
            <span className="text-sm text-zinc-400">…</span>
          ) : email ? (
            <>
              <span className="hidden text-sm text-zinc-600 sm:inline">{email}</span>
              <button onClick={logout} className="rounded border px-3 py-1.5 text-sm hover:bg-zinc-50">
                Logout
              </button>
            </>
          ) : (
            <>
              <Link href="/login" className="rounded border px-3 py-1.5 text-sm hover:bg-zinc-50">
                Login
              </Link>
              <Link href="/register" className="rounded bg-red-500 px-3 py-1.5 text-sm font-medium text-white hover:bg-red-600">
                Sign up
              </Link>
            </>
          )}
        </nav>
      </div>
    </header>
  );
}
