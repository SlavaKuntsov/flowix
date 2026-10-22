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
    <div className="mx-auto max-w-sm">
      <h1 className="text-xl font-semibold">Login</h1>
      <form onSubmit={onSubmit} className="mt-6 space-y-4">
        <input value={email} onChange={(e) => setEmail(e.target.value)} placeholder="Email" type="email" required className="w-full rounded border px-3 py-2 text-sm" />
        <input value={password} onChange={(e) => setPassword(e.target.value)} placeholder="Password" type="password" required className="w-full rounded border px-3 py-2 text-sm" />
        {(localError || error) && <div className="rounded bg-red-50 p-3 text-sm text-red-700">{localError || error}</div>}
        <button disabled={loading} className="w-full rounded bg-black py-2 text-sm font-medium text-white hover:bg-zinc-800 disabled:opacity-50">
          {loading ? "Loading…" : "Login"}
        </button>
      </form>
      <p className="mt-4 text-center text-sm text-zinc-500">
        No account? <Link href="/register" className="text-red-600 hover:underline">Register</Link>
      </p>
    </div>
  );
}
