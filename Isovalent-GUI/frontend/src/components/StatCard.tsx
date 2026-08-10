export function StatCard({
  label,
  value,
  sub,
  accent,
}: {
  label: string;
  value: string;
  sub?: string;
  accent?: "blue" | "red" | "aqua" | "orange";
}) {
  const bar =
    accent === "red"
      ? "bg-series-red"
      : accent === "aqua"
        ? "bg-series-aqua"
        : accent === "orange"
          ? "bg-series-orange"
          : "bg-series-blue";
  return (
    <div className="panel flex items-stretch overflow-hidden">
      <div className={`w-1 ${bar}`} />
      <div className="px-4 py-3">
        <div className="text-[11px] uppercase tracking-wider text-neutral-400">
          {label}
        </div>
        <div className="mt-1 text-2xl font-semibold tabular-nums">{value}</div>
        {sub && <div className="mt-0.5 text-xs text-neutral-500">{sub}</div>}
      </div>
    </div>
  );
}

export function Badge({
  children,
  tone,
}: {
  children: React.ReactNode;
  tone: "ok" | "warn" | "crit" | "muted";
}) {
  const cls = {
    ok: "bg-emerald-950 text-emerald-300 border-emerald-800",
    warn: "bg-amber-950 text-amber-300 border-amber-800",
    crit: "bg-red-950 text-red-300 border-red-800",
    muted: "bg-neutral-800 text-neutral-400 border-neutral-700",
  }[tone];
  return (
    <span
      className={`inline-block rounded border px-1.5 py-0.5 text-[11px] font-medium ${cls}`}
    >
      {children}
    </span>
  );
}
