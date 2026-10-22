import type { Config } from "tailwindcss";

// Semantic tokens live in globals.css as RGB channel triplets, so opacity
// modifiers (bg-accent/10, ring-accent/40) keep working while the palette
// flips with the `.dark` class.
const token = (name: string) => `rgb(var(${name}) / <alpha-value>)`;

const config: Config = {
  darkMode: "class",
  content: ["./src/**/*.{js,ts,jsx,tsx,mdx}", "./src/app/**/*.{js,ts,jsx,tsx,mdx}"],
  theme: {
    extend: {
      colors: {
        bg: token("--bg"),
        surface: {
          DEFAULT: token("--surface"),
          muted: token("--surface-muted"),
          hover: token("--surface-hover"),
        },
        content: {
          DEFAULT: token("--fg"),
          muted: token("--fg-muted"),
          subtle: token("--fg-subtle"),
        },
        line: token("--border"),
        accent: {
          DEFAULT: token("--accent"),
          hover: token("--accent-hover"),
          text: token("--accent-text"),
          soft: token("--accent-soft"),
          fg: token("--accent-fg"),
        },
      },
      borderColor: {
        DEFAULT: token("--border"),
      },
      fontFamily: {
        sans: ["var(--font-inter)", "system-ui", "-apple-system", "Segoe UI", "sans-serif"],
      },
      boxShadow: {
        surface: "var(--shadow-surface)",
        "surface-hover": "var(--shadow-surface-hover)",
      },
    },
  },
  plugins: [],
};
export default config;
