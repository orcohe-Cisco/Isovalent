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
      },
    },
  },
  plugins: [],
} satisfies Config;
