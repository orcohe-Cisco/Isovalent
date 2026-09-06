"use client";

import { useMemo, useState } from "react";
import { usePoll } from "@/lib/usePoll";
import { useStream } from "@/lib/useStream";
import type { Diagnostics, LogLine } from "@/lib/types";
import { PageHeader } from "@/components/ui/PageHeader";
import { ErrorNote } from "@/components/ui/ErrorNote";
import { Chip } from "@/components/ui/Chip";

const STATUS_TONE = { ok: "ok", degraded: "warn", down: "danger", disabled: "neutral" } as const;
const LEVEL_COLOR: Record<string, string> = {
  ERROR: "#f0a3a3",
  WARN: "#e0b25a",
  INFO: "var(--text-secondary)",
  DEBUG: "var(--text-tertiary)",
};

/**
 * Diagnostics.
 *
 * The page to open when a table is unexpectedly empty. Each check probes one
 * dependency and, when it fails, says what to do about it — an indicator that
 * goes red and stops has moved the problem rather than solved it.
 */
export default function DiagnosticsPage() {
  const { data, error, busy, reload } = usePoll<Diagnostics>("/api/v1/diagnostics", 15000);
  const [level, setLevel] = useState("INFO");
  const [origin, setOrigin] = useState("all");
  const [q, setQ] = useState("");
  const [live, setLive] = useState(true);

  const logPath = useMemo(() => {
    const p = new URLSearchParams({ level, origin, limit: "300" });
    if (q.trim()) p.set("q", q.trim());
    return `/api/v1/logs?${p.toString()}`;
  }, [level, origin, q]);

  const logs = usePoll<{ lines: LogLine[]; counts: Record<string, number> }>(
    logPath,
    live ? 0 : 10000,
  );
  const streamed = useStream<LogLine>("/ws/logs", 300, !live);

  const lines = live
    ? [...streamed.items, ...(logs.data?.lines ?? [])].slice(0, 300)
    : (logs.data?.lines ?? []);

  return (
    <>
      <PageHeader
        title="Diagnostics"
        subtitle="Connectivity to everything the console depends on, and the logs from both halves of it in one timeline."
        actions={
          <>
            {data && (
              <Chip tone={STATUS_TONE[data.status as keyof typeof STATUS_TONE] ?? "neutral"}>
                {data.status}
              </Chip>
            )}
            <button className="btn btn-secondary" onClick={() => void reload()} disabled={busy}>
              {busy ? "Checking…" : "Re-check"}
            </button>
          </>
        }
      />

      <ErrorNote error={error} />

      <div className="mb-5 grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {(data?.checks ?? []).map((c) => (
          <section key={c.id} className="panel px-4 py-3">
            <div className="flex items-center gap-2">
              <span
                aria-hidden
                className="inline-block h-2 w-2 shrink-0 rounded-full"
                style={{
                  background:
                    c.status === "ok"
                      ? "#199e70"
                      : c.status === "degraded"
                        ? "#c98500"
                        : c.status === "down"
                          ? "#e66767"
                          : "#6f6e68",
                }}
              />
              <h3 className="text-[13px] font-medium">{c.name}</h3>
              <span className="ml-auto">
                <Chip tone={STATUS_TONE[c.status] ?? "neutral"}>{c.status}</Chip>
              </span>
            </div>
            {c.target && (
              <div className="mono mt-1 truncate text-[11px] text-[color:var(--text-tertiary)]">
                {c.target}
                {c.latencyMs ? ` · ${c.latencyMs}ms` : ""}
              </div>
            )}
            {c.detail && (
              <p className="mt-1.5 text-[12px] leading-relaxed text-[color:var(--text-secondary)]">
                {c.detail}
              </p>
            )}
            {c.hint && (
              <p className="mt-1.5 text-[12px] leading-relaxed" style={{ color: "#e0b25a" }}>
                {c.hint}
              </p>
            )}
          </section>
        ))}
      </div>

      <div className="panel overflow-hidden">
        <div className="hairline-b flex flex-wrap items-center gap-2 px-4 py-2.5">
          <span className="text-[13px] font-medium">Console logs</span>
          <div className="flex gap-1">
            {["DEBUG", "INFO", "WARN", "ERROR"].map((l) => (
              <button
                key={l}
                onClick={() => setLevel(l)}
                className={`btn ${l === level ? "btn-secondary" : "btn-ghost"} !px-2 !py-1 !text-[11px]`}
              >
                {l}
              </button>
            ))}
          </div>
          <div className="flex gap-1">
            {["all", "backend", "frontend"].map((o) => (
              <button
                key={o}
                onClick={() => setOrigin(o)}
                className={`btn ${o === origin ? "btn-secondary" : "btn-ghost"} !px-2 !py-1 !text-[11px]`}
              >
                {o}
              </button>
            ))}
          </div>
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Filter…"
            className="field !w-48 !py-1 !text-[12px]"
          />
          <label className="ml-auto flex items-center gap-1.5 text-[12px] text-[color:var(--text-secondary)]">
            <input type="checkbox" checked={live} onChange={(e) => setLive(e.target.checked)} />
            Live {live && (streamed.connected ? "· connected" : "· reconnecting")}
          </label>
        </div>
        <ul className="max-h-[26rem] overflow-y-auto">
          {lines.map((l, i) => (
            <li key={`${l.time}-${i}`} className="hairline-t px-4 py-1.5">
              <div className="flex flex-wrap items-baseline gap-2">
                <span className="mono text-[11px] text-[color:var(--text-tertiary)]">
                  {new Date(l.time).toLocaleTimeString()}
                </span>
                <span
                  className="mono w-12 shrink-0 text-[10px] font-medium"
                  style={{ color: LEVEL_COLOR[l.level?.toUpperCase()] ?? "var(--text-secondary)" }}
                >
                  {l.level}
                </span>
                <Chip>{l.origin}</Chip>
                <span className="mono min-w-0 flex-1 text-[12px]">{l.message}</span>
              </div>
              {l.fields && Object.keys(l.fields).length > 0 && (
                <div className="mono mt-0.5 pl-[7.5rem] text-[10px] text-[color:var(--text-tertiary)]">
                  {Object.entries(l.fields)
                    .filter(([k]) => k !== "time" && k !== "level" && k !== "msg")
                    .map(([k, v]) => `${k}=${v}`)
                    .join("  ")}
                </div>
              )}
            </li>
          ))}
          {lines.length === 0 && (
            <li className="px-4 py-6 text-center text-[12px] text-[color:var(--text-tertiary)]">
              No log lines at this level.
            </li>
          )}
        </ul>
      </div>
    </>
  );
}
