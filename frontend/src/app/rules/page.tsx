"use client";

import { useCallback, useEffect, useState } from "react";
import { apiDelete, apiGet, apiPost } from "@/lib/api";
import type { TracingPolicyInfo } from "@/lib/types";
import { Badge } from "@/components/StatCard";

const CATEGORY_LABELS: Record<string, string> = {
  egress: "Network egress",
  file: "File integrity",
  exec: "Process execution",
  privilege: "Privilege escalation",
  capability: "Capabilities",
  "": "Uncategorized",
};

function ActionToggle({
  policy,
  onChange,
}: {
  policy: TracingPolicyInfo;
  onChange: (action: "monitor" | "enforce") => Promise<void>;
}) {
  const [busy, setBusy] = useState(false);
  const enforce = policy.action === "enforce";
  return (
    <div className="flex items-center gap-1 rounded-md border border-neutral-700 bg-neutral-900 p-0.5 text-xs">
      {(["monitor", "enforce"] as const).map((mode) => {
        const active = policy.action === mode;
        return (
          <button
            key={mode}
            disabled={busy}
            onClick={async () => {
              if (active) return;
              setBusy(true);
              try {
                await onChange(mode);
              } finally {
                setBusy(false);
              }
            }}
            className={`rounded px-2.5 py-1 font-medium transition-colors disabled:opacity-50 ${
              active
                ? mode === "enforce"
                  ? "bg-series-red text-white"
                  : "bg-series-blue text-white"
                : "text-neutral-400 hover:text-neutral-200"
            }`}
          >
            {mode === "monitor" ? "◉ Monitor" : "☠ Kill"}
          </button>
        );
      })}
      {busy && <span className="px-1 text-neutral-500">…</span>}
      {!busy && <span className="sr-only">{enforce ? "enforcing" : "monitoring"}</span>}
    </div>
  );
}

export default function RulesPage() {
  const [policies, setPolicies] = useState<TracingPolicyInfo[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setPolicies(await apiGet<TracingPolicyInfo[]>("/api/v1/tracingpolicies"));
      setError(null);
    } catch (e) {
      setError(String(e));
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const toggle = async (p: TracingPolicyInfo, action: "monitor" | "enforce") => {
    const ns = p.namespace || "-";
    setError(null);
    try {
      await apiPost(`/api/v1/tracingpolicies/${ns}/${p.name}/action`, { action });
      setNote(`${p.name} → ${action === "enforce" ? "KILL" : "monitor"}`);
      setPolicies((prev) =>
        prev.map((x) => (x.name === p.name && x.namespace === p.namespace ? { ...x, action } : x)),
      );
    } catch (e) {
      setError(String(e));
    }
  };

  const remove = async (p: TracingPolicyInfo) => {
    if (!confirm(`Remove policy "${p.name}"? It will be deleted from the cluster.`)) return;
    const ns = p.namespace || "-";
    setError(null);
    try {
      await apiDelete(`/api/v1/policies/${p.kind}/${ns}/${p.name}`);
      setNote(`Removed ${p.name}`);
      setPolicies((prev) => prev.filter((x) => !(x.name === p.name && x.namespace === p.namespace)));
    } catch (e) {
      setError(String(e));
    }
  };

  const cats = [...new Set(policies.map((p) => p.category ?? ""))].sort();
  const enforcing = policies.filter((p) => p.action === "enforce").length;

  return (
    <div className="space-y-5">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-semibold">Runtime Policies (Tetragon)</h1>
          <p className="text-sm text-neutral-400">
            Suggested best-practice TracingPolicies, organized by category.
            Toggle each between <span className="text-series-blue">Monitor</span> (observe) and{" "}
            <span className="text-series-red">Kill</span> (enforce), or{" "}
            <span className="text-neutral-300">Remove</span> any you don&apos;t want — changes apply immediately.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Badge tone="muted">{policies.length} policies</Badge>
          <Badge tone={enforcing > 0 ? "crit" : "ok"}>{enforcing} enforcing</Badge>
        </div>
      </header>

      {error && <div className="rounded bg-red-950 px-3 py-2 text-xs text-red-300">{error}</div>}
      {note && <div className="rounded bg-emerald-950 px-3 py-2 text-xs text-emerald-300">{note}</div>}

      {policies.length === 0 && !error && (
        <div className="panel p-6 text-sm text-neutral-500">
          No TracingPolicies found. Apply the defaults with{" "}
          <code className="mono">kubectl apply -f policies/tetragon/</code>.
        </div>
      )}

      {cats.map((cat) => {
        const items = policies.filter((p) => (p.category ?? "") === cat);
        if (items.length === 0) return null;
        return (
          <section key={cat} className="panel overflow-hidden">
            <div className="border-b border-neutral-800 bg-[color:var(--surface-2)] px-4 py-2 text-xs font-medium uppercase tracking-wider text-neutral-300">
              {CATEGORY_LABELS[cat] ?? cat}
            </div>
            <ul className="divide-y divide-neutral-800/70">
              {items.map((p) => (
                <li key={`${p.namespace}/${p.name}`} className="flex items-center justify-between gap-4 px-4 py-3">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="mono text-sm">{p.name}</span>
                      {p.namespace && <Badge tone="muted">ns:{p.namespace}</Badge>}
                    </div>
                    {p.description && (
                      <div className="mt-0.5 truncate text-xs text-neutral-500">{p.description}</div>
                    )}
                    {p.hooks && p.hooks.length > 0 && (
                      <div className="mono mt-0.5 truncate text-[11px] text-neutral-600">
                        hooks: {p.hooks.join(", ")}
                      </div>
                    )}
                  </div>
                  <div className="flex items-center gap-2">
                    <ActionToggle policy={p} onChange={(a) => toggle(p, a)} />
                    <button
                      onClick={() => remove(p)}
                      title="Remove this policy from the cluster"
                      className="rounded border border-neutral-700 px-2 py-1 text-xs text-neutral-400 hover:border-red-800 hover:bg-red-950/50 hover:text-red-300"
                    >
                      Remove
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          </section>
        );
      })}
    </div>
  );
}
