"use client";
import { login as apiLogin, register as apiRegister, fetchMe } from "@/lib/api";
import { create } from "zustand";

interface AuthState {
  token: string | null;
  email: string | null;
  userId: string | null;
  loading: boolean;
  error: string | null;
  hydrated: boolean;
  init: () => Promise<void>;
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, password: string) => Promise<void>;
  logout: () => void;
}

export const useAuth = create<AuthState>((set) => ({
  token: null,
  email: null,
  userId: null,
  loading: false,
  error: null,
  hydrated: false,

  init: async () => {
    if (typeof window === "undefined") return;
    const token = localStorage.getItem("access_token");
    if (!token) {
      set({ hydrated: true });
      return;
    }
    set({ token, loading: true });
    try {
      const me = await fetchMe();
      set({ email: me.email, userId: me.id, loading: false, hydrated: true });
    } catch {
      localStorage.removeItem("access_token");
      localStorage.removeItem("refresh_token");
      set({ token: null, email: null, userId: null, loading: false, hydrated: true });
    }
  },

  login: async (email, password) => {
    set({ loading: true, error: null });
    try {
      const res = await apiLogin(email, password);
      localStorage.setItem("access_token", res.access_token);
      localStorage.setItem("refresh_token", res.refresh_token);
      const me = await fetchMe();
      set({ token: res.access_token, email: me.email, userId: me.id, loading: false });
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
      throw e;
    }
  },

  register: async (email, password) => {
    set({ loading: true, error: null });
    try {
      const res = await apiRegister(email, password);
      localStorage.setItem("access_token", res.access_token);
      localStorage.setItem("refresh_token", res.refresh_token);
      const me = await fetchMe();
      set({ token: res.access_token, email: me.email, userId: me.id, loading: false });
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
      throw e;
    }
  },

  logout: () => {
    localStorage.removeItem("access_token");
    localStorage.removeItem("refresh_token");
    set({ token: null, email: null, userId: null });
  },
}));
