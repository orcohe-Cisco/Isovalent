import type { Config } from "tailwindcss";

export default {
  content: ["./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        // Validated dark-mode dataviz slots (see docs/architecture.md).
        series: {
          blue: "#3987e5",
          orange: "#d95926",
          aqua: "#199e70",
          yellow: "#c98500",
          red: "#e66767",
          violet: "#9085e9",
        },
        // Surface ramp — mirrors the CSS custom properties in globals.css so
        // both `bg-surface-2` and `bg-[color:var(--surface-2)]` work.
        surface: {
          0: "#0e0e0d",
          1: "#171716",
          2: "#1f1f1e",
          3: "#292928",
        },
        hairline: {
          DEFAULT: "rgba(255,255,255,0.07)",
          strong: "rgba(255,255,255,0.12)",
        },
        ink: {
          primary: "#f5f5f3",
          secondary: "#a8a79e",
          tertiary: "#6f6e68",
        },
      },
      transitionTimingFunction: {
        out: "cubic-bezier(0.32, 0.72, 0, 1)",
        smooth: "cubic-bezier(0.65, 0, 0.35, 1)",
      },
      boxShadow: {
        e1: "0 1px 2px rgba(0,0,0,0.4)",
        e2: "0 8px 24px -8px rgba(0,0,0,0.6)",
        e3: "0 24px 64px -16px rgba(0,0,0,0.75)",
      },
      borderRadius: {
        xl: "12px",
        "2xl": "16px",
      },
      fontSize: {
        // A tighter, more deliberate scale than Tailwind's default.
        "2xs": ["10px", { lineHeight: "14px", letterSpacing: "0.02em" }],
      },
    },
  },
  plugins: [],
} satisfies Config;
