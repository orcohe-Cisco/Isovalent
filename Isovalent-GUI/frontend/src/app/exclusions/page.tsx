"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { usePoll } from "@/lib/usePoll";
import { sinceLabel } from "@/lib/observed";
import type { HitDetailResponse, HitRow, HitsResponse } from "@/lib/types";
import { PageHeader } from "@/components/ui/PageHeader";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorNote } from "@/components/ui/ErrorNote";
import { Chip } from "@/components/ui/Chip";
import { Sheet } from "@/components/ui/Sheet";
import { PolicyHitDetail } from "@/components/exclusions/PolicyHitDetail";

type Filter = "firing" | "quiet" | "all";

/**
 * Exclusions.
 *
 * Deliberately separate from policy creation. You cannot know what to exclude
 * at the moment you write a policy — you find out days later, when it has
 * fired four thousand times on your log shipper. So this page starts from what
 * actually happened and works backwards to the rule.
 */
export default function ExclusionsPage() {
  const { data, error, loading, reload } = usePoll<HitsResponse>("/api/v1/hits", 10000);
  const [filter, setFilter] = useState<Filter>("firing");
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState<HitRow | null>(null);

  const rows = useMemo(() => {
    const all = data?.policies ?? [];
    const q = query.trim().toLowerCase();
    return all.filter((r) => {
      if (filter === "firing" && r.total === 0) return false;
      if (filter === "quiet" && r.total > 0) return false;
      if (!q) return true;
      return [r.policy, r.namespace, r.topBinary, r.category].join(" ").toLowerCase().includes(q);
    });
  }, [data, filter, query]);

  const totals = useMemo(() => {
    const all = data?.policies ?? [];
    return {
      policies: all.length,
      firing: all.filter((r) => r.total > 0).length,
      hits: all.reduce((n, r) => n + r.total, 0),
      enforced: all.reduce((n, r) => n + r.enforced, 0),
    };
  }, [data]);

  const detailPath = open
    ? `/api/v1/hits/${encodeURIComponent(open.namespace || "-")}/${encodeURIComponent(open.policy)}`
    : null;
  const detail = usePoll<HitDetailResponse>(detailPath, 0);

  return (
    <>
      <PageHeader
        title="Exclusions"
        subtitle="Every runtime policy, what it has actually matched, and the exclusions that would quieten it. Read from live Tetragon events; written back into the policy YAML."
        actions={
          <>
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Filter policies…"
              className="field !w-56 !py-1.5"
            />
            <div className="flex gap-1">
              {(["firing", "quiet", "all"] as Filter[]).map((f) => (
                <button
                  key={f}
                  onClick={() => setFilter(f)}
                  className={`btn ${filter === f ? "btn-secondary" : "btn-ghost"} !px-2.5 !py-1.5 !text-[12px]`}
                >
                  {f === "firing" ? "Firing" : f === "quiet" ? "Silent" : "All"}
                </button>
              ))}
            </div>
          </>
        }
      />

      <ErrorNote error={error} />

      <div className="mb-4 flex flex-wrap gap-2 text-[12px] text-[color:var(--text-secondary)]">
        <Chip mono>{totals.policies} policies</Chip>
        <Chip tone="accent" mono>{totals.firing} firing</Chip>
        <Chip mono>{totals.hits.toLocaleString()} events attributed</Chip>
        {totals.enforced > 0 && (
          <Chip tone="danger" mono>{totals.enforced.toLocaleString()} enforcement actions</Chip>
        )}
      </div>

      {!loading && rows.length === 0 ? (
        <EmptyState
          title={filter === "firing" ? "Nothing is firing" : "No policies match"}
          body={
            filter === "firing" ? (
              <p>
                No TracingPolicy has produced an event since the console started. That is either a
                very quiet cluster or a policy set that is not selecting anything — switch to{" "}
                <button className="underline" onClick={() => setFilter("all")}>
                  All
                </button>{" "}
                to see what is installed.
              </p>
            ) : (
              <p>
                Nothing matches that filter. Apply something from the{" "}
                <Link href="/library" className="underline">
                  Policy Library
                </Link>{" "}
                to get started.
              </p>
            )
          }
        />
      ) : (
        <div className="panel overflow-hidden">
          <table className="w-full text-[13px]">
            <thead>
              <tr className="section-label">
                <th className="px-4 py-2.5 text-left font-medium">Policy</th>
                <th className="px-3 py-2.5 text-left font-medium">Mode</th>
                <th className="px-3 py-2.5 text-right font-medium">Hits</th>
                <th className="px-3 py-2.5 text-right font-medium">Rate</th>
                <th className="px-3 py-2.5 text-left font-medium">Loudest process</th>
                <th className="px-3 py-2.5 text-right font-medium">Last seen</th>
                <th className="px-4 py-2.5" />
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr
                  key={`${r.namespace ?? "-"}/${r.policy}`}
                  className="hairline-t cursor-pointer transition-colors hover:bg-[color:var(--surface-2)]"
                  onClick={() => setOpen(r)}
                >
                  <td className="px-4 py-2.5">
                    <div className="font-medium">{r.policy}</div>
                    <div className="text-[11px] text-[color:var(--text-tertiary)]">
                      {r.namespace ? `namespace ${r.namespace}` : "cluster-wide"}
                      {r.present === false && " · not found in the cluster"}
                    </div>
                  </td>
                  <td className="px-3 py-2.5">
                    {r.action ? (
                      <Chip tone={r.action === "enforce" ? "danger" : "accent"}>{r.action}</Chip>
                    ) : (
                      <Chip>unknown</Chip>
                    )}
                  </td>
                  <td className="mono px-3 py-2.5 text-right">
                    {r.total.toLocaleString()}
                    {r.enforced > 0 && (
                      <span className="ml-1.5 text-[11px]" style={{ color: "#f0a3a3" }}>
                        ({r.enforced.toLocaleString()} killed)
                      </span>
                    )}
                  </td>
                  <td className="mono px-3 py-2.5 text-right text-[color:var(--text-secondary)]">
                    {r.total > 0 ? `${r.ratePerMin.toFixed(1)}/min` : "—"}
                  </td>
                  <td className="mono px-3 py-2.5 text-[12px] text-[color:var(--text-secondary)]">
                    {r.topBinary ?? "—"}
                  </td>
                  <td className="px-3 py-2.5 text-right text-[11px] text-[color:var(--text-tertiary)]">
                    {r.lastSeen && r.total > 0 ? sinceLabel(r.lastSeen) : "—"}
                  </td>
                  <td className="px-4 py-2.5 text-right text-[color:var(--text-tertiary)]">›</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <Sheet
        open={Boolean(open)}
        onClose={() => setOpen(null)}
        width={900}
        title={open?.policy ?? ""}
        subtitle={open?.namespace ? `namespace ${open.namespace}` : "cluster-wide TracingPolicy"}
      >
        {detail.data ? (
          <PolicyHitDetail
            data={detail.data}
            onApplied={() => {
              void detail.reload();
              void reload();
            }}
            onReset={() => {
              void detail.reload();
              void reload();
            }}
          />
        ) : (
          <p className="text-[13px] text-[color:var(--text-tertiary)]">
            {detail.error ?? "Loading…"}
          </p>
        )}
      </Sheet>
    </>
  );
}
