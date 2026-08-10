"use client";

import { useEffect, useState } from "react";
import { apiBase } from "@/lib/api";
import { clearLog, subscribeHealth, subscribeLog, type Health, type LogEntry } from "@/lib/log";

/**
 * Always-visible connection status + expandable log. This is the thing that
 * tells you "the backend port-forward is dead" instead of silently showing
 * empty dashboards.
 */
export function StatusBar() {
  const [health, setHealth] = useState<Health | null>(null);
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [open, setOpen] = useState(false);

  useEffect(() => subscribeHealth(setHealth), []);
  useEffect(() => subscribeLog(setEntries), []);

  const apiOk = health?.apiOk;
  const errors = entries.filter((e) => e.level === "error").length;

  const dot = apiOk === null ? "bg-neutral-500" : apiOk ? "bg-emerald-500" : "bg-red-500";
  const label = apiOk === null ? "connecting…" : apiOk ? "API connected" : "API unreachable";

  return (
    <>
      {apiOk === false && (
        <div className="mb-4 rounded border border-red-900 bg-red-950/60 px-4 py-3 text-sm text-red-200">
          <div className="font-medium">Can&apos;t reach the backend API at {apiBase()}</div>
          <div className="mt-1 text-xs text-red-300/80">
            {health?.apiError}
          </div>
          <div className="mt-2 text-xs text-red-300/80">
            The API is served through this same origin, so this page loading
            means the forward is alive — the frontend pod just can&apos;t reach
            the backend Service. Check the backend pod, then reconnect:
          </div>
          <pre className="mono mt-2 overflow-x-auto rounded bg-black/30 p-2 text-[11px] text-red-200">{`kubectl -n isovalent-control get pods
./connect.sh`}</pre>
          <div className="mt-1 text-[11px] text-red-300/60">
            proxy diagnostics: <a className="underline" href="/_ic/proxy" target="_blank" rel="noreferrer">/_ic/proxy</a>
          </div>
        </div>
      )}

      <button
        onClick={() => setOpen((v) => !v)}
        className="fixed bottom-3 right-3 z-40 flex items-center gap-2 rounded-full border border-neutral-700 bg-[color:var(--surface-1)] px-3 py-1.5 text-xs text-neutral-300 shadow-lg hover:bg-neutral-800"
        title="Connection status and client log"
      >
        <span className={`inline-block h-2 w-2 rounded-full ${dot}`} />
        {label}
        {errors > 0 && (
          <span className="rounded bg-red-950 px-1.5 py-0.5 text-[10px] text-red-300">{errors}</span>
        )}
        <span className="text-neutral-500">{open ? "▾" : "▴"}</span>
      </button>

      {open && (
        <div className="fixed bottom-14 right-3 z-40 flex h-96 w-[42rem] max-w-[92vw] flex-col rounded-lg border border-neutral-700 bg-[color:var(--surface-1)] shadow-2xl">
          <div className="flex items-center gap-3 border-b border-neutral-800 px-3 py-2 text-xs">
            <span className="font-medium text-neutral-200">Diagnostics</span>
            <span className="text-neutral-500">api: {apiBase()}</span>
            {health?.mode && <span className="text-neutral-500">mode: {health.mode}</span>}
            <span className={apiOk ? "text-emerald-400" : "text-red-400"}>{label}</span>
            <span className="text-neutral-500">
              ws: flows {health?.wsFlows ? "✓" : "✗"} · events {health?.wsEvents ? "✓" : "✗"} · alerts{" "}
              {health?.wsAlerts ? "✓" : "✗"}
            </span>
            <button onClick={clearLog} className="ml-auto text-neutral-400 hover:text-neutral-200">
              clear
            </button>
          </div>
          <div className="mono flex-1 overflow-y-auto p-2 text-[11px] leading-relaxed">
            {entries.length === 0 && <div className="p-2 text-neutral-600">no activity yet</div>}
            {entries.map((e, i) => (
              <div
                key={i}
                className={
                  e.level === "error"
                    ? "text-red-400"
                    : e.level === "warn"
                      ? "text-amber-400"
                      : "text-neutral-400"
                }
              >
                <span className="text-neutral-600">{new Date(e.time).toLocaleTimeString()} </span>
                <span className="text-neutral-500">[{e.source}]</span> {e.message}
                {e.detail && <span className="text-neutral-600"> — {e.detail}</span>}
              </div>
            ))}
          </div>
        </div>
      )}
    </>
  );
}
