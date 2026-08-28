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
    <div className="mx-auto max-w-sm">
      <h1 className="text-xl font-semibold">Create account</h1>
      <form onSubmit={onSubmit} className="mt-6 space-y-4">
        <input value={email} onChange={(e) => setEmail(e.target.value)} placeholder="Email" type="email" required className="w-full rounded border px-3 py-2 text-sm" />
        <input value={password} onChange={(e) => setPassword(e.target.value)} placeholder="Password (min 8)" type="password" required minLength={8} className="w-full rounded border px-3 py-2 text-sm" />
        {(localError || error) && <div className="rounded bg-red-50 p-3 text-sm text-red-700">{localError || error}</div>}
        <button disabled={loading} className="w-full rounded bg-red-500 py-2 text-sm font-medium text-white hover:bg-red-600 disabled:opacity-50">
          {loading ? "Loading…" : "Register"}
        </button>
      </form>
      <p className="mt-4 text-center text-sm text-zinc-500">
        Have account? <Link href="/login" className="text-red-600 hover:underline">Login</Link>
      </p>
    </div>
  );
}
