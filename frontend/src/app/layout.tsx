import Header from "@/components/Header";
import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Flowix — Video Streaming",
  description: "Open-source adaptive HLS platform",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <Header />
        <main className="mx-auto max-w-6xl px-4 py-6">{children}</main>
        <footer className="mx-auto max-w-6xl px-4 py-8 text-center text-xs text-zinc-400">
          Flowix — open-source HLS • gateway :8080 • vod :8081
        </footer>
      </body>
    </html>
  );
}
