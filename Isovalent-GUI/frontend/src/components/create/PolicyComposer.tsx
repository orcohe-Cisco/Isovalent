"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import yaml from "js-yaml";
import { apiGet, apiPost, apiPut } from "@/lib/api";
import type { DryRunResult, Policy, ServiceMapEdge, TracingDryRun } from "@/lib/types";
import { Chip } from "@/components/ui/Chip";
import { YamlBlock } from "@/components/ui/YamlBlock";
import { ErrorNote } from "@/components/ui/ErrorNote";
import { NamespacePicker } from "@/components/create/NamespacePicker";
import { TEMPLATES } from "@/components/create/templates";

interface Parsed {
  ok: boolean;
  error?: string;
  kind?: string;
  name?: string;
  namespace?: string;
  object?: Record<string, unknown>;
  isNetwork?: boolean;
}

const NETWORK_KINDS = ["CiliumNetworkPolicy", "CiliumClusterwideNetworkPolicy", "NetworkPolicy"];
const RUNTIME_KINDS = ["TracingPolicy", "TracingPolicyNamespaced"];

function parse(text: string): Parsed {
  if (!text.trim()) return { ok: false };
  let doc: unknown;
  try {
    doc = yaml.load(text);
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : "not valid YAML" };
  }
  if (!doc || typeof doc !== "object") return { ok: false, error: "not a Kubernetes object" };
  const o = doc as Record<string, unknown>;
  const kind = typeof o.kind === "string" ? o.kind : "";
  const meta = (o.metadata ?? {}) as Record<string, unknown>;
  const name = typeof meta.name === "string" ? meta.name : "";
  const namespace = typeof meta.namespace === "string" ? meta.namespace : "";
  if (!NETWORK_KINDS.includes(kind) && !RUNTIME_KINDS.includes(kind)) {
    return { ok: false, error: `unsupported kind "${kind || "(missing)"}"` };
  }
  if (!name) return { ok: false, error: "metadata.name is required" };
  return { ok: true, kind, name, namespace, object: o, isNetwork: NETWORK_KINDS.includes(kind) };
}

/**
 * Create Policy.
 *
 * One page for both halves of the product, because from the operator's side
 * "stop this pod talking to the metadata service" and "stop this pod running
 * nc" are the same task with two mechanisms. It loads what the cluster already
 * has — including native NetworkPolicies the console did not write — so
 * editing an existing rule is the same flow as writing a new one.
 *
 * Nothing applies without a dry run being available first. For a network
 * policy that means replaying recent flows; for a runtime policy it means
 * replaying stored events. Runtime enforcement has no undo.
 */
export function PolicyComposer() {
  const [text, setText] = useState(TEMPLATES[0].yaml);
  const [existing, setExisting] = useState<Policy[]>([]);
  const [listError, setListError] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [status, setStatus] = useState<{ ok: boolean; msg: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [netRun, setNetRun] = useState<DryRunResult | null>(null);
  const [runRun, setRunRun] = useState<TracingDryRun | null>(null);
  const [dryWindow, setDryWindow] = useState("1h");
  const [applyMode, setApplyMode] = useState<"live" | "pr">("live");
  const [gitops, setGitops] = useState<string | null>(null);
  const [filter, setFilter] = useState("");

  const parsed = useMemo(() => parse(text), [text]);

  const refresh = useCallback(async () => {
    try {
      const kinds = ["CiliumNetworkPolicy", "CiliumClusterwideNetworkPolicy", "NetworkPolicy", "TracingPolicy", "TracingPolicyNamespaced"];
      const results = await Promise.allSettled(kinds.map((k) => apiGet<Policy[]>(`/api/v1/policies/${k}`)));
      const all: Policy[] = [];
      const failures: string[] = [];
      results.forEach((r, i) => {
        if (r.status === "fulfilled") all.push(...r.value);
        else failures.push(kinds[i]);
      });
      setExisting(all);
      // A missing CRD is normal (no Tetragon installed, say) and is not an
      // error worth shouting about — but it should not be silent either.
      setListError(
        failures.length && failures.length === kinds.length
          ? "Could not read any policies from the cluster. Check Diagnostics."
          : failures.length
            ? `Not available in this cluster: ${failures.join(", ")}`
            : null,
      );
    } catch (e) {
      setListError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  useEffect(() => {
    void refresh();
    apiGet<{ enabled: boolean; repo: string }>("/api/v1/gitops/status")
      .then((s) => s.enabled && setGitops(s.repo))
      .catch(() => {});
  }, [refresh]);

  const load = (p: Policy) => {
    setText(yaml.dump(p.manifest, { lineWidth: 100 }));
    setNetRun(null);
    setRunRun(null);
    setStatus(null);
  };

  const setNamespace = (ns: string) => {
    if (!parsed.object) return;
    const next = structuredClone(parsed.object) as Record<string, unknown>;
    const meta = { ...((next.metadata ?? {}) as Record<string, unknown>) };
    if (ns) meta.namespace = ns;
    else delete meta.namespace;
    next.metadata = meta;
    // Cluster scope and namespace scope are different CRDs in both products;
    // switching one without the other produces a manifest the API rejects.
    if (next.kind === "CiliumNetworkPolicy" && !ns) next.kind = "CiliumClusterwideNetworkPolicy";
    if (next.kind === "CiliumClusterwideNetworkPolicy" && ns) next.kind = "CiliumNetworkPolicy";
    if (next.kind === "TracingPolicy" && ns) next.kind = "TracingPolicyNamespaced";
    if (next.kind === "TracingPolicyNamespaced" && !ns) next.kind = "TracingPolicy";
    setText(yaml.dump(next, { lineWidth: 100 }));
    setNetRun(null);
    setRunRun(null);
  };

  const dryRun = useCallback(async () => {
    if (!parsed.ok || !parsed.object) return;
    setBusy(true);
    setError(null);
    setNetRun(null);
    setRunRun(null);
    try {
      if (parsed.isNetwork) {
        setNetRun(await apiPost<DryRunResult>("/api/v1/policies/dryrun?flows=500", parsed.object));
      } else {
        setRunRun(
          await apiPost<TracingDryRun>(`/api/v1/tracingpolicies/dryrun?window=${dryWindow}`, parsed.object),
        );
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }, [parsed, dryWindow]);

  const apply = useCallback(async () => {
    if (!parsed.ok || !parsed.object || !parsed.kind || !parsed.name) return;
    setBusy(true);
    setStatus(null);
    setError(null);
    try {
      const ns = parsed.namespace || "-";
      const q = applyMode === "pr" ? "?mode=pr" : "";
      const res = await apiPut<{ pullRequest?: string }>(
        `/api/v1/policies/${parsed.kind}/${encodeURIComponent(ns)}/${encodeURIComponent(parsed.name)}${q}`,
        parsed.object,
      );
      setStatus({
        ok: true,
        msg: res?.pullRequest
          ? `Pull request opened: ${res.pullRequest}`
          : `Applied ${parsed.kind}/${parsed.name}`,
      });
      void refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }, [parsed, applyMode, refresh]);

  const generate = useCallback(async () => {
    // Build an allow-list from what the cluster is actually doing. Starting a
    // policy from observed traffic is the difference between a rule you can
    // apply and a rule you have to debug.
    setBusy(true);
    setError(null);
    try {
      const map = await apiGet<{ edges: ServiceMapEdge[] }>("/api/v1/servicemap");
      const ns = parsed.namespace || "default";
      const target = prompt(`Generate an egress allow-list for which workload in "${ns}"?`);
      if (!target) return;
      const id = `${ns}/${target}`;
      const out = map.edges.filter((e) => e.source === id && e.forwarded > 0);
      if (!out.length) {
        setError(`No forwarded traffic observed from ${id}. Nothing to base a policy on yet.`);
        return;
      }
      const egress = out.map((e) => {
        const [peerNs, peerWorkload] = e.target.includes("/") ? e.target.split("/") : ["", e.target];
        return {
          toEndpoints: peerNs
            ? [{ matchLabels: { "k8s:io.kubernetes.pod.namespace": peerNs, "k8s:app": peerWorkload } }]
            : [{ matchLabels: {} }],
          toPorts: [
            {
              ports: e.ports.filter((p) => p > 0).map((p) => ({ port: String(p), protocol: "TCP" })),
            },
          ],
        };
      });
      setText(
        yaml.dump(
          {
            apiVersion: "cilium.io/v2",
            kind: "CiliumNetworkPolicy",
            metadata: { name: `${target}-egress`, namespace: ns },
            spec: { endpointSelector: { matchLabels: { app: target } }, egress },
          },
          { lineWidth: 100 },
        ),
      );
      setStatus({
        ok: true,
        msg: `Generated from ${out.length} observed destination(s). Review the labels — the generator assumes app=<workload>.`,
      });
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }, [parsed.namespace]);

  const visible = existing.filter((p) => {
    const q = filter.trim().toLowerCase();
    return !q || `${p.namespace ?? ""}/${p.name} ${p.kind}`.toLowerCase().includes(q);
  });

  return (
    <div className="grid gap-4 xl:grid-cols-[18rem_minmax(0,1fr)]">
      {/* --- existing policies -------------------------------------- */}
      <aside className="space-y-2">
        <div className="panel px-3 py-2.5">
          <div className="flex items-center justify-between">
            <div className="section-label">In this cluster</div>
            <button className="btn btn-ghost !px-1.5 !py-0.5 !text-[11px]" onClick={() => void refresh()}>
              refresh
            </button>
          </div>
          <input
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter…"
            className="field mt-2 !py-1.5 !text-[12px]"
          />
          {listError && (
            <p className="mt-2 text-[11px] leading-relaxed text-[color:var(--text-tertiary)]">
              {listError}
            </p>
          )}
          <ul className="mt-2 max-h-[60vh] space-y-0.5 overflow-y-auto">
            {visible.map((p) => (
              <li key={`${p.kind}:${p.namespace ?? "-"}:${p.name}`}>
                <button
                  onClick={() => load(p)}
                  className="w-full rounded-md px-2 py-1.5 text-left hover:bg-[color:var(--surface-2)]"
                >
                  <div className="mono truncate text-[12px]">{p.name}</div>
                  <div className="truncate text-[10px] text-[color:var(--text-tertiary)]">
                    {p.kind} · {p.namespace || "cluster-wide"}
                  </div>
                </button>
              </li>
            ))}
            {visible.length === 0 && (
              <li className="px-2 py-2 text-[11px] text-[color:var(--text-tertiary)]">
                No policies yet.
              </li>
            )}
          </ul>
        </div>

        <div className="panel px-3 py-2.5">
          <div className="section-label mb-1.5">Start from</div>
          <ul className="space-y-1">
            {TEMPLATES.map((t) => (
              <li key={t.id}>
                <button
                  onClick={() => {
                    setText(t.yaml);
                    setNetRun(null);
                    setRunRun(null);
                    setStatus(null);
                  }}
                  className="w-full rounded-md px-2 py-1.5 text-left hover:bg-[color:var(--surface-2)]"
                >
                  <div className="text-[12px] font-medium">{t.name}</div>
                  <div className="text-[10px] leading-snug text-[color:var(--text-tertiary)]">
                    {t.blurb}
                  </div>
                </button>
              </li>
            ))}
          </ul>
          <button className="btn btn-secondary mt-2 w-full !py-1.5 !text-[12px]" onClick={generate} disabled={busy}>
            Generate from observed traffic
          </button>
        </div>
      </aside>

      {/* --- editor -------------------------------------------------- */}
      <section className="space-y-4">
        <ErrorNote error={error} />

        <div className="panel p-4">
          <div className="mb-2.5 flex flex-wrap items-center gap-2">
            {parsed.ok ? (
              <>
                <Chip tone="ok">valid</Chip>
                <Chip mono>{parsed.kind}</Chip>
                <Chip mono>{parsed.name}</Chip>
                <NamespacePicker
                  value={parsed.namespace ?? ""}
                  onChange={setNamespace}
                  allowCluster={parsed.kind !== "NetworkPolicy"}
                />
                <Chip tone={parsed.isNetwork ? "accent" : "violet"}>
                  {parsed.isNetwork ? "network" : "runtime"}
                </Chip>
              </>
            ) : parsed.error ? (
              <Chip tone="danger">{parsed.error}</Chip>
            ) : (
              <span className="text-[11px] text-[color:var(--text-tertiary)]">nothing to apply</span>
            )}
          </div>

          <textarea
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              setStatus(null);
              setNetRun(null);
              setRunRun(null);
            }}
            spellCheck={false}
            className="mono h-[46vh] w-full resize-y rounded-lg p-3 text-[11.5px] leading-relaxed outline-none"
            style={{ background: "var(--surface-0)", border: "1px solid var(--hairline)", color: "#d8d7cf" }}
          />

          <div className="mt-3 flex flex-wrap items-center gap-2">
            <button className="btn btn-secondary" onClick={dryRun} disabled={!parsed.ok || busy}>
              {busy ? "Working…" : "Dry run"}
            </button>
            {!parsed.isNetwork && (
              <select
                className="field !w-auto !py-1.5 !text-[12px]"
                value={dryWindow}
                onChange={(e) => setDryWindow(e.target.value)}
                aria-label="Dry-run window"
              >
                {["15m", "1h", "6h", "1d", "7d"].map((w) => (
                  <option key={w} value={w}>
                    replay last {w}
                  </option>
                ))}
              </select>
            )}
            {gitops && (
              <select
                className="field !w-auto !py-1.5 !text-[12px]"
                value={applyMode}
                onChange={(e) => setApplyMode(e.target.value as "live" | "pr")}
              >
                <option value="live">Apply directly</option>
                <option value="pr">Open a pull request ({gitops})</option>
              </select>
            )}
            <button
              className="btn btn-primary"
              onClick={apply}
              disabled={!parsed.ok || busy || (!netRun && !runRun)}
              title={!netRun && !runRun ? "Dry run first" : undefined}
            >
              {applyMode === "pr" ? "Open pull request" : "Apply to cluster"}
            </button>
            {!netRun && !runRun && parsed.ok && (
              <span className="text-[11px] text-[color:var(--text-tertiary)]">
                Dry run first — enforcement has no undo.
              </span>
            )}
          </div>

          {status && (
            <div
              className="mt-3 rounded-lg px-3 py-2 text-[12px]"
              style={
                status.ok
                  ? { background: "rgba(25,158,112,0.12)", color: "#6dd3ab" }
                  : { background: "rgba(230,103,103,0.12)", color: "#f0a3a3" }
              }
            >
              {status.msg}
            </div>
          )}
        </div>

        {netRun && <NetworkDryRun result={netRun} />}
        {runRun && <RuntimeDryRun result={runRun} />}

        {parsed.ok && <YamlBlock yaml={text.trim()} filename={`${parsed.name}.yaml`} maxHeight={280} />}
      </section>
    </div>
  );
}

function NetworkDryRun({ result }: { result: DryRunResult }) {
  if (result.policyError) {
    return (
      <div className="panel px-4 py-3 text-[12px]" style={{ color: "#f0a3a3" }}>
        {result.policyError}
      </div>
    );
  }
  const blocked = result.verdicts.filter((v) => v.applies && !v.allowed).slice(0, 12);
  return (
    <section className="panel px-4 py-3">
      <div className="section-label mb-2">Dry run · replayed {result.total} recent flows</div>
      <div className="flex flex-wrap gap-2">
        <Chip mono>{result.applied} selected by this policy</Chip>
        <Chip tone="ok" mono>{result.allowed} still allowed</Chip>
        <Chip tone={result.blocked > 0 ? "danger" : "neutral"} mono>
          {result.blocked} would be blocked
        </Chip>
      </div>
      {result.applied === 0 && (
        <p className="mt-2 text-[12px] leading-relaxed text-[color:var(--text-secondary)]">
          This policy did not select any recent flow. Either the workload is idle, or the
          endpointSelector does not match what you think it does — which looks identical from here
          until you check the labels.
        </p>
      )}
      {blocked.length > 0 && (
        <>
          <div className="section-label mb-1 mt-3">Traffic this would break</div>
          <ul className="space-y-0.5">
            {blocked.map((v, i) => (
              <li key={i} className="mono text-[11px] text-[color:var(--text-secondary)]">
                {v.flow.source.namespace}/{v.flow.source.workload} → {v.flow.destination.namespace}/
                {v.flow.destination.workload}:{v.flow.l4.dstPort} — {v.reason}
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  );
}

function RuntimeDryRun({ result }: { result: TracingDryRun }) {
  if (result.policyError) {
    return (
      <div className="panel px-4 py-3 text-[12px]" style={{ color: "#f0a3a3" }}>
        {result.policyError}
      </div>
    );
  }
  return (
    <section className="panel px-4 py-3">
      <div className="section-label mb-2">Dry run · replayed {result.total} stored events</div>
      <div className="flex flex-wrap gap-2">
        <Chip mono>{result.matched} would have matched</Chip>
        <Chip tone={result.enforcing ? "danger" : "accent"}>
          {result.enforcing ? "enforcing — would kill" : "monitor only"}
        </Chip>
        {result.enforcing && <Chip tone="danger" mono>{result.wouldEnforce} processes killed</Chip>}
      </div>
      {result.advice && (
        <p className="mt-2 text-[12px] leading-relaxed text-[color:var(--text-secondary)]">
          {result.advice}
        </p>
      )}
      {result.byBinary.length > 0 && (
        <div className="mt-3 grid gap-3 sm:grid-cols-2">
          <Breakdown title="By process" rows={result.byBinary} />
          <Breakdown title="By namespace" rows={result.byNamespace} />
        </div>
      )}
      {result.samples.length > 0 && (
        <details className="mt-3">
          <summary className="cursor-pointer text-[12px] text-[color:var(--text-secondary)]">
            {result.samples.length} sample match{result.samples.length === 1 ? "" : "es"}
          </summary>
          <ul className="mt-2 space-y-1">
            {result.samples.map((s, i) => (
              <li key={i} className="mono text-[11px] text-[color:var(--text-secondary)]">
                {s.event.binary} {s.event.args} — {s.explain.summary}
              </li>
            ))}
          </ul>
        </details>
      )}
    </section>
  );
}

function Breakdown({ title, rows }: { title: string; rows: { value: string; count: number }[] }) {
  const max = Math.max(...rows.map((r) => r.count), 1);
  return (
    <div>
      <div className="section-label mb-1">{title}</div>
      <ul className="space-y-0.5">
        {rows.slice(0, 6).map((r) => (
          <li key={r.value} className="relative overflow-hidden rounded px-1.5 py-0.5">
            <span
              aria-hidden
              className="absolute inset-y-0 left-0"
              style={{ width: `${(r.count / max) * 100}%`, background: "rgba(255,255,255,0.05)" }}
            />
            <span className="mono relative z-10 flex justify-between gap-2 text-[11px]">
              <span className="truncate">{r.value}</span>
              <span className="text-[color:var(--text-tertiary)]">{r.count}</span>
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}
