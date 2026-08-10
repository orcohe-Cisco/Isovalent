"use client";

import { useCallback, useEffect, useState } from "react";
import { apiGet } from "@/lib/api";
import type { Alert, Flow, HistoryRecord, TetragonEvent } from "@/lib/types";
import { Badge } from "@/components/StatCard";

type Kind = "flow" | "event" | "alert";
const RANGES = [
  { label: "last 15m", mins: 15 },
  { label: "last 1h", mins: 60 },
  { label: "last 6h", mins: 360 },
  { label: "last 24h", mins: 1440 },
];

export default function HistoryPage() {
  const [kind, setKind] = useState<Kind>("flow");
  const [mins, setMins] = useState(60);
  const [records, setRecords] = useState<HistoryRecord[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const since = new Date(Date.now() - mins * 60_000).toISOString();
      const data = await apiGet<HistoryRecord[]>(
        `/api/v1/history/${kind}?since=${encodeURIComponent(since)}&limit=500`,
      );
      setRecords(data ?? []);
      setError(null);
    } catch (e) {
      setError(String(e));
    } finally {
      setLoading(false);
    }
  }, [kind, mins]);

  useEffect(() => {
    load();
  }, [load]);

  return (
    <div className="space-y-4">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold">Historical Investigation</h1>
          <p className="text-sm text-neutral-400">
            Time-travel over persisted flows, runtime events, and alerts
            (in-memory by default; Postgres when <span className="mono">IC_DB_DSN</span> is set).
          </p>
        </div>
        <div className="flex items-center gap-2 text-sm">
          <select value={kind} onChange={(e) => setKind(e.target.value as Kind)} className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5">
            <option value="flow">flows</option>
            <option value="event">runtime events</option>
            <option value="alert">alerts</option>
          </select>
          <select value={mins} onChange={(e) => setMins(Number(e.target.value))} className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5">
            {RANGES.map((r) => (
              <option key={r.mins} value={r.mins}>{r.label}</option>
            ))}
          </select>
          <button onClick={load} className="rounded border border-neutral-700 bg-neutral-900 px-3 py-1.5 hover:bg-neutral-800">
            {loading ? "…" : "↻ refresh"}
          </button>
        </div>
      </header>

      {error && <div className="rounded bg-red-950 px-3 py-2 text-xs text-red-300">{error}</div>}

      <div className="panel overflow-x-auto">
        <div className="border-b border-neutral-800 px-4 py-2 text-xs text-neutral-400">
          {records.length} {kind} record(s)
        </div>
        <table className="w-full text-left text-xs">
          <thead className="text-neutral-500">
            <tr>
              <th className="px-3 py-2 font-medium">time</th>
              <th className="px-3 py-2 font-medium">summary</th>
            </tr>
          </thead>
          <tbody className="mono divide-y divide-neutral-800/60">
            {records.length === 0 && (
              <tr><td colSpan={2} className="px-3 py-8 text-center text-neutral-500">no records in range</td></tr>
            )}
            {records.map((r, i) => (
              <tr key={i}>
                <td className="whitespace-nowrap px-3 py-1.5 text-neutral-500">{new Date(r.time).toLocaleString()}</td>
                <td className="px-3 py-1.5">{summarize(kind, r.payload)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function summarize(kind: Kind, payload: unknown): React.ReactNode {
  if (kind === "flow") {
    const f = payload as Flow;
    const s = f.source?.namespace ? `${f.source.namespace}/${f.source.workload}` : "world";
    const d = f.destination?.namespace ? `${f.destination.namespace}/${f.destination.workload}` : "world";
    return (
      <span>
        <Badge tone={f.verdict === "DROPPED" ? "crit" : "ok"}>{f.verdict}</Badge>{" "}
        {s} → {d} {f.l4?.protocol}:{f.l4?.dstPort}
        {f.l7?.type === "http" && <span className="text-neutral-500"> {f.l7.method} {f.l7.url} → {f.l7.status}</span>}
      </span>
    );
  }
  if (kind === "event") {
    const e = payload as TetragonEvent;
    return (
      <span>
        <Badge tone={e.action === "SIGKILL" ? "crit" : "muted"}>{e.type}</Badge>{" "}
        {e.namespace}/{e.workload} {e.binary} {e.action && <span className="text-red-400">☠ {e.action}</span>}
      </span>
    );
  }
  const a = payload as Alert;
  return (
    <span>
      <Badge tone={a.severity === "critical" ? "crit" : "warn"}>{a.severity}</Badge> {a.title}
    </span>
  );
}
