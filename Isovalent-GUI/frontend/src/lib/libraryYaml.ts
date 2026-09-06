/**
 * Turns a selection from the policy library into applyable Tetragon manifests.
 *
 * Two rules govern everything here:
 *
 *  1. The upstream spec is the source of truth. We clone it and apply only the
 *     transformations the user asked for — mode and exclusions. We never
 *     rewrite a hook or invent a selector.
 *
 *  2. Every exclusion maps onto a documented Tetragon mechanism. If Tetragon
 *     cannot express an exclusion in the policy, it does not appear as a
 *     policy-level option — it goes to the agent's export denylist instead,
 *     and the UI says which is which. An exclusion that silently does nothing
 *     is worse than no exclusion.
 */

import yaml from "js-yaml";
import { POLICIES_BY_ID, type LibraryPolicy, type PolicyMode } from "@/lib/policyLibrary";

/* ---------------------------------------------------------- exclusions --- */

export interface Exclusions {
  /** "key=value" — pods carrying these are not selected. */
  podLabels: string[];
  /** Binary paths the hooks should ignore. */
  binaries: string[];
  /** Parent binary paths — exempts a whole subtree of a known-good launcher. */
  parentBinaries: string[];
  /** File/string argument prefixes to ignore, e.g. /var/log/. */
  paths: string[];
  /** Container names (containerSelector). */
  containers: string[];
  /**
   * Kubernetes namespaces. NOT expressible inside a cluster-wide TracingPolicy —
   * emitted as an agent export denylist instead. See EXCLUSION_DIMENSIONS.
   */
  namespaces: string[];
}

export const EMPTY_EXCLUSIONS: Exclusions = {
  podLabels: [],
  binaries: [],
  parentBinaries: [],
  paths: [],
  containers: [],
  namespaces: [],
};

export type ExclusionDimension = keyof Exclusions;

export const EXCLUSION_DIMENSIONS: {
  id: ExclusionDimension;
  label: string;
  where: "policy" | "agent";
  mechanism: string;
  hint: string;
}[] = [
  {
    id: "binaries",
    label: "Binaries",
    where: "policy",
    mechanism: "matchBinaries · NotIn",
    hint: "The single most useful exclusion. Filtered in the kernel, so the event is never generated — this is how you stop a file policy firing on your log shipper.",
  },
  {
    id: "parentBinaries",
    label: "Parent binaries",
    where: "policy",
    mechanism: "matchParentBinaries · NotIn",
    hint: "Exempts everything launched by a known-good process, rather than listing each child. Useful for package managers and init systems.",
  },
  {
    id: "paths",
    label: "File paths",
    where: "policy",
    mechanism: "matchArgs · NotPrefix",
    hint: "Only applies to hooks that take a file or string argument, and only where the policy has not already constrained that argument. The UI shows which policies it reached.",
  },
  {
    id: "podLabels",
    label: "Pod labels",
    where: "policy",
    mechanism: "podSelector · matchExpressions NotIn",
    hint: "Pods carrying these labels are not selected at all. In-kernel, and applies to enforcement as well as events.",
  },
  {
    id: "containers",
    label: "Container names",
    where: "policy",
    mechanism: "containerSelector · matchExpressions NotIn",
    hint: "Exempts a sidecar by name across every pod — the istio-proxy case.",
  },
  {
    id: "namespaces",
    label: "Namespaces",
    where: "agent",
    mechanism: "agent export denylist",
    hint: "Tetragon scopes namespaces by including them (TracingPolicyNamespaced), not by excluding them. Namespace exclusions are therefore emitted as an agent export denylist, which drops the events — it does not disable enforcement. Use the scope selector above to include instead.",
  },
];

export function mergeExclusions(a: Exclusions, b?: Exclusions): Exclusions {
  if (!b) return a;
  const u = (x: string[], y: string[]) => [...new Set([...x, ...y])].sort();
  return {
    podLabels: u(a.podLabels, b.podLabels),
    binaries: u(a.binaries, b.binaries),
    parentBinaries: u(a.parentBinaries, b.parentBinaries),
    paths: u(a.paths, b.paths),
    containers: u(a.containers, b.containers),
    namespaces: u(a.namespaces, b.namespaces),
  };
}

export function countExclusions(e?: Exclusions): number {
  if (!e) return 0;
  return (
    e.podLabels.length +
    e.binaries.length +
    e.parentBinaries.length +
    e.paths.length +
    e.containers.length +
    e.namespaces.length
  );
}

/* --------------------------------------------------------------- draft --- */

export interface LibraryDraft {
  name: string;
  description: string;
  /** Empty = cluster-wide TracingPolicy. Otherwise one TracingPolicyNamespaced per namespace. */
  scopeNamespaces: string[];
  /** hostSelector: {} when true, omitted when false. */
  includeHost: boolean;
  /** nodeSelector matchLabels, as "key=value". */
  nodeLabels: string[];
  selection: Record<string, PolicyMode>;
  exclusions: Exclusions;
  policyExclusions: Record<string, Exclusions>;
}

export const emptyDraft: LibraryDraft = {
  name: "Runtime security",
  description: "",
  scopeNamespaces: [],
  includeHost: false,
  nodeLabels: [],
  selection: {},
  exclusions: EMPTY_EXCLUSIONS,
  policyExclusions: {},
};

export function selectedPolicies(draft: LibraryDraft): LibraryPolicy[] {
  return Object.keys(draft.selection)
    .map((id) => POLICIES_BY_ID[id])
    .filter(Boolean)
    .sort((a, b) => a.name.localeCompare(b.name));
}

export function effectiveExclusions(draft: LibraryDraft, id: string): Exclusions {
  return mergeExclusions(draft.exclusions, draft.policyExclusions[id]);
}

/* ------------------------------------------------------------ mutation --- */

const ENFORCE_ACTIONS = new Set(["Sigkill", "Override", "NotifyEnforcer", "Signal"]);
const HOOK_GROUPS = ["kprobes", "tracepoints", "uprobes", "lsmhooks", "usdts"] as const;

type Dict = Record<string, unknown>;

function hooksOf(spec: Dict): Dict[] {
  const out: Dict[] = [];
  for (const g of HOOK_GROUPS) {
    const list = spec[g];
    if (Array.isArray(list)) out.push(...(list as Dict[]));
  }
  return out;
}

/**
 * Alert strips enforcement; Block adds it only where the policy has somewhere
 * to put it. We never convert an Override with argError into a Sigkill — the
 * CVE policies use Override deliberately, and killing instead of returning an
 * error changes what the mitigation does.
 */
function applyMode(spec: Dict, mode: PolicyMode): { changed: boolean; enforceable: boolean } {
  let changed = false;
  let enforceable = false;
  for (const hook of hooksOf(spec)) {
    const selectors = hook.selectors;
    if (!Array.isArray(selectors)) continue;
    for (const sel of selectors as Dict[]) {
      for (const key of ["matchActions", "matchReturnActions"]) {
        const actions = sel[key];
        if (!Array.isArray(actions)) continue;
        enforceable = true;
        for (const a of actions as Dict[]) {
          const action = String(a.action ?? "");
          if (mode === "alert" && ENFORCE_ACTIONS.has(action)) {
            a.action = "Post";
            delete a.argError;
            delete a.argSig;
            changed = true;
          }
          if (mode === "block" && action === "Post") {
            a.action = "Sigkill";
            changed = true;
          }
        }
      }
    }
  }
  return { changed, enforceable };
}

/** Index of the first file/string argument of a hook, or null. */
function pathArgIndex(hook: Dict): number | null {
  const args = hook.args;
  if (!Array.isArray(args)) return null;
  for (const a of args as Dict[]) {
    const t = String(a.type ?? "");
    if (t === "file" || t === "string" || t === "fd" || t === "path") {
      const idx = a.index;
      if (typeof idx === "number") return idx;
    }
  }
  return null;
}

function labelSelector(pairs: string[]): Dict {
  const byKey = new Map<string, string[]>();
  for (const pair of pairs) {
    const i = pair.indexOf("=");
    const key = i >= 0 ? pair.slice(0, i) : pair;
    const value = i >= 0 ? pair.slice(i + 1) : "";
    byKey.set(key, [...(byKey.get(key) ?? []), value]);
  }
  return {
    matchExpressions: [...byKey.entries()].map(([key, values]) => ({
      key,
      operator: "NotIn",
      values: [...new Set(values)].sort(),
    })),
  };
}

function matchLabels(pairs: string[]): Dict {
  const out: Dict = {};
  for (const pair of pairs) {
    const i = pair.indexOf("=");
    if (i < 0) continue;
    out[pair.slice(0, i)] = pair.slice(i + 1);
  }
  return out;
}

export interface AppliedNote {
  policyId: string;
  dimension: ExclusionDimension;
  applied: boolean;
  detail: string;
}

/**
 * Injects exclusions into a cloned spec. Returns notes describing what landed
 * and what did not, which the review step shows verbatim — a path exclusion
 * that could not be applied to three of your policies is exactly the sort of
 * thing that should not be silent.
 */
function applyExclusions(
  spec: Dict,
  excl: Exclusions,
  policy: LibraryPolicy,
): AppliedNote[] {
  const notes: AppliedNote[] = [];
  const note = (dimension: ExclusionDimension, applied: boolean, detail: string) =>
    notes.push({ policyId: policy.id, dimension, applied, detail });

  // --- spec-level selectors ---
  if (excl.podLabels.length) {
    const existing = (spec.podSelector as Dict | undefined) ?? {};
    const sel = labelSelector(excl.podLabels);
    spec.podSelector = {
      ...existing,
      matchExpressions: [
        ...((existing.matchExpressions as Dict[]) ?? []),
        ...(sel.matchExpressions as Dict[]),
      ],
    };
    note("podLabels", true, `podSelector on ${excl.podLabels.length} label(s)`);
  }

  if (excl.containers.length) {
    const existing = (spec.containerSelector as Dict | undefined) ?? {};
    spec.containerSelector = {
      ...existing,
      matchExpressions: [
        ...((existing.matchExpressions as Dict[]) ?? []),
        { key: "name", operator: "NotIn", values: [...excl.containers].sort() },
      ],
    };
    note("containers", true, `containerSelector on ${excl.containers.length} name(s)`);
  }

  // --- selector-level filters ---
  const hooks = hooksOf(spec);
  let binaryHits = 0;
  let parentHits = 0;
  let pathHits = 0;
  let pathMisses = 0;

  for (const hook of hooks) {
    let selectors = hook.selectors as Dict[] | undefined;
    if (!Array.isArray(selectors)) {
      if (!excl.binaries.length && !excl.parentBinaries.length && !excl.paths.length) continue;
      selectors = [{}];
      hook.selectors = selectors;
    }
    const idx = pathArgIndex(hook);

    for (const sel of selectors) {
      if (excl.binaries.length && !sel.matchBinaries) {
        sel.matchBinaries = [
          { operator: "NotIn", values: [...excl.binaries].sort(), followChildren: false },
        ];
        binaryHits++;
      }
      if (excl.parentBinaries.length && !sel.matchParentBinaries) {
        sel.matchParentBinaries = [
          { operator: "NotIn", values: [...excl.parentBinaries].sort() },
        ];
        parentHits++;
      }
      if (excl.paths.length) {
        if (idx === null) {
          pathMisses++;
        } else {
          const existing = (sel.matchArgs as Dict[] | undefined) ?? [];
          // Two matchArgs on the same index contradict each other, so leave a
          // selector that already constrains the path argument alone.
          if (existing.some((m) => m.index === idx)) {
            pathMisses++;
          } else {
            sel.matchArgs = [
              ...existing,
              { index: idx, operator: "NotPrefix", values: [...excl.paths].sort() },
            ];
            pathHits++;
          }
        }
      }
    }
  }

  if (excl.binaries.length) {
    note("binaries", binaryHits > 0, `matchBinaries added to ${binaryHits} selector(s)`);
  }
  if (excl.parentBinaries.length) {
    note("parentBinaries", parentHits > 0, `matchParentBinaries added to ${parentHits} selector(s)`);
  }
  if (excl.paths.length) {
    note(
      "paths",
      pathHits > 0,
      pathHits > 0
        ? `matchArgs NotPrefix added to ${pathHits} selector(s)` +
            (pathMisses ? `; ${pathMisses} skipped (no path argument, or already constrained)` : "")
        : "no selector takes a file/string argument this could filter on",
    );
  }
  return notes;
}

/* ----------------------------------------------------------- assembly --- */

export interface BuiltPolicy {
  policy: LibraryPolicy;
  mode: PolicyMode;
  objects: Dict[];
  notes: AppliedNote[];
  /** Upstream shipped enforcement but the user asked for alert, or vice versa. */
  modeChanged: boolean;
  /** Block was requested but the policy has no matchActions to put it in. */
  notEnforceable: boolean;
  /** metadata.name had to be suffixed because two selections collided. */
  renamedFrom?: string;
}

export function buildPolicies(draft: LibraryDraft): BuiltPolicy[] {
  const used = new Map<string, number>();
  const out: BuiltPolicy[] = [];

  for (const policy of selectedPolicies(draft)) {
    const mode = draft.selection[policy.id];
    const excl = effectiveExclusions(draft, policy.id);
    const spec = structuredClone(policy.spec) as Dict;

    const { changed, enforceable } = applyMode(spec, mode);
    const notes = applyExclusions(spec, excl, policy);

    if (draft.nodeLabels.length) spec.nodeSelector = matchLabels(draft.nodeLabels);
    if (draft.includeHost) spec.hostSelector = {};

    // Two upstream files really do both define metadata.name: "syscalls".
    const seen = used.get(policy.name) ?? 0;
    used.set(policy.name, seen + 1);
    const name = seen === 0 ? policy.name : `${policy.name}-${seen + 1}`;

    const meta: Dict = {
      name,
      labels: {
        "app.kubernetes.io/managed-by": "isovalent-control",
        "isovalent-control.io/category": policy.section,
        "isovalent-control.io/source": policy.source,
      },
      annotations: {
        "isovalent-control.io/action": mode === "block" ? "enforce" : "monitor",
        "isovalent-control.io/description": policy.summary,
        ...(policy.url ? { "isovalent-control.io/upstream": policy.url } : {}),
      },
    };

    const objects: Dict[] = [];
    if (draft.scopeNamespaces.length) {
      // Namespace scoping in Tetragon is inclusion: one namespaced policy each.
      for (const ns of [...draft.scopeNamespaces].sort()) {
        objects.push({
          apiVersion: "cilium.io/v1alpha1",
          kind: "TracingPolicyNamespaced",
          metadata: { ...meta, namespace: ns },
          spec: structuredClone(spec),
        });
      }
    } else {
      objects.push({
        apiVersion: "cilium.io/v1alpha1",
        kind: "TracingPolicy",
        metadata: meta,
        spec,
      });
    }

    out.push({
      policy,
      mode,
      objects,
      notes,
      modeChanged: changed,
      notEnforceable: mode === "block" && !enforceable,
      renamedFrom: seen === 0 ? undefined : policy.name,
    });
  }
  return out;
}

/* --------------------------------------------------------- agent config --- */

/**
 * Namespace exclusions and health-check noise are agent-level export filters,
 * not policy content. Emitted as a Helm values fragment plus the equivalent
 * tetragon.conf.d drop-in, mirroring examples/configuration upstream.
 */
export function buildAgentConfig(draft: LibraryDraft): string | null {
  const namespaces = draft.exclusions.namespaces;
  if (!namespaces.length) return null;
  const denyLines = [
    JSON.stringify({ health_check: true }),
    JSON.stringify({ namespace: [...namespaces].sort() }),
  ];
  return [
    "# Tetragon agent export filters.",
    "#",
    "# Namespace exclusions are applied here rather than in a TracingPolicy,",
    "# because Tetragon scopes namespaces by inclusion (TracingPolicyNamespaced),",
    "# not exclusion. This drops the EVENTS from these namespaces; it does not",
    "# disable enforcement for them. To exempt a namespace from enforcement too,",
    "# scope the policy to the namespaces you do want instead.",
    "#",
    "# Helm (cilium/tetragon chart):",
    "tetragon:",
    "  exportDenyList: |",
    ...denyLines.map((l) => `    ${l}`),
    "",
    "# Standalone equivalent — /etc/tetragon/tetragon.conf.d/export-denylist",
    "# (one JSON object per line, same content as above):",
    ...denyLines.map((l) => `#   ${l}`),
    "",
  ].join("\n");
}

/* -------------------------------------------------------------- output --- */

export interface GeneratedDoc {
  id: string;
  title: string;
  kind: string;
  filename: string;
  objects: Dict[];
  yaml: string;
  note?: string;
}

export function slug(input: string, fallback = "runtime-security"): string {
  const s = input
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "");
  return s.length ? s.slice(0, 63).replace(/-$/, "") : fallback;
}

function dump(objects: Dict[], header: string[]): string {
  const body = objects
    .map((o) => yaml.dump(o, { noRefs: true, lineWidth: 100 }))
    .join("---\n");
  return `${header.map((l) => `# ${l}`).join("\n")}\n${body}`;
}

export function generateDocs(draft: LibraryDraft): GeneratedDoc[] {
  const built = buildPolicies(draft);
  const docs: GeneratedDoc[] = [];
  const base = slug(draft.name);
  if (!built.length) return docs;

  // One document per section, so a 30-policy selection stays readable.
  const bySection = new Map<string, BuiltPolicy[]>();
  for (const b of built) {
    bySection.set(b.policy.section, [...(bySection.get(b.policy.section) ?? []), b]);
  }

  for (const [section, items] of bySection) {
    const objects = items.flatMap((i) => i.objects);
    const enforcing = items.filter((i) => i.mode === "block").length;
    docs.push({
      id: `policies-${section}`,
      title: `${section} (${items.length})`,
      kind: "TracingPolicy",
      filename: `${base}-${section}.yaml`,
      objects,
      note: items.some((i) => i.notEnforceable)
        ? "Some policies here define no matchActions, so Block cannot be applied to them — they were left reporting only. They are flagged in the selection list."
        : undefined,
      yaml: dump(objects, [
        `Tetragon policies — ${section}.`,
        `${items.length} policy(ies), ${enforcing} enforcing.`,
        draft.scopeNamespaces.length
          ? `Scoped to namespace(s): ${draft.scopeNamespaces.join(", ")} (TracingPolicyNamespaced).`
          : "Cluster-wide (TracingPolicy).",
        "Specs are upstream cilium/tetragon examples with mode and exclusions applied.",
      ]),
    });
  }

  const agent = buildAgentConfig(draft);
  if (agent) {
    docs.push({
      id: "agent-config",
      title: "Agent config",
      kind: "Config",
      filename: `${base}-tetragon-config.yaml`,
      objects: [],
      yaml: agent,
      note: "Applied to the Tetragon agent, not the cluster API. This console does not apply it for you — it changes how the agent runs.",
    });
  }

  return docs;
}

export function generateBundle(draft: LibraryDraft): string {
  return generateDocs(draft)
    .map((d) => d.yaml.trimEnd())
    .join("\n---\n");
}

/* ------------------------------------------------------------ summary --- */

export interface DraftSummary {
  total: number;
  blocking: number;
  alerting: number;
  bySection: Record<string, number>;
  enforcingUpstream: number;
  notEnforceable: number;
  sampleOnly: number;
  exclusions: number;
  policiesWithOwnExclusions: number;
  objectCount: number;
  /** Per-dimension roll-up, because a per-policy list of 74 identical
   *  "not applicable" notes is noise rather than information. */
  noteSummary: {
    dimension: ExclusionDimension;
    applied: number;
    notApplied: number;
    reason: string;
  }[];
}

export function summarize(draft: LibraryDraft): DraftSummary {
  const built = buildPolicies(draft);
  const bySection: Record<string, number> = {};
  for (const b of built) {
    bySection[b.policy.section] = (bySection[b.policy.section] ?? 0) + 1;
  }
  return {
    total: built.length,
    blocking: built.filter((b) => b.mode === "block").length,
    alerting: built.filter((b) => b.mode === "alert").length,
    bySection,
    enforcingUpstream: built.filter((b) => b.policy.enforcing).length,
    notEnforceable: built.filter((b) => b.notEnforceable).length,
    sampleOnly: built.filter((b) => b.policy.source === "tracingpolicy").length,
    exclusions: countExclusions(draft.exclusions),
    policiesWithOwnExclusions: Object.values(draft.policyExclusions).filter(
      (e) => countExclusions(e) > 0,
    ).length,
    objectCount: built.reduce((n, b) => n + b.objects.length, 0),
    noteSummary: rollUpNotes(built.flatMap((b) => b.notes)),
  };
}

function rollUpNotes(notes: AppliedNote[]) {
  const byDim = new Map<ExclusionDimension, { applied: number; notApplied: number; reason: string }>();
  for (const n of notes) {
    const cur = byDim.get(n.dimension) ?? { applied: 0, notApplied: 0, reason: "" };
    if (n.applied) cur.applied++;
    else {
      cur.notApplied++;
      if (!cur.reason) cur.reason = n.detail;
    }
    byDim.set(n.dimension, cur);
  }
  return [...byDim.entries()].map(([dimension, v]) => ({ dimension, ...v }));
}
