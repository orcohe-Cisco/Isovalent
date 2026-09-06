"use client";

import { useCallback, useMemo, useState } from "react";
import yaml from "js-yaml";
import { apiGet } from "@/lib/api";
import { usePoll } from "@/lib/usePoll";
import type { Explanation, InvestigateResult, InvestigateRow } from "@/lib/types";
import { PageHeader } from "@/components/ui/PageHeader";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorNote } from "@/components/ui/ErrorNote";
import { Chip } from "@/components/ui/Chip";
import { Sheet } from "@/components/ui/Sheet";
import { YamlBlock } from "@/components/ui/YamlBlock";

const WINDOWS = ["5m", "15m", "1h", "6h", "1d", "7d", "30d"];
const SOURCES = [
  { id: "", label: "Both" },
  { id: "cilium", label: "Cilium" },
  { id: "tetragon", label: "Tetragon" },
];

/** Facet dimensions worth showing, and the query parameter each drives. */
const FACETS: { dim: string; param: string; label: string }[] = [
  { dim: "namespaces", param: "namespace", label: "Namespace" },
  { dim: "sources", param: "src", label: "Source" },
  { dim: "destinations", param: "dst", label: "Destination" },
  { dim: "verdicts", param: "verdict", label: "Verdict" },
  { dim: "policies", param: "policy", label: "Policy" },
  { dim: "binaries", param: "binary", label: "Process" },
  { dim: "l7Methods", param: "method", label: "HTTP method" },
  { dim: "workloads", param: "workload", label: "Workload" },
  { dim: "nodes", param: "node", label: "Node" },
];

type Selections = Record<string, string[]>;

/**
 * Historical investigation.
 *
 * One search box over both data sources, because during an incident nobody
 * wants to run the Cilium query and the Tetragon query separately and then
 * reconcile the timestamps by eye. Every filter value on this page came from
 * the result set, so clicking is always a valid query — there is no way to
 * type a namespace that does not exist and conclude nothing happened.
 */
export default function InvestigatePage() {
  const [window, setWindow] = useState("1h");
  const [source, setSource] = useState("");
  const [query, setQuery] = useState("");
  const [pathQuery, setPathQuery] = useState("");
  const [blockedOnly, setBlockedOnly] = useState(false);
  const [showReplies, setShowReplies] = useState(false);
  const [sel, setSel] = useState<Selections>({});
  const [open, setOpen] = useState<InvestigateRow | null>(null);

  const path = useMemo(() => {
    const p = new URLSearchParams();
    p.set("window", window);
    if (source) p.set("source", source);
    if (query.trim()) p.set("q", query.trim());
    if (pathQuery.trim()) p.set("path", pathQuery.trim());
    if (blockedOnly) p.set("blocked", "true");
    if (showReplies) p.set("replies", "true");
    for (const [param, values] of Object.entries(sel)) {
      for (const v of values) p.append(param, v);
    }
    p.set("limit", "300");
    return `/api/v1/investigate?${p.toString()}`;
  }, [window, source, query, pathQuery, blockedOnly, showReplies, sel]);

  const { data, error, loading, busy, reload } = usePoll<InvestigateResult>(path, 15000);

  const toggle = useCallback((param: string, value: string) => {
    setSel((prev) => {
      const cur = prev[param] ?? [];
      const next = cur.includes(value) ? cur.filter((v) => v !== value) : [...cur, value];
      const out = { ...prev };
      if (next.length) out[param] = next;
      else delete out[param];
      return out;
    });
  }, []);

  const activeCount = Object.values(sel).reduce((n, v) => n + v.length, 0);

  return (
    <>
      <PageHeader
        title="Investigation"
        subtitle="Everything Cilium and Tetragon recorded, in one timeline. Click any value to filter by it."
        actions={
          <button className="btn btn-secondary" onClick={() => void reload()} disabled={busy}>
            {busy ? "Searching…" : "Refresh"}
          </button>
        }
      />

      <ErrorNote error={error} />

      {/* --- search bar ------------------------------------------------- */}
      <div className="panel mb-4 px-4 py-3">
        <div className="flex flex-wrap items-center gap-2">
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search everything — process, pod, path, policy, drop reason…"
            className="field min-w-[18rem] flex-1"
          />
          <div className="flex gap-1">
            {WINDOWS.map((w) => (
              <button
                key={w}
                onClick={() => setWindow(w)}
                className={`btn ${w === window ? "btn-secondary" : "btn-ghost"} !px-2.5 !py-1.5 !text-[12px]`}
              >
                {w}
              </button>
            ))}
          </div>
        </div>

        <div className="mt-2.5 flex flex-wrap items-center gap-3">
          <div className="flex gap-1">
            {SOURCES.map((s) => (
              <button
                key={s.id}
                onClick={() => setSource(s.id)}
                className={`btn ${s.id === source ? "btn-secondary" : "btn-ghost"} !px-2.5 !py-1.5 !text-[12px]`}
              >
                {s.label}
              </button>
            ))}
          </div>
          <label className="flex items-center gap-1.5 text-[12px] text-[color:var(--text-secondary)]">
            <input
              type="checkbox"
              checked={blockedOnly}
              onChange={(e) => setBlockedOnly(e.target.checked)}
            />
            Blocked / enforced only
          </label>
          <label className="flex items-center gap-1.5 text-[12px] text-[color:var(--text-secondary)]">
            <input
              type="checkbox"
              checked={showReplies}
              onChange={(e) => setShowReplies(e.target.checked)}
            />
            Include reply traffic
          </label>
          <input
            value={pathQuery}
            onChange={(e) => setPathQuery(e.target.value)}
            placeholder="L7 path contains… e.g. /admin"
            className="field !w-56 !py-1.5"
          />
          {activeCount > 0 && (
            <button className="btn btn-ghost !py-1.5 !text-[12px]" onClick={() => setSel({})}>
              Clear {activeCount} filter{activeCount === 1 ? "" : "s"}
            </button>
          )}
        </div>

        {activeCount > 0 && (
          <div className="mt-2.5 flex flex-wrap gap-1.5">
            {Object.entries(sel).flatMap(([param, values]) =>
              values.map((v) => (
                <button key={`${param}:${v}`} onClick={() => toggle(param, v)} title="Remove filter">
                  <Chip tone="accent" mono>
                    {param}={v} ✕
                  </Chip>
                </button>
              )),
            )}
          </div>
        )}
      </div>

      {/* --- timeline --------------------------------------------------- */}
      {data && data.series.length > 0 && <Timeline series={data.series} />}

      <div className="grid gap-4 lg:grid-cols-[15rem_minmax(0,1fr)]">
        {/* --- facets --------------------------------------------------- */}
        <aside className="space-y-3">
          {FACETS.map(({ dim, param, label }) => {
            const values = (data?.facets?.[dim] ?? []).slice(0, 8);
            if (!values.length) return null;
            return (
              <section key={dim} className="panel px-3 py-2.5">
                <div className="section-label mb-1.5">{label}</div>
                <ul className="space-y-0.5">
                  {values.map((f) => {
                    const on = (sel[param] ?? []).includes(f.value);
                    return (
                      <li key={f.value}>
                        <button
                          onClick={() => toggle(param, f.value)}
                          className="flex w-full items-center gap-2 rounded-md px-1.5 py-1 text-left text-[12px]"
                          style={{
                            background: on ? "var(--accent-soft)" : "transparent",
                            color: on ? "var(--text-primary)" : "var(--text-secondary)",
                          }}
                        >
                          <span className="mono min-w-0 flex-1 truncate">{f.value}</span>
                          <span className="mono shrink-0 text-[10px] text-[color:var(--text-tertiary)]">
                            {f.count}
                          </span>
                        </button>
                      </li>
                    );
                  })}
                </ul>
              </section>
            );
          })}
        </aside>

        {/* --- results -------------------------------------------------- */}
        <div>
          <div className="mb-2 flex items-center justify-between text-[12px] text-[color:var(--text-tertiary)]">
            <span>
              {data ? `${data.total.toLocaleString()} matching records` : "…"}
              {data?.truncated && " (showing the newest 300)"}
            </span>
            <span>scanned {data?.scanned?.toLocaleString() ?? 0}</span>
          </div>

          {!loading && data && data.rows.length === 0 ? (
            <EmptyState
              title="Nothing matched"
              body={
                <p>
                  No stored record in the last {window} matches. Widen the window, or clear the
                  filters. If every window is empty, history has not accumulated yet — the console
                  keeps an in-memory ring unless a database is configured, and it starts empty
                  after a restart.
                </p>
              }
            />
          ) : (
            <div className="panel overflow-hidden">
              <table className="w-full text-[12px]">
                <thead>
                  <tr className="section-label">
                    <th className="px-3 py-2 text-left font-medium">Time</th>
                    <th className="px-2 py-2 text-left font-medium">Source</th>
                    <th className="px-2 py-2 text-left font-medium">Verdict</th>
                    <th className="px-2 py-2 text-left font-medium">What happened</th>
                    <th className="px-2 py-2 text-left font-medium">Policy</th>
                    <th className="px-3 py-2" />
                  </tr>
                </thead>
                <tbody>
                  {(data?.rows ?? []).map((r) => (
                    <tr
                      key={r.id + r.summary}
                      className="hairline-t cursor-pointer align-top transition-colors hover:bg-[color:var(--surface-2)]"
                      onClick={() => setOpen(r)}
                    >
                      <td className="mono whitespace-nowrap px-3 py-2 text-[color:var(--text-tertiary)]">
                        {new Date(r.time).toLocaleTimeString()}
                      </td>
                      <td className="px-2 py-2">
                        <Chip tone={r.source === "cilium" ? "accent" : "violet"}>
                          {r.source === "cilium" ? "net" : "run"}
                        </Chip>
                      </td>
                      <td className="px-2 py-2">
                        <Chip tone={verdictTone(r.verdict)}>{r.verdict}</Chip>
                      </td>
                      <td className="px-2 py-2">
                        <div className="mono max-w-xl truncate">{r.summary}</div>
                        <div className="mt-0.5 flex flex-wrap gap-x-2 text-[10px] text-[color:var(--text-tertiary)]">
                          {r.namespace && <span>ns={r.namespace}</span>}
                          {r.l7Method && (
                            <span>
                              {r.l7Method} {r.l7Status ? `→ ${r.l7Status}` : ""}
                            </span>
                          )}
                          {r.user && <span>user={r.user}</span>}
                          {r.reason && <span className="truncate">{r.reason}</span>}
                        </div>
                      </td>
                      <td className="mono px-2 py-2">
                        {r.policy ? (
                          <button
                            onClick={(e) => {
                              e.stopPropagation();
                              toggle("policy", r.policyNamespace ? `${r.policyNamespace}/${r.policy}` : r.policy!);
                            }}
                            className="underline decoration-dotted underline-offset-2"
                          >
                            {r.policy}
                          </button>
                        ) : (
                          <span className="text-[color:var(--text-tertiary)]">—</span>
                        )}
                      </td>
                      <td className="px-3 py-2 text-right text-[color:var(--text-tertiary)]">›</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>

      <RecordSheet row={open} onClose={() => setOpen(null)} onFilter={toggle} />
    </>
  );
}

function verdictTone(v: string) {
  switch (v) {
    case "dropped":
    case "enforced":
      return "danger" as const;
    case "audit":
      return "warn" as const;
    case "error":
      return "warn" as const;
    default:
      return "neutral" as const;
  }
}

function Timeline({ series }: { series: { t: number; total: number; blocked: number }[] }) {
  const max = Math.max(...series.map((s) => s.total), 1);
  return (
    <div className="panel mb-4 px-4 py-3">
      <div className="section-label mb-2">Matches over time</div>
      <div className="flex h-16 items-end gap-[2px]">
        {series.map((s) => (
          <div
            key={s.t}
            className="flex-1 rounded-t-[2px]"
            title={`${new Date(s.t * 1000).toLocaleTimeString()} — ${s.total} records, ${s.blocked} blocked`}
            style={{
              height: `${Math.max((s.total / max) * 100, 3)}%`,
              background:
                s.blocked > 0
                  ? `linear-gradient(to top, var(--series-8) ${(s.blocked / s.total) * 100}%, var(--series-1) ${(s.blocked / s.total) * 100}%)`
                  : "var(--series-1)",
              opacity: 0.85,
            }}
          />
        ))}
      </div>
    </div>
  );
}

/** The drill-down: full record, plus which clause of which policy matched. */
function RecordSheet({
  row,
  onClose,
  onFilter,
}: {
  row: InvestigateRow | null;
  onClose: () => void;
  onFilter: (param: string, value: string) => void;
}) {
  const [explain, setExplain] = useState<Explanation | null>(null);
  const [explainErr, setExplainErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    if (!row?.policy) return;
    setBusy(true);
    setExplainErr(null);
    try {
      const p = new URLSearchParams({
        source: row.source,
        policy: row.policy,
        policyNamespace: row.policyNamespace ?? "",
        raw: JSON.stringify(row.raw ?? {}),
      });
      setExplain(await apiGet<Explanation>(`/api/v1/investigate/explain?${p.toString()}`));
    } catch (e) {
      setExplainErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }, [row]);

  return (
    <Sheet
      open={Boolean(row)}
      onClose={() => {
        setExplain(null);
        setExplainErr(null);
        onClose();
      }}
      width={760}
      title={row?.summary ?? ""}
      subtitle={row ? `${new Date(row.time).toLocaleString()} · ${row.source}` : undefined}
    >
      {row && (
        <div className="space-y-4">
          <div className="flex flex-wrap gap-1.5">
            <Chip tone={verdictTone(row.verdict)}>{row.verdict}</Chip>
            {row.namespace && (
              <button onClick={() => onFilter("namespace", row.namespace!)}>
                <Chip mono>ns={row.namespace}</Chip>
              </button>
            )}
            {row.src && (
              <button onClick={() => onFilter("src", row.src!)}>
                <Chip mono>src={row.src}</Chip>
              </button>
            )}
            {row.dst && (
              <button onClick={() => onFilter("dst", row.dst!)}>
                <Chip mono>dst={row.dst}</Chip>
              </button>
            )}
            {row.node && <Chip mono>node={row.node}</Chip>}
            {row.user && <Chip mono>user={row.user}</Chip>}
            {row.container && <Chip mono>container={row.container}</Chip>}
          </div>

          {row.policy ? (
            <section className="panel px-4 py-3">
              <div className="flex items-center justify-between gap-2">
                <div>
                  <div className="section-label">Attributed policy</div>
                  <div className="mono mt-0.5 text-[13px]">
                    {row.policyNamespace ? `${row.policyNamespace}/` : ""}
                    {row.policy}
                    {row.policyKind ? ` · ${row.policyKind}` : ""}
                  </div>
                </div>
                <button className="btn btn-secondary !py-1.5 !text-[12px]" onClick={load} disabled={busy}>
                  {busy ? "Checking…" : explain ? "Re-check" : "Why did this match?"}
                </button>
              </div>

              {explainErr && (
                <p className="mt-2 text-[12px]" style={{ color: "#f0a3a3" }}>
                  {explainErr}
                </p>
              )}

              {explain && (
                <div className="mt-3 space-y-3">
                  <p className="mono text-[12px] leading-relaxed text-[color:var(--text-secondary)]">
                    {explain.summary}
                  </p>
                  {explain.clauses.length > 0 && (
                    <ul className="space-y-1.5">
                      {explain.clauses.map((c, i) => (
                        <li key={i} className="rounded-lg px-3 py-2" style={{ background: "var(--surface-2)" }}>
                          <div className="mono text-[11px] text-[color:var(--accent)]">{c.path}</div>
                          <div className="mono mt-0.5 text-[12px]">
                            {c.kind} {c.operator} {(c.values ?? []).join(", ")}
                          </div>
                          {c.matched && (
                            <div className="mt-0.5 text-[11px] text-[color:var(--text-tertiary)]">
                              matched on <span className="mono">{c.matched}</span>
                              {c.argIndex !== undefined && c.argIndex >= 0 && ` (argument ${c.argIndex})`}
                            </div>
                          )}
                          {c.excludable && (
                            <div className="mt-1">
                              <Chip tone="ok">can be excluded via {c.excludable}</Chip>
                            </div>
                          )}
                        </li>
                      ))}
                    </ul>
                  )}
                  {!explain.confident && (
                    <p className="text-[11px] text-[color:var(--text-tertiary)]">
                      The console could not tie this event to a specific hook. The agent named the
                      policy, but the manifest it read does not contain a matching clause — most
                      often because the policy was edited after the event.
                    </p>
                  )}
                  {explain.manifest && (
                    <YamlBlock
                      yaml={yaml.dump(explain.manifest, { lineWidth: 100 })}
                      filename={`${explain.policy}.yaml`}
                      maxHeight={280}
                    />
                  )}
                </div>
              )}
            </section>
          ) : (
            <p className="text-[12px] text-[color:var(--text-tertiary)]">
              No policy was attributed to this record.{" "}
              {row.source === "cilium"
                ? "Cilium reports the deciding policy from 1.15 onwards; older agents only report the verdict."
                : "Events from Tetragon's base sensors are not produced by a TracingPolicy."}
            </p>
          )}

          <details>
            <summary className="cursor-pointer text-[12px] text-[color:var(--text-secondary)]">
              Raw record
            </summary>
            <pre className="mono mt-2 max-h-80 overflow-auto whitespace-pre-wrap rounded-lg p-3 text-[11px]" style={{ background: "var(--surface-2)" }}>
              {JSON.stringify(row.raw ?? row, null, 2)}
            </pre>
          </details>
        </div>
      )}
    </Sheet>
  );
}
