"use client";

import { useMemo, useState } from "react";
import { usePoll } from "@/lib/usePoll";
import type { AuditEntry } from "@/lib/types";
import { PageHeader } from "@/components/ui/PageHeader";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorNote } from "@/components/ui/ErrorNote";
import { Chip } from "@/components/ui/Chip";
import { Sheet } from "@/components/ui/Sheet";
import { useConfig } from "@/lib/config";

const WINDOWS = ["1h", "6h", "1d", "7d", "30d"];

const OUTCOME_TONE = { success: "ok", denied: "warn", error: "danger" } as const;

/**
 * Every state-changing action, including the ones that were refused.
 *
 * An audit log that only records successes tells you nothing about the
 * interesting afternoon — a denied attempt to enforce a policy against a
 * protected namespace is precisely the entry you want to find later.
 */
export default function AuditPage() {
  const [window, setWindow] = useState("1d");
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState<AuditEntry | null>(null);
  const { config } = useConfig();

  const path = useMemo(() => {
    const p = new URLSearchParams({ window, limit: "300" });
    if (query.trim()) p.set("q", query.trim());
    return `/api/v1/audit?${p.toString()}`;
  }, [window, query]);

  const { data, error, loading } = usePoll<{ entries: AuditEntry[]; stats: Record<string, number> }>(
    path,
    15000,
  );

  return (
    <>
      <PageHeader
        title="Audit"
        subtitle="Who changed what, when, and what the object looked like before and after."
        actions={
          <>
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Filter by actor, action, target…"
              className="field !w-64 !py-1.5"
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
          </>
        }
      />

      <ErrorNote error={error} />

      {!config.features.database && (
        <div className="mb-4 text-[12px] text-[color:var(--text-tertiary)]">
          No database is configured, so the audit log lives in memory and is lost when the backend
          restarts. Set <code className="mono">IC_DB_DSN</code> to keep it.
        </div>
      )}

      {!loading && (data?.entries ?? []).length === 0 ? (
        <EmptyState
          title="Nothing recorded in this window"
          body="Applying a policy, toggling enforcement, approving an exclusion or issuing an API token all land here."
        />
      ) : (
        <div className="panel overflow-hidden">
          <table className="w-full text-[12px]">
            <thead>
              <tr className="section-label">
                <th className="px-3 py-2 text-left font-medium">When</th>
                <th className="px-2 py-2 text-left font-medium">Actor</th>
                <th className="px-2 py-2 text-left font-medium">Action</th>
                <th className="px-2 py-2 text-left font-medium">Target</th>
                <th className="px-2 py-2 text-left font-medium">Outcome</th>
                <th className="px-3 py-2 text-right font-medium">Changes</th>
              </tr>
            </thead>
            <tbody>
              {(data?.entries ?? []).map((e) => (
                <tr
                  key={e.id}
                  className="hairline-t cursor-pointer transition-colors hover:bg-[color:var(--surface-2)]"
                  onClick={() => setOpen(e)}
                >
                  <td className="mono whitespace-nowrap px-3 py-2 text-[color:var(--text-tertiary)]">
                    {new Date(e.time).toLocaleString()}
                  </td>
                  <td className="mono px-2 py-2">{e.actor}</td>
                  <td className="mono px-2 py-2">{e.action}</td>
                  <td className="mono px-2 py-2 text-[color:var(--text-secondary)]">{e.target}</td>
                  <td className="px-2 py-2">
                    <Chip tone={OUTCOME_TONE[e.outcome] ?? "neutral"}>{e.outcome}</Chip>
                  </td>
                  <td className="px-3 py-2 text-right text-[color:var(--text-tertiary)]">
                    {e.diff?.length ? `${e.diff.length} field${e.diff.length === 1 ? "" : "s"}` : "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <Sheet
        open={Boolean(open)}
        onClose={() => setOpen(null)}
        width={720}
        title={open?.action ?? ""}
        subtitle={open ? `${open.actor} · ${new Date(open.time).toLocaleString()}` : undefined}
      >
        {open && (
          <div className="space-y-4">
            <div className="flex flex-wrap gap-1.5">
              <Chip tone={OUTCOME_TONE[open.outcome] ?? "neutral"}>{open.outcome}</Chip>
              {open.roles?.map((r) => <Chip key={r}>{r}</Chip>)}
              {open.sourceIP && <Chip mono>{open.sourceIP}</Chip>}
              {open.target && <Chip mono>{open.target}</Chip>}
            </div>
            {open.message && <p className="text-[13px] leading-relaxed">{open.message}</p>}
            {open.diff?.length ? (
              <section>
                <div className="section-label mb-1.5">What changed</div>
                <pre className="mono max-h-80 overflow-auto whitespace-pre-wrap rounded-lg p-3 text-[11px]" style={{ background: "var(--surface-2)" }}>
                  {open.diff.join("\n")}
                </pre>
              </section>
            ) : null}
            {open.before ? (
              <details>
                <summary className="cursor-pointer text-[12px] text-[color:var(--text-secondary)]">
                  Before
                </summary>
                <pre className="mono mt-2 max-h-72 overflow-auto whitespace-pre-wrap rounded-lg p-3 text-[11px]" style={{ background: "var(--surface-2)" }}>
                  {JSON.stringify(open.before, null, 2)}
                </pre>
              </details>
            ) : null}
            {open.after ? (
              <details>
                <summary className="cursor-pointer text-[12px] text-[color:var(--text-secondary)]">
                  After
                </summary>
                <pre className="mono mt-2 max-h-72 overflow-auto whitespace-pre-wrap rounded-lg p-3 text-[11px]" style={{ background: "var(--surface-2)" }}>
                  {JSON.stringify(open.after, null, 2)}
                </pre>
              </details>
            ) : null}
          </div>
        )}
      </Sheet>
    </>
  );
}
