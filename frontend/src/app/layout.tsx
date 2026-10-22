import Header from "@/components/Header";
import ScrollToTop from "@/components/ScrollToTop";
import ThemeProvider from "@/components/ThemeProvider";
import type { Metadata } from "next";
import { Inter } from "next/font/google";
import "./globals.css";

const inter = Inter({
  subsets: ["latin", "cyrillic"],
  display: "swap",
  variable: "--font-inter",
});

export const metadata: Metadata = {
  title: "Flowix — Video Streaming",
  description: "Open-source adaptive HLS platform",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className={inter.variable} suppressHydrationWarning>
      <body className="flex min-h-dvh flex-col">
        <ThemeProvider>
          <ScrollToTop />
          <Header />
          <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-8">{children}</main>
          <footer className="border-t border-line">
            <div className="mx-auto flex max-w-6xl flex-col items-center gap-1 px-4 py-6 text-center text-xs text-content-subtle sm:flex-row sm:justify-between sm:text-left">
              <span>Flowix — open-source adaptive HLS platform</span>
              <span className="tabular-nums">gateway :8080 · nginx-vod :8081</span>
            </div>
          </footer>
        </ThemeProvider>
      </body>
    </html>
  );
}
