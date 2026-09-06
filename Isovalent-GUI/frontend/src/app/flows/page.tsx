"use client";

import { useMemo, useState } from "react";
import { useStream } from "@/lib/useStream";
import type { Flow } from "@/lib/types";
import { Badge } from "@/components/StatCard";

function ep(f: Flow, side: "source" | "destination") {
  const e = f[side];
  if (!e.namespace) return e.workload ?? "world";
  return `${e.namespace}/${e.workload ?? e.podName}`;
}

export default function FlowsPage() {
  const [paused, setPaused] = useState(false);
  const [verdict, setVerdict] = useState<string>("all");
  const [ns, setNs] = useState<string>("");
  const { items, connected, clear } = useStream<Flow>("/ws/flows", 300, paused);

  const filtered = useMemo(
    () =>
      items.filter((f) => {
        if (verdict !== "all" && f.verdict !== verdict) return false;
        if (
          ns &&
          f.source.namespace !== ns &&
          f.destination.namespace !== ns
        )
          return false;
        return true;
      }),
    [items, verdict, ns],
  );

  const namespaces = useMemo(
    () =>
      [...new Set(items.flatMap((f) => [f.source.namespace, f.destination.namespace]))]
        .filter(Boolean)
        .sort() as string[],
    [items],
  );

  return (
    <div className="space-y-4">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold">Hubble Flow Stream</h1>
          <p className="text-sm text-neutral-400">
            Live L4/L7 flows{" "}
            <Badge tone={connected ? "ok" : "crit"}>
              {connected ? "streaming" : "reconnecting…"}
            </Badge>
          </p>
        </div>
        <div className="flex items-center gap-2 text-sm">
          <select
            value={verdict}
            onChange={(e) => setVerdict(e.target.value)}
            className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm"
          >
            <option value="all">all verdicts</option>
            <option value="FORWARDED">forwarded</option>
            <option value="DROPPED">dropped</option>
          </select>
          <select
            value={ns}
            onChange={(e) => setNs(e.target.value)}
            className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm"
          >
            <option value="">all namespaces</option>
            {namespaces.map((n) => (
              <option key={n}>{n}</option>
            ))}
          </select>
          <button
            onClick={() => setPaused(!paused)}
            className="rounded border border-neutral-700 bg-neutral-900 px-3 py-1.5 hover:bg-neutral-800"
          >
            {paused ? "▶ resume" : "⏸ pause"}
          </button>
          <button
            onClick={clear}
            className="rounded border border-neutral-700 bg-neutral-900 px-3 py-1.5 hover:bg-neutral-800"
          >
            clear
          </button>
        </div>
      </header>

      <div className="panel overflow-x-auto">
        <table className="w-full text-left text-xs">
          <thead className="sticky top-0 border-b border-neutral-800 bg-[color:var(--surface-1)] text-neutral-400">
            <tr>
              <th className="px-3 py-2 font-medium">time</th>
              <th className="px-3 py-2 font-medium">verdict</th>
              <th className="px-3 py-2 font-medium">source</th>
              <th className="px-3 py-2 font-medium">destination</th>
              <th className="px-3 py-2 font-medium">l4</th>
              <th className="px-3 py-2 font-medium">l7 / reason</th>
            </tr>
          </thead>
          <tbody className="mono divide-y divide-neutral-800/60">
            {filtered.length === 0 && (
              <tr>
                <td colSpan={6} className="px-3 py-8 text-center text-neutral-500">
                  waiting for flows…
                </td>
              </tr>
            )}
            {filtered.map((f, i) => (
              <tr
                key={i}
                className={f.verdict === "DROPPED" ? "bg-red-950/20" : undefined}
              >
                <td className="whitespace-nowrap px-3 py-1.5 text-neutral-500">
                  {new Date(f.time).toLocaleTimeString()}
                </td>
                <td className="px-3 py-1.5">
                  <Badge tone={f.verdict === "DROPPED" ? "crit" : "ok"}>
                    {f.verdict}
                  </Badge>
                </td>
                <td className="px-3 py-1.5">{ep(f, "source")}</td>
                <td className="px-3 py-1.5">{ep(f, "destination")}</td>
                <td className="whitespace-nowrap px-3 py-1.5 text-neutral-400">
                  {f.l4.protocol}
                  {f.l4.dstPort ? `:${f.l4.dstPort}` : ""}
                </td>
                <td className="max-w-md truncate px-3 py-1.5 text-neutral-400">
                  {f.verdict === "DROPPED"
                    ? f.dropReason
                    : f.l7?.type === "http"
                      ? `${f.l7.method} ${f.l7.url} → ${f.l7.status} (${f.l7.latencyMs?.toFixed(1)}ms)`
                      : f.l7?.type === "dns"
                        ? `DNS ${f.l7.dnsQuery} → ${f.l7.dnsRcode}`
                        : ""}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
