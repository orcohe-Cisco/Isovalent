"use client";

import { Chip } from "@/components/ui/Chip";
import { Segmented } from "@/components/ui/Segmented";
import type { LibraryPolicy, PolicyMode } from "@/lib/policyLibrary";

const SOURCE_TONE = {
  policylibrary: "ok",
  authored: "violet",
  quickstart: "accent",
  other: "neutral",
  tracingpolicy: "neutral",
} as const;

const SOURCE_LABEL = {
  policylibrary: "policy library",
  authored: "authored",
  quickstart: "quickstart",
  other: "demo",
  tracingpolicy: "sample",
} as const;

export function PolicyCard({
  policy,
  mode,
  exclusionCount,
  onToggle,
  onModeChange,
  onDetails,
}: {
  policy: LibraryPolicy;
  mode: PolicyMode | undefined;
  exclusionCount: number;
  onToggle: () => void;
  onModeChange: (next: PolicyMode) => void;
  onDetails: () => void;
}) {
  const selected = Boolean(mode);

  return (
    <div
      role="button"
      tabIndex={0}
      aria-pressed={selected}
      aria-label={`${policy.title} — ${selected ? "selected" : "not selected"}`}
      onClick={onToggle}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onToggle();
        }
      }}
      className={`card-interactive flex h-full flex-col p-3.5 text-left ${
        selected ? "card-selected" : ""
      }`}
    >
      <div className="flex items-start gap-2.5">
        <span
          aria-hidden
          className="mt-0.5 flex h-[18px] w-[18px] shrink-0 items-center justify-center rounded-[6px] text-[11px] font-bold"
          style={{
            background: selected ? "#3987e5" : "transparent",
            border: `1px solid ${selected ? "#3987e5" : "var(--hairline-strong)"}`,
            color: "#fff",
            transition:
              "background-color var(--dur-fast) var(--ease-out), border-color var(--dur-fast) var(--ease-out)",
          }}
        >
          {selected ? "✓" : ""}
        </span>

        <div className="min-w-0 flex-1">
          <div className="flex items-start justify-between gap-2">
            <h3 className="mono text-[12.5px] font-semibold leading-5">
              {policy.name}
            </h3>
            <Chip tone={SOURCE_TONE[policy.source]}>{SOURCE_LABEL[policy.source]}</Chip>
          </div>
          <p className="mt-1 text-xs leading-[1.45] text-[color:var(--text-secondary)]">
            {policy.summary}
          </p>
        </div>
      </div>

      <div className="mt-2.5 flex flex-wrap items-center gap-1.5 pl-[28px]">
        {policy.enforcing && (
          <Chip tone="danger" title={`Upstream ships this enforcing: ${policy.actions.join(", ")}`}>
            ships enforcing
          </Chip>
        )}
        {exclusionCount > 0 && <Chip tone="warn">{exclusionCount} exclusion</Chip>}
        {policy.hooks.slice(0, 2).map((h) => (
          <Chip key={h} mono title="Kernel hook">
            {h.length > 26 ? h.slice(0, 25) + "…" : h}
          </Chip>
        ))}
        {policy.hooks.length > 2 && <Chip>+{policy.hooks.length - 2}</Chip>}
      </div>

      <div className="mt-auto flex items-center justify-between gap-2 pl-[28px] pt-3">
        <Segmented<PolicyMode>
          value={mode ?? "alert"}
          disabled={!selected}
          ariaLabel={`Mode for ${policy.name}`}
          size="sm"
          onChange={onModeChange}
          options={[
            {
              value: "alert",
              label: "Alert",
              tone: selected ? "warn" : "neutral",
              title: "Every action becomes Post — report only",
            },
            {
              value: "block",
              label: "Block",
              tone: selected ? "danger" : "neutral",
              title: "Keeps upstream enforcement, or upgrades Post to Sigkill",
            },
          ]}
        />
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onDetails();
          }}
          className="btn btn-ghost px-2 py-1 text-[11px]"
        >
          Details
        </button>
      </div>
    </div>
  );
}
