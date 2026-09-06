"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { apiPut } from "@/lib/api";
import yaml from "js-yaml";
import {
  POLICIES,
  SECTIONS,
  matchesQuery,
  type LibraryPolicy,
  type PolicyMode,
  type SectionId,
} from "@/lib/policyLibrary";
import {
  emptyDraft,
  generateDocs,
  slug,
  summarize,
  type GeneratedDoc,
  type LibraryDraft,
} from "@/lib/libraryYaml";
import { useObserved, type ObservedResponse } from "@/lib/observed";
import { PolicyCard } from "@/components/library/PolicyCard";
import { Chip } from "@/components/ui/Chip";
import { Segmented } from "@/components/ui/Segmented";
import { Sheet } from "@/components/ui/Sheet";
import { Stepper } from "@/components/ui/Stepper";
import { YamlBlock } from "@/components/ui/YamlBlock";

const STEPS = [
  { id: "define", label: "Define", hint: "Name and scope" },
  { id: "select", label: "Choose policies", hint: "Tetragon library" },
  { id: "review", label: "Review", hint: "Generated manifests" },
  { id: "confirm", label: "Confirm", hint: "Apply to cluster" },
];

const FALLBACK_NAMESPACES = ["default", "big-monolith", "secure-middleware"];

/* ---------------------------------------------------------- presets --- */

const ids = (fn: (p: LibraryPolicy) => boolean) => POLICIES.filter(fn).map((p) => p.id);
const asSelection = (list: string[], mode: PolicyMode) =>
  Object.fromEntries(list.map((id) => [id, mode])) as Record<string, PolicyMode>;

interface Bundle {
  id: string;
  name: string;
  description: string;
  selection: () => Record<string, PolicyMode>;
}

const BUNDLES: Bundle[] = [
  {
    id: "essentials",
    name: "Runtime essentials",
    description:
      "The documented policy-library set plus the escape primitives, all reporting. A sane standing configuration that will not page you at 3am.",
    selection: () =>
      asSelection(
        [
          ...ids((p) => p.source === "policylibrary" && p.section !== "instrumentation"),
          "quickstart-file-monitoring",
          "tracingpolicy-sys-mount",
          "tracingpolicy-sys-ptrace",
          "tracingpolicy-process-exec-process-exec-elf-begin",
        ],
        "alert",
      ),
  },
  {
    id: "cve",
    name: "CVE watch",
    description:
      "Every CVE policy in the library, reporting only. Two are upstream Tetragon examples; four were written here against the published advisories.",
    selection: () => asSelection(ids((p) => p.section === "cve"), "alert"),
  },
  {
    id: "escape",
    name: "Container escape — enforcing",
    description:
      "Escape primitives and the escape-related CVEs, set to Block. Aggressive; run it in Alert on a real cluster first.",
    selection: () =>
      asSelection(ids((p) => p.section === "escape" || p.section === "cve"), "block"),
  },
  {
    id: "documented",
    name: "Documented only",
    description:
      "Everything upstream documents or we authored — policy library, quickstart and CVEs. Excludes the sample-only policies that carry no guarantee.",
    selection: () =>
      asSelection(
        ids((p) => ["policylibrary", "quickstart", "authored"].includes(p.source) || p.section === "cve"),
        "alert",
      ),
  },
  {
    id: "goat",
    name: "Kubernetes Goat demo",
    description:
      "Tuned for the Goat walkthrough: Block on what the scenarios actually trip, Alert on the supporting visibility.",
    selection: () => ({
      ...asSelection(ids((p) => p.section === "cve"), "alert"),
      "tracingpolicy-sys-mount": "block",
      "tracingpolicy-sys-ptrace": "alert",
      "tracingpolicy-sandbox-linux-namespaces-kill-unprivileged-user-namespace": "block",
      "quickstart-file-monitoring-enforce": "block",
      "quickstart-network-egress-cluster": "alert",
      "policylibrary-modules": "alert",
      "policylibrary-bpf": "alert",
      "policylibrary-privileges-privileges-setuid-root": "alert",
      "tracingpolicy-process-exec-process-exec-elf-begin": "alert",
    }),
  },
];

/* --------------------------------------------------------- component --- */

interface ApplyOutcome {
  kind: string;
  name: string;
  ok: boolean;
  detail?: string;
}

export function LibraryWizard() {
  const [step, setStep] = useState(0);
  const [draft, setDraft] = useState<LibraryDraft>(emptyDraft);
  const [query, setQuery] = useState("");
  const [detail, setDetail] = useState<LibraryPolicy | null>(null);
  const [namespaces, setNamespaces] = useState<string[]>(FALLBACK_NAMESPACES);
  const [openSections, setOpenSections] = useState<SectionId[]>(["cve", "escape"]);
  const [activeDoc, setActiveDoc] = useState(0);
  const [applying, setApplying] = useState(false);
  const [outcomes, setOutcomes] = useState<ApplyOutcome[] | null>(null);
  const [hideSamples, setHideSamples] = useState(false);

  const { data: observed, error: observedError } = useObserved();

  useEffect(() => {
    const found = (observed.values.namespaces ?? []).map((v) => v.value);
    if (found.length) setNamespaces(found);
  }, [observed]);

  const summary = useMemo(() => summarize(draft), [draft]);
  const docs = useMemo<GeneratedDoc[]>(
    () => (summary.total ? generateDocs(draft) : []),
    [draft, summary.total],
  );

  const setMode = useCallback((id: string, mode: PolicyMode) => {
    setDraft((d) => ({ ...d, selection: { ...d.selection, [id]: mode } }));
  }, []);

  const toggle = useCallback((p: LibraryPolicy) => {
    setDraft((d) => {
      const next = { ...d.selection };
      if (next[p.id]) delete next[p.id];
      // Upstream already ships enforcement on some policies — default to the
      // mode the author chose rather than silently downgrading it.
      else next[p.id] = p.enforcing ? "block" : "alert";
      return { ...d, selection: next };
    });
  }, []);

  const setSection = (section: SectionId, mode: PolicyMode | null) => {
    setDraft((d) => {
      const next = { ...d.selection };
      for (const p of POLICIES.filter((x) => x.section === section)) {
        if (mode === null) delete next[p.id];
        else next[p.id] = mode;
      }
      return { ...d, selection: next };
    });
  };

  const apply = async () => {
    setApplying(true);
    setOutcomes(null);
    const results: ApplyOutcome[] = [];
    for (const doc of docs) {
      if (doc.kind === "Config") continue; // agent config, not a cluster object
      for (const obj of doc.objects) {
        const kind = String(obj.kind);
        const meta = obj.metadata as { name: string; namespace?: string };
        const ns = meta.namespace || "-";
        try {
          await apiPut(`/api/v1/policies/${kind}/${ns}/${meta.name}`, obj);
          results.push({ kind, name: meta.name, ok: true });
        } catch (e) {
          results.push({ kind, name: meta.name, ok: false, detail: String(e) });
        }
      }
    }
    setOutcomes(results);
    setApplying(false);
  };

  const canAdvance = step === 0 ? draft.name.trim().length > 0 : summary.total > 0;

  return (
    <div className="space-y-5">
      <div className="panel px-3 py-2">
        <Stepper steps={STEPS} current={step} onJump={setStep} />
      </div>

      {step === 0 && (
        <DefineStep
          draft={draft}
          setDraft={setDraft}
          namespaces={namespaces}
          observed={observed}
          observedError={observedError}
        />
      )}

      {step === 1 && (
        <SelectStep
          draft={draft}
          query={query}
          setQuery={setQuery}
          hideSamples={hideSamples}
          setHideSamples={setHideSamples}
          openSections={openSections}
          setOpenSections={setOpenSections}
          onBundle={(b) => setDraft((d) => ({ ...d, selection: b.selection() }))}
          onSetSection={setSection}
          onToggle={toggle}
          onModeChange={setMode}
          onDetails={setDetail}
        />
      )}

      {step === 2 && (
        <ReviewStep docs={docs} active={activeDoc} setActive={setActiveDoc} summary={summary} />
      )}

      {step === 3 && (
        <ConfirmStep
          draft={draft}
          docs={docs}
          summary={summary}
          applying={applying}
          outcomes={outcomes}
          onApply={apply}
        />
      )}

      <div className="panel sticky bottom-4 flex items-center justify-between gap-4 px-4 py-3">
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <Chip tone={summary.total ? "accent" : "neutral"}>
            {summary.total} of {POLICIES.length} policies
          </Chip>
          <Chip tone={summary.blocking ? "danger" : "neutral"}>{summary.blocking} enforcing</Chip>
          <Chip tone={summary.alerting ? "warn" : "neutral"}>{summary.alerting} reporting</Chip>
          {summary.objectCount !== summary.total && (
            <Chip tone="neutral">{summary.objectCount} objects</Chip>
          )}
          {summary.sampleOnly > 0 && (
            <Chip tone="warn" title="From examples/tracingpolicy — upstream gives no guarantee">
              {summary.sampleOnly} sample-only
            </Chip>
          )}
        </div>
        <div className="flex items-center gap-2">
          <button
            className="btn btn-secondary"
            disabled={step === 0}
            onClick={() => setStep((s) => Math.max(0, s - 1))}
          >
            Back
          </button>
          <button
            className="btn btn-primary"
            disabled={step === STEPS.length - 1 || !canAdvance}
            onClick={() => setStep((s) => Math.min(STEPS.length - 1, s + 1))}
          >
            {step === 2 ? "Continue to confirm" : "Next"}
          </button>
        </div>
      </div>

      <PolicyDetailSheet
        policy={detail}
        mode={detail ? draft.selection[detail.id] : undefined}
        onClose={() => setDetail(null)}
        onToggle={() => detail && toggle(detail)}
        onModeChange={(m) => detail && setMode(detail.id, m)}
      />
    </div>
  );
}

/* ============================================================ step 1 === */

function DefineStep({
  draft,
  setDraft,
  namespaces,
  observed,
  observedError,
}: {
  draft: LibraryDraft;
  setDraft: React.Dispatch<React.SetStateAction<LibraryDraft>>;
  namespaces: string[];
  observed: ObservedResponse;
  observedError: string | null;
}) {
  const observedTotal = Object.values(observed.counts ?? {}).reduce((a, b) => a + b, 0);
  const toggleNs = (ns: string) =>
    setDraft((d) => ({
      ...d,
      scopeNamespaces: d.scopeNamespaces.includes(ns)
        ? d.scopeNamespaces.filter((x) => x !== ns)
        : [...d.scopeNamespaces, ns],
    }));

  return (
    <div className="animate-fade-up space-y-4">
      <div className="grid gap-4 lg:grid-cols-[1.2fr_1fr]">
        <section className="panel space-y-4 p-5">
          <div>
            <h2 className="text-sm font-semibold">Policy set</h2>
            <p className="mt-0.5 text-xs text-[color:var(--text-secondary)]">
              Used to name the generated files. Each policy keeps its own upstream
              metadata.name so it stays recognisable against the Tetragon docs.
            </p>
          </div>
          <label className="block">
            <span className="section-label">Name</span>
            <input
              className="field mt-1.5"
              value={draft.name}
              onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))}
            />
            <span className="mono mt-1 block text-[11px] text-[color:var(--text-tertiary)]">
              {slug(draft.name)}-&lt;section&gt;.yaml
            </span>
          </label>
          <label className="block">
            <span className="section-label">Description</span>
            <textarea
              className="field mt-1.5 h-20 resize-y"
              value={draft.description}
              onChange={(e) => setDraft((d) => ({ ...d, description: e.target.value }))}
              placeholder="What this set is for, and who signed off on it."
            />
          </label>
        </section>

        <section className="panel space-y-4 p-5">
          <div>
            <h2 className="text-sm font-semibold">Scope</h2>
            <p className="mt-0.5 text-xs leading-relaxed text-[color:var(--text-secondary)]">
              Tetragon scopes namespaces by <em>including</em> them. Pick none and
              you get one cluster-wide <span className="mono">TracingPolicy</span>;
              pick some and you get a{" "}
              <span className="mono">TracingPolicyNamespaced</span> per namespace.
            </p>
          </div>
          <div>
            <span className="section-label">Namespaces</span>
            <div className="mt-2 flex flex-wrap gap-1.5">
              {namespaces.map((ns) => {
                const on = draft.scopeNamespaces.includes(ns);
                return (
                  <button
                    key={ns}
                    onClick={() => toggleNs(ns)}
                    className="mono rounded-md px-2 py-1 text-[11px] transition-colors duration-150"
                    style={{
                      background: on ? "rgba(57,135,229,0.16)" : "var(--surface-2)",
                      border: `1px solid ${on ? "rgba(57,135,229,0.45)" : "var(--hairline)"}`,
                      color: on ? "#8fbdf3" : "var(--text-secondary)",
                    }}
                  >
                    {ns}
                  </button>
                );
              })}
            </div>
            {draft.scopeNamespaces.length === 0 && (
              <p className="mt-2 text-[11px] text-[color:var(--text-tertiary)]">
                Cluster-wide.
              </p>
            )}
          </div>
          <label className="flex items-start gap-2.5">
            <input
              type="checkbox"
              className="mt-0.5"
              checked={draft.includeHost}
              onChange={(e) => setDraft((d) => ({ ...d, includeHost: e.target.checked }))}
            />
            <span className="text-xs leading-relaxed">
              <span className="font-medium">Include host workloads</span>
              <span className="mono ml-1.5 text-[color:var(--text-tertiary)]">
                hostSelector: {}
              </span>
              <span className="mt-0.5 block text-[11px] text-[color:var(--text-tertiary)]">
                Processes outside any container. Off by default — on a node running
                system daemons this is where the volume comes from. Not valid with
                namespaced policies.
              </span>
            </span>
          </label>
          <label className="block">
            <span className="section-label">Node selector</span>
            <input
              className="field mono mt-1.5 text-xs"
              value={draft.nodeLabels.join(", ")}
              onChange={(e) =>
                setDraft((d) => ({
                  ...d,
                  nodeLabels: e.target.value.split(",").map((s) => s.trim()).filter(Boolean),
                }))
              }
              placeholder="kubernetes.io/os=linux"
            />
            <span className="mt-1 block text-[11px] text-[color:var(--text-tertiary)]">
              Restricts which nodes load the policy at all, as opposed to which
              workloads it applies to.
            </span>
          </label>
        </section>
      </div>

      <Callout tone="neutral">
        Exclusions are not part of authoring a policy. You cannot know what to exclude before the
        policy has run — you find out days later, when it has fired four thousand times on your log
        shipper. Apply this set in alert mode, then use the <strong>Exclusions</strong> tab, which
        starts from what actually fired and writes the exclusion back into the policy.
      </Callout>
    </div>
  );
}

/* ============================================================ step 2 === */

function SelectStep({
  draft,
  query,
  setQuery,
  hideSamples,
  setHideSamples,
  openSections,
  setOpenSections,
  onBundle,
  onSetSection,
  onToggle,
  onModeChange,
  onDetails,
}: {
  draft: LibraryDraft;
  query: string;
  setQuery: (q: string) => void;
  hideSamples: boolean;
  setHideSamples: (v: boolean) => void;
  openSections: SectionId[];
  setOpenSections: React.Dispatch<React.SetStateAction<SectionId[]>>;
  onBundle: (b: Bundle) => void;
  onSetSection: (s: SectionId, mode: PolicyMode | null) => void;
  onToggle: (p: LibraryPolicy) => void;
  onModeChange: (id: string, mode: PolicyMode) => void;
  onDetails: (p: LibraryPolicy) => void;
}) {
  const searching = query.trim().length > 0;

  return (
    <div className="animate-fade-up space-y-4">
      <div className="panel flex flex-wrap items-center gap-3 p-3">
        <input
          className="field max-w-xs flex-1"
          placeholder="Search hooks, names, CVEs, Goat scenarios…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <div className="flex items-center gap-2">
          <span className="section-label">Bundle</span>
          <select
            className="field w-auto py-1.5 text-xs"
            defaultValue=""
            onChange={(e) => {
              const b = BUNDLES.find((x) => x.id === e.target.value);
              if (b) onBundle(b);
              e.target.value = "";
            }}
          >
            <option value="" disabled>
              Choose…
            </option>
            {BUNDLES.map((b) => (
              <option key={b.id} value={b.id} title={b.description}>
                {b.name} ({Object.keys(b.selection()).length})
              </option>
            ))}
          </select>
        </div>
        <label className="flex items-center gap-1.5 text-[11px] text-[color:var(--text-secondary)]">
          <input
            type="checkbox"
            checked={hideSamples}
            onChange={(e) => setHideSamples(e.target.checked)}
          />
          Hide sample-only policies
        </label>
        <div className="ml-auto text-[11px] text-[color:var(--text-tertiary)]">
          Alert = Post everywhere · Block = upstream enforcement, or Post→Sigkill
        </div>
      </div>

      {SECTIONS.map((section) => {
        const all = POLICIES.filter(
          (p) => p.section === section.id && (!hideSamples || p.source !== "tracingpolicy"),
        );
        const visible = all.filter((p) => matchesQuery(p, query));
        if (!visible.length) return null;
        const selectedCount = all.filter((p) => draft.selection[p.id]).length;
        const open = searching || openSections.includes(section.id);

        return (
          <section key={section.id} className="panel overflow-hidden">
            <header className="hairline-b flex flex-wrap items-center justify-between gap-3 px-4 py-3">
              <button
                className="min-w-0 flex-1 text-left"
                onClick={() =>
                  setOpenSections((s) =>
                    s.includes(section.id)
                      ? s.filter((x) => x !== section.id)
                      : [...s, section.id],
                  )
                }
              >
                <div className="flex items-center gap-2">
                  <span
                    aria-hidden
                    className="text-[10px] text-[color:var(--text-tertiary)]"
                    style={{
                      display: "inline-block",
                      transform: open ? "rotate(90deg)" : "none",
                      transition: "transform var(--dur) var(--ease-out)",
                    }}
                  >
                    ▶
                  </span>
                  <h2 className="text-sm font-semibold">{section.label}</h2>
                  <Chip tone={selectedCount ? "accent" : "neutral"}>
                    {selectedCount} / {all.length}
                  </Chip>
                </div>
                <p className="mt-0.5 max-w-3xl pl-5 text-xs text-[color:var(--text-secondary)]">
                  {section.blurb}
                </p>
              </button>
              <div className="flex items-center gap-1">
                <button
                  className="btn btn-ghost px-2 py-1 text-[11px]"
                  onClick={() => onSetSection(section.id, "alert")}
                >
                  All alert
                </button>
                <button
                  className="btn btn-ghost px-2 py-1 text-[11px]"
                  onClick={() => onSetSection(section.id, "block")}
                >
                  All block
                </button>
                <button
                  className="btn btn-ghost px-2 py-1 text-[11px]"
                  onClick={() => onSetSection(section.id, null)}
                >
                  Clear
                </button>
              </div>
            </header>
            {open && (
              <div className="grid gap-3 p-4 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
                {visible.map((p) => (
                  <PolicyCard
                    key={p.id}
                    policy={p}
                    mode={draft.selection[p.id]}
                    exclusionCount={0}
                    onToggle={() => onToggle(p)}
                    onModeChange={(m) => onModeChange(p.id, m)}
                    onDetails={() => onDetails(p)}
                  />
                ))}
              </div>
            )}
          </section>
        );
      })}
    </div>
  );
}

/* ============================================================ step 3 === */

function ReviewStep({
  docs,
  active,
  setActive,
  summary,
}: {
  docs: GeneratedDoc[];
  active: number;
  setActive: (i: number) => void;
  summary: ReturnType<typeof summarize>;
}) {
  const doc = docs[Math.min(active, docs.length - 1)];
  if (!docs.length) {
    return (
      <div className="panel animate-fade-up p-8 text-center text-sm text-[color:var(--text-secondary)]">
        Nothing selected yet — go back and pick a bundle.
      </div>
    );
  }

  return (
    <div className="animate-fade-up space-y-4">
      {summary.notEnforceable > 0 && (
        <Callout tone="warn">
          <strong className="font-semibold">
            {summary.notEnforceable} policy(ies) cannot enforce
          </strong>{" "}
          — they define no matchActions, so there is nowhere to put a Sigkill.
          They were left reporting.
        </Callout>
      )}
      {summary.noteSummary.length > 0 && (
        <Callout tone={summary.noteSummary.some((n) => n.notApplied) ? "warn" : "neutral"}>
          <strong className="font-semibold">Where your exclusions landed</strong>
          {summary.noteSummary.map((n) => (
            <span key={n.dimension} className="mt-1 block">
              <span className="mono">{n.dimension}</span> — applied to {n.applied}{" "}
              policy(ies)
              {n.notApplied > 0 && (
                <>
                  , not applicable to {n.notApplied} ({n.reason})
                </>
              )}
            </span>
          ))}
        </Callout>
      )}
      {summary.sampleOnly > 0 && (
        <Callout tone="neutral">
          {summary.sampleOnly} of these come from{" "}
          <span className="mono">examples/tracingpolicy</span>, which upstream
          ships as samples with no security-observability guarantee. Read them
          before you rely on them.
        </Callout>
      )}

      <div className="flex flex-wrap gap-1.5">
        {docs.map((d, i) => (
          <button
            key={d.id}
            onClick={() => setActive(i)}
            className="rounded-lg px-3 py-1.5 text-xs font-medium transition-colors duration-200"
            style={{
              background: i === active ? "var(--surface-3)" : "transparent",
              border: `1px solid ${i === active ? "var(--hairline-strong)" : "transparent"}`,
              color: i === active ? "var(--text-primary)" : "var(--text-secondary)",
            }}
          >
            {d.title}
          </button>
        ))}
      </div>

      {doc?.note && (
        <p className="max-w-4xl text-xs leading-relaxed text-[color:var(--text-secondary)]">
          {doc.note}
        </p>
      )}
      {doc && <YamlBlock yaml={doc.yaml} filename={doc.filename} maxHeight={520} />}
    </div>
  );
}

function Callout({
  tone,
  children,
}: {
  tone: "warn" | "neutral";
  children: React.ReactNode;
}) {
  const style =
    tone === "warn"
      ? { background: "rgba(201,133,0,0.10)", border: "1px solid rgba(201,133,0,0.28)", color: "#e0b25a" }
      : { background: "var(--surface-2)", border: "1px solid var(--hairline)", color: "var(--text-secondary)" };
  return (
    <div className="rounded-xl px-4 py-3 text-xs leading-relaxed" style={style}>
      {children}
    </div>
  );
}

/* ============================================================ step 4 === */

function ConfirmStep({
  draft,
  docs,
  summary,
  applying,
  outcomes,
  onApply,
}: {
  draft: LibraryDraft;
  docs: GeneratedDoc[];
  summary: ReturnType<typeof summarize>;
  applying: boolean;
  outcomes: ApplyOutcome[] | null;
  onApply: () => void;
}) {
  const applyable = docs.filter((d) => d.kind !== "Config");
  const objectCount = applyable.reduce((n, d) => n + d.objects.length, 0);

  return (
    <div className="animate-fade-up grid gap-4 lg:grid-cols-2">
      <section className="panel space-y-4 p-5">
        <h2 className="text-sm font-semibold">Summary</h2>
        <dl className="space-y-2.5 text-sm">
          <Row label="Policy set">{draft.name}</Row>
          <Row label="Scope">
            {draft.scopeNamespaces.length
              ? `${draft.scopeNamespaces.join(", ")} (namespaced)`
              : "cluster-wide"}
            {draft.includeHost && " · including host workloads"}
          </Row>
          <Row label="Policies">
            {summary.total} selected — {summary.blocking} enforcing, {summary.alerting}{" "}
            reporting
          </Row>
          <Row label="Sections">
            {Object.entries(summary.bySection)
              .map(([s, n]) => `${s} ${n}`)
              .join(" · ")}
          </Row>
          <Row label="Provenance">
            {summary.sampleOnly} sample-only · {summary.total - summary.sampleOnly}{" "}
            documented or authored
          </Row>
          <Row label="Exclusions">tuned later, from the Exclusions tab</Row>
        </dl>
        <div className="hairline-t pt-4">
          <button
            className="btn btn-primary w-full"
            disabled={applying || objectCount === 0}
            onClick={onApply}
          >
            {applying ? "Applying…" : `Apply ${objectCount} object(s) to the cluster`}
          </button>
          <p className="mt-2 text-[11px] leading-relaxed text-[color:var(--text-tertiary)]">
            Server-side apply, the same path Create Policy uses. Applied policies
            appear on Active Policies with the monitor/enforce toggle, and start
            accumulating hits on the Exclusions tab immediately.
            The agent config document, if present, is not applied from here — it
            changes how the Tetragon agent runs.
          </p>
        </div>
      </section>

      <section className="panel space-y-3 p-5">
        <h2 className="text-sm font-semibold">What will be created</h2>
        <ul className="max-h-[420px] space-y-1.5 overflow-y-auto">
          {applyable.flatMap((d) =>
            d.objects.map((o) => {
              const meta = o.metadata as { name: string; namespace?: string };
              return (
                <li
                  key={`${o.kind}-${meta.namespace ?? "-"}-${meta.name}`}
                  className="flex items-center gap-2 rounded-lg px-3 py-1.5"
                  style={{ background: "var(--surface-2)" }}
                >
                  <Chip mono>{String(o.kind).replace("TracingPolicy", "TP")}</Chip>
                  <span className="mono truncate text-xs">{meta.name}</span>
                  {meta.namespace && (
                    <span className="mono ml-auto text-[10px] text-[color:var(--text-tertiary)]">
                      ns:{meta.namespace}
                    </span>
                  )}
                </li>
              );
            }),
          )}
        </ul>
        {outcomes && (
          <div className="hairline-t max-h-56 space-y-1.5 overflow-y-auto pt-3">
            {outcomes.map((o, i) => (
              <div
                key={`${o.kind}/${o.name}/${i}`}
                className="mono flex items-start gap-2 text-[11px]"
                style={{ color: o.ok ? "#6dd3ab" : "#f0a3a3" }}
              >
                <span>{o.ok ? "✓" : "✕"}</span>
                <span className="min-w-0">
                  {o.name}
                  {o.detail && <span className="block opacity-80">{o.detail}</span>}
                </span>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex gap-3">
      <dt className="w-32 shrink-0 text-xs text-[color:var(--text-tertiary)]">{label}</dt>
      <dd className="min-w-0 flex-1 text-[13px]">{children}</dd>
    </div>
  );
}

/* ====================================================== detail sheet === */

function PolicyDetailSheet({
  policy,
  mode,
  onClose,
  onToggle,
  onModeChange,
}: {
  policy: LibraryPolicy | null;
  mode: PolicyMode | undefined;
  onClose: () => void;
  onToggle: () => void;
  onModeChange: (m: PolicyMode) => void;
}) {
  if (!policy) return null;
  const specYaml = yaml.dump({ spec: policy.spec }, { noRefs: true, lineWidth: 96 });

  return (
    <Sheet
      open={Boolean(policy)}
      onClose={onClose}
      width={600}
      title={<span className="mono">{policy.name}</span>}
      subtitle={`${policy.section} · ${policy.path}`}
      footer={
        <div className="flex items-center justify-between gap-3">
          <button className={mode ? "btn btn-secondary" : "btn btn-primary"} onClick={onToggle}>
            {mode ? "Remove from set" : "Add to set"}
          </button>
          {mode && (
            <Segmented<PolicyMode>
              value={mode}
              onChange={onModeChange}
              options={[
                { value: "alert", label: "Alert", tone: "warn" },
                { value: "block", label: "Block", tone: "danger" },
              ]}
            />
          )}
        </div>
      }
    >
      <div className="space-y-5 text-sm">
        <p className="leading-relaxed text-[color:var(--text-secondary)]">{policy.summary}</p>

        <div className="flex flex-wrap gap-1.5">
          {policy.enforcing && <Chip tone="danger">ships enforcing</Chip>}
          {policy.actions.map((a) => (
            <Chip key={a} mono>
              {a}
            </Chip>
          ))}
          <Chip mono>{policy.kind}</Chip>
        </div>

        <Callout tone="neutral">{policy.caveat}</Callout>

        {policy.url && (
          <a
            href={policy.url}
            target="_blank"
            rel="noreferrer noopener"
            className="mono block break-all text-[11px] text-[#8fbdf3] hover:underline"
          >
            {policy.url} ↗
          </a>
        )}

        {policy.goat && (
          <div
            className="rounded-lg px-3 py-2 text-xs"
            style={{
              background: "rgba(144,133,233,0.10)",
              border: "1px solid rgba(144,133,233,0.26)",
              color: "#b3aaf2",
            }}
          >
            <span className="font-semibold">Kubernetes Goat: </span>
            {policy.goat}
          </div>
        )}

        {policy.doc && (
          <div>
            <h3 className="section-label">Upstream notes</h3>
            <pre
              className="mono mt-2 overflow-x-auto whitespace-pre-wrap rounded-lg px-3 py-2 text-[11px] leading-relaxed"
              style={{
                background: "var(--surface-0)",
                border: "1px solid var(--hairline)",
                color: "var(--text-secondary)",
              }}
            >
              {policy.doc}
            </pre>
          </div>
        )}

        <div>
          <h3 className="section-label">Hooks</h3>
          <div className="mt-2 flex flex-wrap gap-1.5">
            {policy.hooks.map((h, i) => (
              <Chip key={`${h}-${i}`} mono>
                {h}
              </Chip>
            ))}
          </div>
        </div>

        <Callout tone="neutral">
          Tune this policy after it has run, not before. The Exclusions tab shows what actually
          fired it — process, path, user, pod label, container — with counts, and writes the
          exclusion into the live policy for you.
        </Callout>

        <div>
          <h3 className="section-label">Upstream spec</h3>
          <pre
            className="mono mt-2 max-h-80 overflow-auto rounded-lg px-3 py-2 text-[11px] leading-relaxed"
            style={{
              background: "var(--surface-0)",
              border: "1px solid var(--hairline)",
              color: "#d8d7cf",
            }}
          >
            {specYaml}
          </pre>
        </div>
      </div>
    </Sheet>
  );
}
