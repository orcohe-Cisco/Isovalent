"use client";

import { useMemo, useState } from "react";
import { usePoll } from "@/lib/usePoll";
import type { NamespaceInfo } from "@/lib/types";
import { Chip } from "@/components/ui/Chip";

/**
 * A namespace picker rather than a text field.
 *
 * Typing a namespace name is how you write a policy that selects nothing and
 * looks like it worked. Protected namespaces are listed but not selectable,
 * with the reason attached — hiding them would just make the eventual rejection
 * confusing.
 */
export function NamespacePicker({
  value,
  onChange,
  allowCluster = true,
}: {
  value: string;
  onChange: (ns: string) => void;
  allowCluster?: boolean;
}) {
  const { data } = usePoll<NamespaceInfo[]>("/api/v1/namespaces", 60000, []);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");

  const list = useMemo(() => {
    const q = query.trim().toLowerCase();
    return (data ?? []).filter((n) => !q || n.name.toLowerCase().includes(q));
  }, [data, query]);

  return (
    <div className="relative inline-block">
      <button
        onClick={() => setOpen((v) => !v)}
        className="btn btn-secondary !py-1.5 !text-[12px]"
        title="Change namespace"
      >
        <span className="mono">{value || "cluster-wide"}</span>
        <span className="text-[color:var(--text-tertiary)]">▾</span>
      </button>
      {open && (
        <>
          <div className="fixed inset-0 z-20" onClick={() => setOpen(false)} />
          <div
            className="absolute z-30 mt-1 w-72 overflow-hidden rounded-xl"
            style={{
              background: "var(--surface-1)",
              border: "1px solid var(--hairline-strong)",
              boxShadow: "var(--shadow-3)",
            }}
          >
            <input
              autoFocus
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Filter namespaces…"
              className="w-full bg-transparent px-3 py-2 text-[12px] outline-none"
              style={{ borderBottom: "1px solid var(--hairline)" }}
            />
            <ul className="max-h-64 overflow-y-auto py-1">
              {allowCluster && (
                <li>
                  <button
                    className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-[12px] hover:bg-[color:var(--surface-3)]"
                    onClick={() => {
                      onChange("");
                      setOpen(false);
                    }}
                  >
                    <span className="mono flex-1">cluster-wide</span>
                    <Chip>no namespace</Chip>
                  </button>
                </li>
              )}
              {list.map((n) => (
                <li key={n.name}>
                  <button
                    disabled={n.protected}
                    className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-[12px] hover:bg-[color:var(--surface-3)] disabled:cursor-not-allowed disabled:opacity-50"
                    title={
                      n.protected
                        ? "Protected: policies written here cannot target the console's own components"
                        : undefined
                    }
                    onClick={() => {
                      onChange(n.name);
                      setOpen(false);
                    }}
                  >
                    <span className="mono flex-1 truncate">{n.name}</span>
                    {n.protected && <Chip tone="warn">protected</Chip>}
                  </button>
                </li>
              ))}
              {list.length === 0 && (
                <li className="px-3 py-2 text-[11px] text-[color:var(--text-tertiary)]">
                  No namespace matches. If this list is empty entirely, the console cannot read the
                  Kubernetes API — check Diagnostics.
                </li>
              )}
            </ul>
          </div>
        </>
      )}
    </div>
  );
}
