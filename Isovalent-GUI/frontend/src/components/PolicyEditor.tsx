"use client";

import { useCallback, useEffect, useState } from "react";
import yaml from "js-yaml";
import { apiDelete, apiGet, apiPost, apiPut } from "@/lib/api";
import type { DryRunResult, Policy } from "@/lib/types";
import { Badge } from "@/components/StatCard";
import { LabelsEditor, RulesEditor } from "@/components/RuleEditor";

type Manifest = {
  apiVersion?: string;
  kind?: string;
  metadata?: { name?: string; namespace?: string };
  spec?: Record<string, unknown>;
} & Record<string, unknown>;

const templates: Record<string, Manifest> = {
  CiliumNetworkPolicy: {
    apiVersion: "cilium.io/v2",
    kind: "CiliumNetworkPolicy",
    metadata: { name: "new-policy", namespace: "default" },
    spec: {
      endpointSelector: { matchLabels: { app: "my-app" } },
      ingress: [
        {
          fromEndpoints: [{ matchLabels: { app: "client" } }],
          toPorts: [{ ports: [{ port: "8080", protocol: "TCP" }] }],
        },
      ],
    },
  },
  CiliumClusterwideNetworkPolicy: {
    apiVersion: "cilium.io/v2",
    kind: "CiliumClusterwideNetworkPolicy",
    metadata: { name: "new-clusterwide-policy" },
    spec: {
      endpointSelector: { matchLabels: {} },
      egressDeny: [{ toEntities: ["world"] }],
    },
  },
  TracingPolicy: {
    apiVersion: "cilium.io/v1alpha1",
    kind: "TracingPolicy",
    metadata: { name: "new-tracing-policy" },
    spec: {
      kprobes: [
        {
          call: "security_file_permission",
          syscall: false,
          args: [
            { index: 0, type: "file" },
            { index: 1, type: "int" },
          ],
          selectors: [
            {
              matchArgs: [
                { index: 0, operator: "Prefix", values: ["/etc/shadow"] },
              ],
              matchActions: [{ action: "Sigkill" }],
            },
          ],
        },
      ],
    },
  },
};

function policyPath(m: Manifest): string {
  const kind = m.kind ?? "CiliumNetworkPolicy";
  const ns = m.metadata?.namespace || "-";
  return `/api/v1/policies/${kind}/${ns}/${m.metadata?.name}`;
}

export function PolicyEditor({ family = "all" }: { family?: "all" | "cilium" | "tetragon" }) {
  const [policies, setPolicies] = useState<Policy[]>([]);
  const [listError, setListError] = useState<string | null>(null);
  const [manifest, setManifest] = useState<Manifest | null>(null);
  const [yamlText, setYamlText] = useState("");
  const [parseError, setParseError] = useState<string | null>(null);
  const [status, setStatus] = useState<{ ok: boolean; msg: string } | null>(null);
  const [isNew, setIsNew] = useState(false);
  const [busy, setBusy] = useState(false);
  const [dryRun, setDryRun] = useState<DryRunResult | null>(null);
  const [applyMode, setApplyMode] = useState<"direct" | "pr">("direct");
  const [gitopsRepo, setGitopsRepo] = useState<string | null>(null);

  useEffect(() => {
    apiGet<{ enabled: boolean; repo: string }>("/api/v1/gitops/status")
      .then((s) => s.enabled && setGitopsRepo(s.repo))
      .catch(() => {});
  }, []);

  const refresh = useCallback(async () => {
    try {
      const [network, tracing] = await Promise.all([
        apiGet<Policy[]>("/api/v1/policies/network"),
        apiGet<Policy[]>("/api/v1/policies/tracing"),
      ]);
      setPolicies([...network, ...tracing]);
      setListError(null);
    } catch (e) {
      setListError(String(e));
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  /** Load a manifest object into both panes. */
  const load = (m: Manifest, fresh: boolean) => {
    setManifest(m);
    setYamlText(yaml.dump(m, { noRefs: true }));
    setParseError(null);
    setStatus(null);
    setIsNew(fresh);
  };

  /** YAML pane edited → parse and sync the object/form. */
  const onYamlChange = (text: string) => {
    setYamlText(text);
    try {
      const parsed = yaml.load(text);
      if (parsed && typeof parsed === "object") {
        setManifest(parsed as Manifest);
        setParseError(null);
      } else {
        setParseError("document must be a YAML mapping");
      }
    } catch (e) {
      setParseError((e as Error).message.split("\n")[0]);
    }
  };

  /** Form field edited → update object and re-dump YAML. */
  const patch = (fn: (m: Manifest) => void) => {
    if (!manifest) return;
    const next = structuredClone(manifest);
    fn(next);
    setManifest(next);
    setYamlText(yaml.dump(next, { noRefs: true }));
  };

  const apply = async () => {
    if (!manifest || parseError) return;
    setBusy(true);
    setStatus(null);
    try {
      const q = applyMode === "pr" ? "?mode=pr" : "";
      const res = await apiPut<{ pullRequest?: string }>(policyPath(manifest) + q, manifest);
      if (applyMode === "pr" && res.pullRequest) {
        setStatus({ ok: true, msg: `Opened PR: ${res.pullRequest}` });
      } else {
        setStatus({ ok: true, msg: `Applied ${manifest.kind}/${manifest.metadata?.name}` });
        setIsNew(false);
        refresh();
      }
    } catch (e) {
      setStatus({ ok: false, msg: String(e) });
    } finally {
      setBusy(false);
    }
  };

  const simulate = async () => {
    if (!manifest || parseError) return;
    if (!String(manifest.kind).includes("NetworkPolicy")) {
      setStatus({ ok: false, msg: "Dry-run applies to network policies only" });
      return;
    }
    setBusy(true);
    try {
      setDryRun(await apiPost<DryRunResult>("/api/v1/policies/dryrun?flows=500", manifest));
      setStatus(null);
    } catch (e) {
      setStatus({ ok: false, msg: String(e) });
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    if (!manifest || isNew) return;
    if (!confirm(`Delete ${manifest.kind}/${manifest.metadata?.name}?`)) return;
    setBusy(true);
    try {
      await apiDelete(policyPath(manifest));
      setStatus({ ok: true, msg: "Deleted" });
      setManifest(null);
      setYamlText("");
      refresh();
    } catch (e) {
      setStatus({ ok: false, msg: String(e) });
    } finally {
      setBusy(false);
    }
  };

  const matchLabels =
    (manifest?.spec?.endpointSelector as { matchLabels?: Record<string, string> } | undefined)
      ?.matchLabels ?? null;
  const isNetworkPolicy = String(manifest?.kind ?? "").includes("NetworkPolicy");

  const cilium = policies.filter((p) => p.kind.includes("NetworkPolicy"));
  const tetragon = policies.filter((p) => p.kind.includes("TracingPolicy"));
  const groups: [string, Policy[]][] =
    family === "cilium"
      ? [["Cilium network policies", cilium]]
      : family === "tetragon"
        ? [["Tetragon tracing policies", tetragon]]
        : [
            ["Cilium network policies", cilium],
            ["Tetragon tracing policies", tetragon],
          ];

  return (
    <div className="grid gap-4 lg:grid-cols-[280px_1fr]">
      {/* ------------ list pane ------------ */}
      <div className="panel flex max-h-[calc(100vh-11rem)] flex-col overflow-hidden">
        <div className="border-b border-neutral-800 p-3">
          <select
            className="w-full rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm"
            value=""
            onChange={(e) => {
              const t = templates[e.target.value];
              if (t) load(structuredClone(t), true);
            }}
          >
            <option value="" disabled>
              ＋ New policy…
            </option>
            <option value="CiliumNetworkPolicy">CiliumNetworkPolicy</option>
            <option value="CiliumClusterwideNetworkPolicy">
              CiliumClusterwideNetworkPolicy
            </option>
            <option value="TracingPolicy">TracingPolicy (Tetragon)</option>
          </select>
        </div>
        <div className="flex-1 overflow-y-auto">
          {listError && (
            <div className="p-3 text-xs text-red-400">{listError}</div>
          )}
          {groups.map(([label, items]) => (
            <div key={label}>
              <div className="px-3 pb-1 pt-3 text-[11px] uppercase tracking-wider text-neutral-500">
                {label}
              </div>
              {items.map((p) => {
                const selected =
                  manifest?.metadata?.name === p.name &&
                  (manifest?.metadata?.namespace ?? "") === (p.namespace ?? "");
                return (
                  <button
                    key={`${p.kind}/${p.namespace}/${p.name}`}
                    onClick={() => load(p.manifest as Manifest, false)}
                    className={`block w-full px-3 py-2 text-left text-sm hover:bg-neutral-800/60 ${
                      selected ? "bg-neutral-800" : ""
                    }`}
                  >
                    <div className="truncate">{p.name}</div>
                    <div className="text-[11px] text-neutral-500">
                      {p.namespace ? `ns: ${p.namespace}` : "cluster-wide"} ·{" "}
                      {p.kind.replace("Cilium", "").replace("Clusterwide", "CW ")}
                    </div>
                  </button>
                );
              })}
              {items.length === 0 && (
                <div className="px-3 py-2 text-xs text-neutral-600">none</div>
              )}
            </div>
          ))}
        </div>
      </div>

      {/* ------------ editor pane ------------ */}
      {!manifest ? (
        <div className="panel flex items-center justify-center text-sm text-neutral-500">
          Select a policy or create a new one.
        </div>
      ) : (
        <div className="space-y-4">
          <div className="panel space-y-3 p-4">
            <div className="flex items-center justify-between">
              <h2 className="text-sm font-medium text-neutral-300">
                {isNew ? "New policy" : "Edit policy"}{" "}
                <Badge tone="muted">{manifest.kind}</Badge>
              </h2>
              <div className="flex items-center gap-2">
                {String(manifest.kind).includes("NetworkPolicy") && (
                  <button
                    onClick={simulate}
                    disabled={busy || !!parseError}
                    className="rounded border border-neutral-700 bg-neutral-900 px-3 py-1.5 text-sm hover:bg-neutral-800 disabled:opacity-40"
                    title="Simulate against recent live flows"
                  >
                    ⁂ Dry-run
                  </button>
                )}
                {gitopsRepo && (
                  <select
                    value={applyMode}
                    onChange={(e) => setApplyMode(e.target.value as "direct" | "pr")}
                    className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-xs"
                    title="How to apply"
                  >
                    <option value="direct">apply: direct</option>
                    <option value="pr">apply: GitOps PR</option>
                  </select>
                )}
                <button
                  onClick={apply}
                  disabled={busy || !!parseError}
                  className="rounded bg-series-blue px-4 py-1.5 text-sm font-medium text-white hover:brightness-110 disabled:opacity-40"
                >
                  {busy ? "…" : applyMode === "pr" ? "Open PR" : "Apply to cluster"}
                </button>
                {!isNew && (
                  <button
                    onClick={remove}
                    disabled={busy}
                    className="rounded border border-red-900 bg-red-950 px-4 py-1.5 text-sm text-red-300 hover:bg-red-900/50"
                  >
                    Delete
                  </button>
                )}
              </div>
            </div>

            {dryRun && (
              <div className="rounded border border-neutral-800 bg-[color:var(--surface-0)] p-3">
                <div className="mb-2 flex items-center gap-3 text-xs">
                  <span className="font-medium text-neutral-300">Dry-run vs {dryRun.total} recent flows:</span>
                  <Badge tone="ok">{dryRun.allowed} allowed</Badge>
                  <Badge tone="crit">{dryRun.blocked} would be blocked</Badge>
                  <Badge tone="muted">{dryRun.total - dryRun.applied} unaffected</Badge>
                  <button onClick={() => setDryRun(null)} className="ml-auto text-neutral-500 hover:text-neutral-300">clear</button>
                </div>
                {dryRun.policyError && <div className="text-xs text-red-400">{dryRun.policyError}</div>}
                <div className="mono max-h-40 space-y-0.5 overflow-y-auto text-[11px]">
                  {dryRun.verdicts.filter((v) => v.applies && !v.allowed).slice(0, 12).map((v, i) => (
                    <div key={i} className="text-red-400">
                      ✕ {v.flow.source?.namespace}/{v.flow.source?.workload} → {v.flow.destination?.namespace}/{v.flow.destination?.workload}:{v.flow.l4?.dstPort} — {v.reason}
                    </div>
                  ))}
                  {dryRun.blocked === 0 && <div className="text-emerald-400">No live traffic would be blocked by this policy.</div>}
                </div>
              </div>
            )}

            <div className="grid gap-3 sm:grid-cols-2">
              <label className="block text-xs text-neutral-400">
                name
                <input
                  value={manifest.metadata?.name ?? ""}
                  onChange={(e) =>
                    patch((m) => {
                      m.metadata = { ...m.metadata, name: e.target.value };
                    })
                  }
                  className="mono mt-1 w-full rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm text-white"
                />
              </label>
              <label className="block text-xs text-neutral-400">
                namespace{" "}
                {manifest.kind !== "CiliumNetworkPolicy" && "(cluster-scoped)"}
                <input
                  value={manifest.metadata?.namespace ?? ""}
                  disabled={manifest.kind !== "CiliumNetworkPolicy"}
                  onChange={(e) =>
                    patch((m) => {
                      m.metadata = { ...m.metadata, namespace: e.target.value };
                    })
                  }
                  className="mono mt-1 w-full rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm text-white disabled:opacity-40"
                />
              </label>
            </div>

            {matchLabels && (
              <div className="space-y-1">
                <div className="text-xs font-medium uppercase tracking-wider text-neutral-400">
                  Selector — which pods this policy applies to
                </div>
                <LabelsEditor
                  labels={matchLabels}
                  onChange={(next) =>
                    patch((m) => {
                      (m.spec!.endpointSelector as { matchLabels: Record<string, string> }).matchLabels = next;
                    })
                  }
                />
              </div>
            )}

            {isNetworkPolicy && (
              <div className="grid gap-4 lg:grid-cols-2">
                <RulesEditor
                  direction="ingress"
                  rules={(manifest.spec?.ingress as never[]) ?? []}
                  onChange={(next) => patch((m) => { m.spec!.ingress = next; })}
                />
                <RulesEditor
                  direction="egress"
                  rules={(manifest.spec?.egress as never[]) ?? []}
                  onChange={(next) => patch((m) => { m.spec!.egress = next; })}
                />
              </div>
            )}

            {status && (
              <div
                className={`rounded px-3 py-2 text-xs ${
                  status.ok
                    ? "bg-emerald-950 text-emerald-300"
                    : "bg-red-950 text-red-300"
                }`}
              >
                {status.msg}
              </div>
            )}
          </div>

          <div className="panel">
            <div className="flex items-center justify-between border-b border-neutral-800 px-4 py-2">
              <span className="text-xs font-medium text-neutral-400">
                YAML — bi-directional with the form above
              </span>
              {parseError ? (
                <Badge tone="crit">YAML error: {parseError}</Badge>
              ) : (
                <Badge tone="ok">valid</Badge>
              )}
            </div>
            <textarea
              value={yamlText}
              onChange={(e) => onYamlChange(e.target.value)}
              spellCheck={false}
              className="mono h-[420px] w-full resize-y bg-[#141413] p-4 text-xs leading-relaxed text-neutral-200 outline-none"
            />
          </div>
        </div>
      )}
    </div>
  );
}
