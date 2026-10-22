"use client";
import { useAuth } from "@/store/auth";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";

export default function LoginPage() {
  const router = useRouter();
  const { login, loading, error } = useAuth();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
  const message = localError || error;

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLocalError(null);
    try {
      await login(email, password);
      router.push("/");
    } catch (err) {
      setLocalError((err as Error).message);
    }
  };

  return (
    <div className="mx-auto mt-6 w-full max-w-sm">
      <div className="surface p-6 sm:p-8">
        <h1 className="text-xl font-semibold tracking-tight text-content">Welcome back</h1>
        <p className="mt-1 text-sm text-content-muted">Sign in to upload and manage your videos.</p>

        <form onSubmit={onSubmit} className="mt-6 space-y-4">
          <div>
            <label htmlFor="email" className="label">
              Email
            </label>
            <input
              id="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="you@example.com"
              type="email"
              autoComplete="email"
              required
              className="input"
            />
          </div>
          <div>
            <label htmlFor="password" className="label">
              Password
            </label>
            <input
              id="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="••••••••"
              type="password"
              autoComplete="current-password"
              required
              className="input"
            />
          </div>

          {message && (
            <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-600 dark:text-red-300">
              {message}
            </div>
          )}

          <button disabled={loading} className="btn btn-primary w-full">
            {loading ? "Loading…" : "Login"}
          </button>
        </form>

        <p className="mt-6 text-center text-sm text-content-muted">
          No account?{" "}
          <Link href="/register" className="font-medium text-accent-text hover:underline">
            Register
          </Link>
        </p>
      </div>
    </div>
  );
}
