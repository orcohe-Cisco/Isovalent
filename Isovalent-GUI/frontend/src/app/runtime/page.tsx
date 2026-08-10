"use client";

import { useMemo, useState } from "react";
import { useStream } from "@/lib/useStream";
import type { TetragonEvent } from "@/lib/types";
import { Badge } from "@/components/StatCard";

const typeLabels: Record<string, string> = {
  process_exec: "EXEC",
  process_exit: "EXIT",
  process_kprobe: "KPROBE",
  process_tracepoint: "TRACEPOINT",
};

export default function RuntimePage() {
  const [paused, setPaused] = useState(false);
  const [onlyEnforced, setOnlyEnforced] = useState(false);
  const { items, connected } = useStream<TetragonEvent>("/ws/events", 300, paused);

  const filtered = useMemo(
    () =>
      onlyEnforced
        ? items.filter((e) => e.action === "SIGKILL" || e.action === "OVERRIDE")
        : items,
    [items, onlyEnforced],
  );

  return (
    <div className="space-y-4">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold">Runtime Security — Tetragon</h1>
          <p className="text-sm text-neutral-400">
            Process, syscall and enforcement events{" "}
            <Badge tone={connected ? "ok" : "crit"}>
              {connected ? "streaming" : "reconnecting…"}
            </Badge>
          </p>
        </div>
        <div className="flex items-center gap-2 text-sm">
          <label className="flex cursor-pointer items-center gap-2 text-neutral-300">
            <input
              type="checkbox"
              checked={onlyEnforced}
              onChange={(e) => setOnlyEnforced(e.target.checked)}
              className="accent-red-500"
            />
            enforcement only
          </label>
          <button
            onClick={() => setPaused(!paused)}
            className="rounded border border-neutral-700 bg-neutral-900 px-3 py-1.5 hover:bg-neutral-800"
          >
            {paused ? "▶ resume" : "⏸ pause"}
          </button>
        </div>
      </header>

      <div className="panel overflow-x-auto">
        <table className="w-full text-left text-xs">
          <thead className="border-b border-neutral-800 text-neutral-400">
            <tr>
              <th className="px-3 py-2 font-medium">time</th>
              <th className="px-3 py-2 font-medium">event</th>
              <th className="px-3 py-2 font-medium">workload</th>
              <th className="px-3 py-2 font-medium">process</th>
              <th className="px-3 py-2 font-medium">hook / details</th>
              <th className="px-3 py-2 font-medium">action</th>
              <th className="px-3 py-2 font-medium">policy</th>
            </tr>
          </thead>
          <tbody className="mono divide-y divide-neutral-800/60">
            {filtered.length === 0 && (
              <tr>
                <td colSpan={7} className="px-3 py-8 text-center text-neutral-500">
                  waiting for events…
                </td>
              </tr>
            )}
            {filtered.map((e, i) => {
              const enforced = e.action === "SIGKILL" || e.action === "OVERRIDE";
              return (
                <tr key={i} className={enforced ? "bg-red-950/25" : undefined}>
                  <td className="whitespace-nowrap px-3 py-1.5 text-neutral-500">
                    {new Date(e.time).toLocaleTimeString()}
                  </td>
                  <td className="px-3 py-1.5">
                    <Badge tone={enforced ? "crit" : e.type === "process_kprobe" ? "warn" : "muted"}>
                      {typeLabels[e.type] ?? e.type}
                    </Badge>
                  </td>
                  <td className="px-3 py-1.5">
                    {e.namespace ? `${e.namespace}/${e.workload}` : e.node}
                  </td>
                  <td className="max-w-xs truncate px-3 py-1.5">
                    {e.binary}
                    {e.args && <span className="text-neutral-500"> {e.args}</span>}
                  </td>
                  <td className="max-w-xs truncate px-3 py-1.5 text-neutral-400">
                    {e.function}
                    {e.details && <span className="text-neutral-500"> {e.details}</span>}
                  </td>
                  <td className="px-3 py-1.5">
                    {e.action &&
                      (enforced ? (
                        <Badge tone="crit">☠ {e.action}</Badge>
                      ) : (
                        <span className="text-neutral-400">{e.action}</span>
                      ))}
                  </td>
                  <td className="px-3 py-1.5 text-neutral-400">{e.policy}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
