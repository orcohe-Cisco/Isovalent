"use client";

import { useEffect, useMemo, useState } from "react";
import { apiGet, apiPost, apiPut } from "@/lib/api";
import { useStream } from "@/lib/useStream";
import type { Alert, AlertRoute, HistoryRecord } from "@/lib/types";
import { Badge } from "@/components/StatCard";

const TYPES: AlertRoute["type"][] = ["slack", "webhook", "pagerduty", "splunk"];

function verdictTone(v?: string): "crit" | "warn" | "ok" | "muted" {
  if (v === "killed" || v === "blocked") return "crit";
  if (v === "monitored") return "warn";
  return "muted";
}

/* ------------------------------- events tab ------------------------------- */
function EventsTab() {
  const [history, setHistory] = useState<Alert[]>([]);
  const [mins, setMins] = useState(60);
  const [cat, setCat] = useState<string>("all");
  const [verdict, setVerdict] = useState<string>("all");
  const { items: live } = useStream<Alert>("/ws/alerts", 200);

  useEffect(() => {
    const since = new Date(Date.now() - mins * 60_000).toISOString();
    apiGet<HistoryRecord[]>(`/api/v1/history/alert?since=${encodeURIComponent(since)}&limit=1000`)
      .then((recs) => setHistory(recs.map((r) => r.payload as Alert)))
      .catch(() => {});
  }, [mins]);

  const all = useMemo(() => {
    const seen = new Set<string>();
    const merged: Alert[] = [];
    for (const a of [...live, ...history]) {
      const k = a.time + a.title;
      if (seen.has(k)) continue;
      seen.add(k);
      merged.push(a);
    }
    return merged
      .filter((a) => cat === "all" || a.category === cat)
      .filter((a) => verdict === "all" || a.verdict === verdict)
      .sort((x, y) => (x.time < y.time ? 1 : -1));
  }, [live, history, cat, verdict]);

  const counts = useMemo(() => {
    const c = { blocked: 0, killed: 0, monitored: 0 };
    for (const a of all) if (a.verdict && a.verdict in c) c[a.verdict as keyof typeof c]++;
    return c;
  }, [all]);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <Badge tone="crit">{counts.blocked} blocked (Cilium)</Badge>
        <Badge tone="crit">{counts.killed} killed (Tetragon)</Badge>
        <Badge tone="warn">{counts.monitored} monitored (Tetragon)</Badge>
        <div className="ml-auto flex items-center gap-2 text-sm">
          <select value={cat} onChange={(e) => setCat(e.target.value)} className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5">
            <option value="all">all engines</option>
            <option value="network">network (Cilium)</option>
            <option value="runtime">runtime (Tetragon)</option>
          </select>
          <select value={verdict} onChange={(e) => setVerdict(e.target.value)} className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5">
            <option value="all">all verdicts</option>
            <option value="blocked">blocked</option>
            <option value="killed">killed</option>
            <option value="monitored">monitored</option>
          </select>
          <select value={mins} onChange={(e) => setMins(Number(e.target.value))} className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5">
            <option value={15}>15m</option>
            <option value={60}>1h</option>
            <option value={360}>6h</option>
            <option value={1440}>24h</option>
            <option value={20160}>14 days</option>
          </select>
        </div>
      </div>

      <div className="panel overflow-x-auto">
        <table className="w-full text-left text-xs">
          <thead className="border-b border-neutral-800 text-neutral-400">
            <tr>
              <th className="px-3 py-2 font-medium">time</th>
              <th className="px-3 py-2 font-medium">verdict</th>
              <th className="px-3 py-2 font-medium">engine</th>
              <th className="px-3 py-2 font-medium">workload</th>
              <th className="px-3 py-2 font-medium">rule / policy</th>
              <th className="px-3 py-2 font-medium">event</th>
            </tr>
          </thead>
          <tbody className="mono divide-y divide-neutral-800/60">
            {all.length === 0 && (
              <tr><td colSpan={6} className="px-3 py-8 text-center text-neutral-500">no enforcement events in range</td></tr>
            )}
            {all.slice(0, 500).map((a, i) => (
              <tr key={i} className={a.verdict === "killed" || a.verdict === "blocked" ? "bg-red-950/20" : undefined}>
                <td className="whitespace-nowrap px-3 py-1.5 text-neutral-500">{new Date(a.time).toLocaleTimeString()}</td>
                <td className="px-3 py-1.5"><Badge tone={verdictTone(a.verdict)}>{a.verdict ?? a.kind}</Badge></td>
                <td className="px-3 py-1.5 text-neutral-400">{a.engine}</td>
                <td className="px-3 py-1.5">{a.namespace ? `${a.namespace}/${a.workload}` : a.workload}</td>
                <td className="max-w-xs truncate px-3 py-1.5">
                  {a.policy && <span className="text-series-blue">{a.policy}</span>}
                  {a.policy && a.rule && " · "}
                  {a.rule && <span className="text-neutral-400">{a.rule}</span>}
                </td>
                <td className="max-w-md truncate px-3 py-1.5 text-neutral-400">{a.event || a.detail}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

/* ------------------------------ routing tab ------------------------------- */
function emptyRoute(): AlertRoute {
  return { id: `r${Math.floor(Date.now() % 1e6)}`, name: "New route", type: "slack", url: "", minSeverity: "warning", enabled: true };
}

function RoutingTab() {
  const [routes, setRoutes] = useState<AlertRoute[]>([]);
  const [status, setStatus] = useState<{ ok: boolean; msg: string } | null>(null);
  const [testing, setTesting] = useState<string | null>(null);

  useEffect(() => {
    apiGet<AlertRoute[]>("/api/v1/alerts/routes").then(setRoutes).catch((e) => setStatus({ ok: false, msg: String(e) }));
  }, []);
  const update = (i: number, patch: Partial<AlertRoute>) => setRoutes((p) => p.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));
  const save = async () => {
    try { setRoutes(await apiPut<AlertRoute[]>("/api/v1/alerts/routes", routes)); setStatus({ ok: true, msg: "Saved" }); }
    catch (e) { setStatus({ ok: false, msg: String(e) }); }
  };
  const test = async (r: AlertRoute) => {
    setTesting(r.id);
    try { await apiPost("/api/v1/alerts/routes/test", r); setStatus({ ok: true, msg: `Delivered to ${r.name}` }); }
    catch (e) { setStatus({ ok: false, msg: `Test failed: ${String(e)}` }); }
    finally { setTesting(null); }
  };

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <p className="text-sm text-neutral-400">Forward enforcement events to Slack, PagerDuty, webhooks, or a Splunk/SIEM HEC. Deduplicated per kind+title in a 60s window.</p>
        <button onClick={() => setRoutes((r) => [...r, emptyRoute()])} className="rounded border border-neutral-700 bg-neutral-900 px-3 py-1.5 text-sm hover:bg-neutral-800">＋ Add route</button>
      </div>
      {status && <div className={`rounded px-3 py-2 text-xs ${status.ok ? "bg-emerald-950 text-emerald-300" : "bg-red-950 text-red-300"}`}>{status.msg}</div>}
      {routes.map((r, i) => (
        <div key={r.id} className="panel space-y-3 p-4">
          <div className="flex flex-wrap items-center gap-3">
            <input value={r.name} onChange={(e) => update(i, { name: e.target.value })} className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm font-medium" />
            <select value={r.type} onChange={(e) => update(i, { type: e.target.value as AlertRoute["type"] })} className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm">
              {TYPES.map((t) => <option key={t}>{t}</option>)}
            </select>
            <select value={r.minSeverity} onChange={(e) => update(i, { minSeverity: e.target.value as "warning" | "critical" })} className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm">
              <option value="warning">≥ warning</option>
              <option value="critical">critical only</option>
            </select>
            <label className="flex cursor-pointer items-center gap-1.5 text-xs text-neutral-300"><input type="checkbox" checked={r.enabled} onChange={(e) => update(i, { enabled: e.target.checked })} />enabled</label>
            <div className="ml-auto flex gap-2">
              <button onClick={() => test(r)} disabled={testing === r.id || !r.url} className="rounded border border-neutral-700 px-3 py-1.5 text-xs hover:bg-neutral-800 disabled:opacity-40">{testing === r.id ? "…" : "Test"}</button>
              <button onClick={() => setRoutes((p) => p.filter((_, idx) => idx !== i))} className="rounded border border-red-900 bg-red-950 px-3 py-1.5 text-xs text-red-300 hover:bg-red-900/50">Remove</button>
            </div>
          </div>
          <input value={r.url} placeholder="destination URL" onChange={(e) => update(i, { url: e.target.value })} className="mono w-full rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-xs" />
          {(r.type === "pagerduty" || r.type === "splunk") && (
            <input value={r.token ?? ""} placeholder={r.type === "pagerduty" ? "Routing key" : "HEC token"} onChange={(e) => update(i, { token: e.target.value })} className="mono w-full rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-xs" />
          )}
        </div>
      ))}
      {routes.length > 0 && <button onClick={save} className="rounded bg-series-blue px-4 py-2 text-sm font-medium text-white hover:brightness-110">Save configuration</button>}
    </div>
  );
}

export default function AlertsPage() {
  const [tab, setTab] = useState<"events" | "routing">("events");
  return (
    <div className="space-y-4">
      <header>
        <h1 className="text-lg font-semibold">Security Events &amp; Alerting</h1>
        <p className="text-sm text-neutral-400">
          Everything blocked by Cilium or killed/monitored by Tetragon — with the
          rule that matched and the related event. Retained for the configured
          window (14 days by default; Postgres for durable storage).
        </p>
      </header>
      <div className="flex gap-1 border-b border-neutral-800">
        {(["events", "routing"] as const).map((t) => (
          <button key={t} onClick={() => setTab(t)}
            className={`px-4 py-2 text-sm ${tab === t ? "border-b-2 border-series-blue text-white" : "text-neutral-400 hover:text-neutral-200"}`}>
            {t === "events" ? "Enforcement Log" : "Alert Routing"}
          </button>
        ))}
      </div>
      {tab === "events" ? <EventsTab /> : <RoutingTab />}
    </div>
  );
}
