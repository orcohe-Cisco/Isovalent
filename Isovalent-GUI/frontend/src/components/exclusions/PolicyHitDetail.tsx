"use client";

import { useCallback, useMemo, useState } from "react";
import yaml from "js-yaml";
import { apiDelete, apiPost } from "@/lib/api";
import { sinceLabel } from "@/lib/observed";
import type {
  ExclusionNote,
  ExclusionRequest,
  HitDetailResponse,
  HitValue,
} from "@/lib/types";
import { Chip } from "@/components/ui/Chip";
import { YamlBlock } from "@/components/ui/YamlBlock";
import { ErrorNote } from "@/components/ui/ErrorNote";
import { EmptyState } from "@/components/ui/EmptyState";

/** Which hit dimension maps onto which field of the exclusion request. */
const DIMENSION_FIELD: Record<string, keyof ExclusionRequest> = {
  binaries: "binaries",
  parentBinaries: "parentBinaries",
  paths: "paths",
  podLabels: "podLabels",
  containers: "containers",
};

const DIMENSION_LABEL: Record<string, string> = {
  binaries: "Process",
  parentBinaries: "Parent process",
  paths: "File path",
  users: "User",
  podLabels: "Pod label",
  containers: "Container",
  namespaces: "Namespace",
  workloads: "Workload",
  pods: "Pod",
  nodes: "Node",
  hooks: "Hook",
};

/** How Tetragon expresses the exclusion, shown so nobody has to guess. */
const MECHANISM: Record<string, string> = {
  binaries: "matchBinaries · NotIn — filtered in the kernel, so the event is never generated",
  parentBinaries: "matchParentBinaries · NotIn — exempts everything this launcher spawns",
  paths: "matchArgs · NotPrefix on the argument index the value came from",
  podLabels: "podSelector · matchExpressions NotIn — the pod is not selected at all",
  containers: "containerSelector · matchExpressions NotIn — exempts a sidecar by name",
};

const NOT_EXCLUDABLE_REASON: Record<string, string> = {
  users:
    "Tetragon has no user-based selector. Exclude the process or the pod label instead — or scope the policy to a namespace.",
  namespaces:
    "Tetragon scopes namespaces by including them (TracingPolicyNamespaced), not by excluding them.",
  workloads: "Workloads are selected through pod labels; use the Pod label column.",
  pods: "A pod name never matches a second pod. Use its labels.",
  nodes: "Node selection is an agent-level concern, not a policy one.",
  hooks: "Removing a hook is an edit to the policy, not an exclusion.",
};

export function PolicyHitDetail({
  data,
  onApplied,
  onReset,
}: {
  data: HitDetailResponse;
  onApplied: () => void;
  onReset: () => void;
}) {
  const { detail, manifest, info, explain, policyError, present } = data;
  const gone = present === false;
  const [selected, setSelected] = useState<Record<string, Set<string>>>({});
  const [preview, setPreview] = useState<{ notes: ExclusionNote[]; diff: string[]; manifest: unknown } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [applied, setApplied] = useState<ExclusionNote[] | null>(null);
  const [showYaml, setShowYaml] = useState(false);

  const toggle = (dim: string, value: string) => {
    setPreview(null);
    setApplied(null);
    setSelected((prev) => {
      const next = { ...prev };
      const set = new Set(next[dim] ?? []);
      if (set.has(value)) set.delete(value);
      else set.add(value);
      next[dim] = set;
      return next;
    });
  };

  const request = useMemo<ExclusionRequest>(() => {
    const req: ExclusionRequest = {};
    for (const [dim, field] of Object.entries(DIMENSION_FIELD)) {
      const values = Array.from(selected[dim] ?? []);
      if (values.length) (req[field] as string[]) = values;
    }
    // A NotPrefix is written against an argument index. Take it from the hit
    // itself rather than guessing: the wrong index matches nothing, silently.
    const paths = Array.from(selected.paths ?? []);
    if (paths.length) {
      const first = (detail.values.paths ?? []).find((v) => paths.includes(v.value));
      if (first && first.argIndex >= 0) req.pathArgIndex = first.argIndex;
    }
    return req;
  }, [selected, detail.values.paths]);

  const count = Object.values(selected).reduce((n, s) => n + s.size, 0);
  const path = `/api/v1/hits/${encodeURIComponent(detail.namespace || "-")}/${encodeURIComponent(detail.policy)}/exclusions`;

  const doPreview = useCallback(async () => {
    setBusy(true);
    setError(null);
    try {
      const res = await apiPost<{ notes: ExclusionNote[]; diff: string[]; manifest: unknown }>(
        `${path}?dryRun=true`,
        request,
      );
      setPreview(res);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }, [path, request]);

  const doApply = useCallback(async () => {
    setBusy(true);
    setError(null);
    try {
      const res = await apiPost<{ notes: ExclusionNote[] }>(path, request);
      setApplied(res.notes ?? []);
      setPreview(null);
      setSelected({});
      onApplied();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }, [path, request, onApplied]);

  const doReset = useCallback(async () => {
    setBusy(true);
    try {
      await apiDelete(`/api/v1/hits/${encodeURIComponent(detail.namespace || "-")}/${encodeURIComponent(detail.policy)}`);
      onReset();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }, [detail.namespace, detail.policy, onReset]);

  const dimensions = Object.keys(DIMENSION_LABEL).filter(
    (d) => (detail.values[d] ?? []).length > 0,
  );

  return (
    <div className="space-y-5">
      <ErrorNote error={error ?? policyError} />

      {gone && (
        <div className="panel px-4 py-3" style={{ borderColor: "var(--hairline-strong)" }}>
          <div className="section-label mb-1">No longer in the cluster</div>
          <p className="text-[12px] leading-relaxed text-[color:var(--text-secondary)]">
            This policy has been deleted since it last fired — Tetragon&rsquo;s event history outlives
            the policy that produced it. Everything below is that history; reapply the policy to write
            exclusions back into it.
          </p>
        </div>
      )}

      <div className="flex flex-wrap items-center gap-2">
        <Chip tone={info?.action === "enforce" ? "danger" : "accent"}>
          {info?.action === "enforce" ? "enforcing · kills" : "monitor · observes"}
        </Chip>
        <Chip mono>{detail.total.toLocaleString()} hits</Chip>
        {detail.enforced > 0 && <Chip tone="danger" mono>{detail.enforced.toLocaleString()} enforced</Chip>}
        <Chip mono title="Hits per minute over the tracked window">
          {detail.ratePerMin.toFixed(1)}/min
        </Chip>
        {detail.lastSeen && <Chip>last {sinceLabel(detail.lastSeen)}</Chip>}
        {manifest && (
          <button className="btn btn-ghost !py-1 !text-[12px]" onClick={() => setShowYaml((v) => !v)}>
            {showYaml ? "Hide policy" : "Show policy"}
          </button>
        )}
        <button className="btn btn-ghost !py-1 !text-[12px]" onClick={doReset} disabled={busy}>
          Reset counters
        </button>
      </div>

      {explain?.summary && (
        <div className="panel px-4 py-3">
          <div className="section-label mb-1">Why the most recent event matched</div>
          <p className="mono text-[12px] leading-relaxed text-[color:var(--text-secondary)]">
            {explain.summary}
          </p>
        </div>
      )}

      {showYaml && manifest && (
        <YamlBlock yaml={yaml.dump(manifest, { lineWidth: 100 })} filename={`${detail.policy}.yaml`} />
      )}

      {detail.total === 0 ? (
        <EmptyState
          title="This policy has not fired"
          body={
            <p>
              Either the behaviour it looks for has not happened, or its selector does not match
              what you think it does. Check the hook and pod selector before assuming the former —
              a policy that matches nothing looks exactly like a quiet cluster.
            </p>
          }
        />
      ) : (
        <div className="grid gap-4 lg:grid-cols-2">
          {dimensions.map((dim) => (
            <DimensionCard
              key={dim}
              dim={dim}
              values={detail.values[dim] ?? []}
              total={detail.total}
              selected={selected[dim] ?? new Set()}
              onToggle={(v) => toggle(dim, v)}
              canExclude={!gone}
            />
          ))}
        </div>
      )}

      {applied && (
        <div className="panel px-4 py-3" style={{ borderColor: "rgba(25,158,112,0.35)" }}>
          <div className="section-label mb-1.5" style={{ color: "#6dd3ab" }}>
            Applied
          </div>
          <ul className="space-y-1 text-[12px]">
            {applied.map((n, i) => (
              <li key={i} className={n.applied ? "" : "text-[color:var(--text-tertiary)]"}>
                {n.applied ? "✓" : "—"} {DIMENSION_LABEL[n.dimension] ?? n.dimension}: {n.detail}
              </li>
            ))}
          </ul>
          <p className="mt-2 text-[11px] text-[color:var(--text-tertiary)]">
            Counters were cleared. If the exclusion worked, this policy stays quiet.
          </p>
        </div>
      )}

      {count > 0 && (
        <div
          className="sticky bottom-0 -mx-1 rounded-xl px-4 py-3"
          style={{
            background: "var(--surface-2)",
            border: "1px solid var(--hairline-strong)",
            boxShadow: "var(--shadow-2)",
          }}
        >
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="min-w-0">
              <div className="text-[13px] font-medium">
                {count} exclusion{count === 1 ? "" : "s"} selected
              </div>
              <div className="mt-0.5 truncate text-[11px] text-[color:var(--text-tertiary)]">
                {Object.entries(selected)
                  .filter(([, s]) => s.size)
                  .map(([dim, s]) => `${DIMENSION_LABEL[dim] ?? dim}: ${Array.from(s).join(", ")}`)
                  .join(" · ")}
              </div>
            </div>
            <div className="flex gap-2">
              <button className="btn btn-secondary" onClick={doPreview} disabled={busy}>
                {busy ? "Working…" : "Preview change"}
              </button>
              <button className="btn btn-primary" onClick={doApply} disabled={busy || !preview}>
                Apply to policy
              </button>
            </div>
          </div>
          {!preview && (
            <p className="mt-2 text-[11px] text-[color:var(--text-tertiary)]">
              Preview first — it shows exactly which selectors the exclusion reaches, and which it
              cannot.
            </p>
          )}
          {preview && (
            <div className="mt-3 space-y-2">
              <ul className="space-y-1 text-[12px]">
                {preview.notes.map((n, i) => (
                  <li key={i} className={n.applied ? "" : "text-[color:var(--text-tertiary)]"}>
                    {n.applied ? "✓" : "⚠"} {DIMENSION_LABEL[n.dimension] ?? n.dimension}: {n.detail}
                  </li>
                ))}
              </ul>
              {preview.diff?.length > 0 && (
                <details>
                  <summary className="cursor-pointer text-[12px] text-[color:var(--text-secondary)]">
                    {preview.diff.length} field change{preview.diff.length === 1 ? "" : "s"}
                  </summary>
                  <pre className="mono mt-2 max-h-48 overflow-auto whitespace-pre-wrap text-[11px] text-[color:var(--text-tertiary)]">
                    {preview.diff.join("\n")}
                  </pre>
                </details>
              )}
            </div>
          )}
        </div>
      )}

      {detail.samples.length > 0 && (
        <div className="panel overflow-hidden">
          <div className="section-label px-4 pb-2 pt-3">Recent matches</div>
          <ul className="max-h-72 overflow-y-auto">
            {detail.samples.slice(0, 20).map((s, i) => (
              <li key={i} className="hairline-t px-4 py-2 text-[12px]">
                <div className="flex flex-wrap items-baseline gap-2">
                  <span className="mono text-[color:var(--text-tertiary)]">
                    {new Date(s.time).toLocaleTimeString()}
                  </span>
                  {s.action && s.action !== "POST" && <Chip tone="danger">{s.action}</Chip>}
                  <span className="mono">{s.binary}</span>
                  <span className="mono text-[color:var(--text-secondary)]">{s.args}</span>
                </div>
                <div className="mt-0.5 text-[11px] text-[color:var(--text-tertiary)]">
                  {s.namespace}/{s.workload} · {s.function}
                  {s.details ? ` · ${s.details}` : ""}
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function DimensionCard({
  dim,
  values,
  total,
  selected,
  onToggle,
  canExclude,
}: {
  dim: string;
  values: HitValue[];
  total: number;
  selected: Set<string>;
  onToggle: (value: string) => void;
  canExclude: boolean;
}) {
  const [expanded, setExpanded] = useState(false);
  const excludable = canExclude && Boolean(DIMENSION_FIELD[dim]);
  const shown = expanded ? values : values.slice(0, 6);

  return (
    <section className="panel px-4 py-3">
      <div className="flex items-center gap-2">
        <h3 className="text-[13px] font-medium">{DIMENSION_LABEL[dim] ?? dim}</h3>
        <Chip tone={excludable ? "ok" : "neutral"}>
          {excludable ? "excludable" : "diagnostic only"}
        </Chip>
        <span className="ml-auto text-[11px] text-[color:var(--text-tertiary)]">
          {values.length} distinct
        </span>
      </div>
      <p className="mt-1 text-[11px] leading-relaxed text-[color:var(--text-tertiary)]">
        {excludable
          ? MECHANISM[dim]
          : (NOT_EXCLUDABLE_REASON[dim] ??
             (!canExclude ? "This policy no longer exists in the cluster — nothing to write the exclusion into." : undefined))}
      </p>
      <ul className="mt-2.5 space-y-1">
        {shown.map((v) => {
          const pct = total > 0 ? Math.round((v.count / total) * 100) : 0;
          const on = selected.has(v.value);
          return (
            <li key={v.value}>
              <button
                disabled={!excludable}
                onClick={() => onToggle(v.value)}
                className="relative flex w-full items-center gap-2 overflow-hidden rounded-lg px-2 py-1.5 text-left disabled:cursor-default"
                style={{
                  background: on ? "var(--accent-soft)" : "transparent",
                  border: `1px solid ${on ? "rgba(57,135,229,0.45)" : "transparent"}`,
                }}
                title={excludable ? (on ? "Selected for exclusion" : "Select to exclude") : undefined}
              >
                <span
                  aria-hidden
                  className="absolute inset-y-0 left-0"
                  style={{ width: `${pct}%`, background: "rgba(255,255,255,0.045)" }}
                />
                {excludable && (
                  <span
                    aria-hidden
                    className="relative z-10 grid h-3.5 w-3.5 shrink-0 place-items-center rounded-[4px] text-[9px]"
                    style={{
                      border: `1px solid ${on ? "var(--accent)" : "var(--hairline-strong)"}`,
                      background: on ? "var(--accent)" : "transparent",
                      color: "#fff",
                    }}
                  >
                    {on ? "✓" : ""}
                  </span>
                )}
                <span className="mono relative z-10 min-w-0 flex-1 truncate text-[12px]">
                  {v.value}
                </span>
                {v.enforced > 0 && (
                  <span className="relative z-10 shrink-0">
                    <Chip tone="danger">{v.enforced} killed</Chip>
                  </span>
                )}
                <span className="mono relative z-10 shrink-0 text-[11px] text-[color:var(--text-secondary)]">
                  {v.count.toLocaleString()}
                </span>
                <span className="relative z-10 w-8 shrink-0 text-right text-[10px] text-[color:var(--text-tertiary)]">
                  {pct}%
                </span>
              </button>
            </li>
          );
        })}
      </ul>
      {values.length > 6 && (
        <button
          className="btn btn-ghost mt-1.5 !px-2 !py-1 !text-[11px]"
          onClick={() => setExpanded((v) => !v)}
        >
          {expanded ? "Show less" : `Show all ${values.length}`}
        </button>
      )}
    </section>
  );
}
