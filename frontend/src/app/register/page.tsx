"use client";
import { useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { useAuth } from "@/store/auth";

export default function RegisterPage() {
  const router = useRouter();
  const { register, loading, error } = useAuth();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
  const message = localError || error;

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLocalError(null);
    try {
      await register(email, password);
      router.push("/");
    } catch (err) {
      setLocalError((err as Error).message);
    }
  };

  return (
    <div className="mx-auto mt-6 w-full max-w-sm">
      <div className="surface p-6 sm:p-8">
        <h1 className="text-xl font-semibold tracking-tight text-content">Create account</h1>
        <p className="mt-1 text-sm text-content-muted">Start uploading and streaming in a minute.</p>

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
              placeholder="At least 8 characters"
              type="password"
              autoComplete="new-password"
              required
              minLength={8}
              className="input"
            />
          </div>

          {message && (
            <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-600 dark:text-red-300">
              {message}
            </div>
          )}

          <button disabled={loading} className="btn btn-primary w-full">
            {loading ? "Loading…" : "Register"}
          </button>
        </form>

        <p className="mt-6 text-center text-sm text-content-muted">
          Have account?{" "}
          <Link href="/login" className="font-medium text-accent-text hover:underline">
            Login
          </Link>
        </p>
      </div>
    </div>
  );
}
