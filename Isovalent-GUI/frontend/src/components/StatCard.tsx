const ACCENTS = {
  blue: "#3987e5",
  red: "#e66767",
  aqua: "#199e70",
  orange: "#d95926",
} as const;

export function StatCard({
  label,
  value,
  sub,
  accent = "blue",
}: {
  label: string;
  value: string;
  sub?: string;
  accent?: keyof typeof ACCENTS;
}) {
  const color = ACCENTS[accent];
  return (
    <div className="panel relative overflow-hidden px-4 py-3.5">
      {/* A wash rather than a hard bar: the colour reads as a category cue
          without turning the row of cards into a set of traffic lights. */}
      <span
        aria-hidden
        className="pointer-events-none absolute inset-0"
        style={{
          background: `linear-gradient(105deg, ${color}16 0%, transparent 38%)`,
        }}
      />
      <span
        aria-hidden
        className="absolute inset-y-0 left-0 w-[2px]"
        style={{ background: color, opacity: 0.85 }}
      />
      <div className="relative">
        <div className="section-label">{label}</div>
        <div className="mt-1.5 text-[26px] font-semibold leading-none tabular-nums tracking-[-0.02em]">
          {value}
        </div>
        {sub && (
          <div className="mt-1.5 text-xs text-[color:var(--text-tertiary)]">
            {sub}
          </div>
        )}
      </div>
    </div>
  );
}

const BADGE_TONES = {
  ok: { bg: "rgba(25,158,112,0.14)", fg: "#6dd3ab", bd: "rgba(25,158,112,0.32)" },
  warn: { bg: "rgba(201,133,0,0.16)", fg: "#e0b25a", bd: "rgba(201,133,0,0.34)" },
  crit: { bg: "rgba(230,103,103,0.14)", fg: "#f0a3a3", bd: "rgba(230,103,103,0.32)" },
  muted: {
    bg: "rgba(255,255,255,0.06)",
    fg: "var(--text-secondary)",
    bd: "var(--hairline)",
  },
} as const;

export function Badge({
  children,
  tone,
}: {
  children: React.ReactNode;
  tone: keyof typeof BADGE_TONES;
}) {
  const t = BADGE_TONES[tone];
  return (
    <span
      className="inline-flex items-center rounded-md border px-1.5 py-0.5 text-[11px] font-medium leading-4"
      style={{ background: t.bg, color: t.fg, borderColor: t.bd }}
    >
      {children}
    </span>
  );
}
