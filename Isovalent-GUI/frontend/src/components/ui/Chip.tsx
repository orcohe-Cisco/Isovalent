import type { ReactNode } from "react";

export type ChipTone =
  | "neutral"
  | "accent"
  | "ok"
  | "warn"
  | "danger"
  | "violet";

const TONES: Record<ChipTone, { bg: string; fg: string; bd: string }> = {
  neutral: {
    bg: "rgba(255,255,255,0.06)",
    fg: "var(--text-secondary)",
    bd: "var(--hairline)",
  },
  accent: { bg: "rgba(57,135,229,0.14)", fg: "#8fbdf3", bd: "rgba(57,135,229,0.3)" },
  ok: { bg: "rgba(25,158,112,0.14)", fg: "#6dd3ab", bd: "rgba(25,158,112,0.3)" },
  warn: { bg: "rgba(201,133,0,0.16)", fg: "#e0b25a", bd: "rgba(201,133,0,0.34)" },
  danger: { bg: "rgba(230,103,103,0.14)", fg: "#f0a3a3", bd: "rgba(230,103,103,0.32)" },
  violet: { bg: "rgba(144,133,233,0.14)", fg: "#b3aaf2", bd: "rgba(144,133,233,0.3)" },
};

export function Chip({
  children,
  tone = "neutral",
  title,
  mono,
}: {
  children: ReactNode;
  tone?: ChipTone;
  title?: string;
  mono?: boolean;
}) {
  const t = TONES[tone];
  return (
    <span
      title={title}
      className={`inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-[10px] font-medium leading-4 ${
        mono ? "mono" : ""
      }`}
      style={{ background: t.bg, color: t.fg, borderColor: t.bd }}
    >
      {children}
    </span>
  );
}
