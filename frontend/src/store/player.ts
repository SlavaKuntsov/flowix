"use client";
import { create } from "zustand";

interface PlayerState {
  playbackRate: number;
  setPlaybackRate: (v: number) => void;
}

export const usePlayer = create<PlayerState>((set) => ({
  playbackRate: 1,
  setPlaybackRate: (v) => set({ playbackRate: v }),
}));
