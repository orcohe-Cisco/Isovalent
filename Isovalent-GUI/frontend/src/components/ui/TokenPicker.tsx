"use client";

import { useMemo, useState } from "react";
import { Chip } from "@/components/ui/Chip";
import { sinceLabel, type ObservedValue } from "@/lib/observed";

/**
 * Multi-select over values the platform has already seen, with manual entry as
 * an escape hatch. Selected values become chips; the list below shows what is
 * available, how often it has been seen and how recently — which is the signal
 * that tells you whether an exclusion is still relevant or a leftover from a
 * workload that was deleted last month.
 */
export function TokenPicker({
  label,
  hint,
  placeholder,
  options,
  selected,
  onChange,
  emptyNote,
  maxVisible = 8,
}: {
  label: string;
  hint?: string;
  placeholder?: string;
  options: ObservedValue[];
  selected: string[];
  onChange: (next: string[]) => void;
  emptyNote?: string;
  maxVisible?: number;
}) {
  const [query, setQuery] = useState("");
  const [showAll, setShowAll] = useState(false);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    const available = options.filter((o) => !selected.includes(o.value));
    if (!q) return available;
    return available.filter(
      (o) =>
        o.value.toLowerCase().includes(q) ||
        (o.context ?? "").toLowerCase().includes(q),
    );
  }, [options, selected, query]);

  const visible = showAll ? filtered : filtered.slice(0, maxVisible);
  const trimmedQuery = query.trim();
  const canAddManual =
    trimmedQuery.length > 0 &&
    !selected.includes(trimmedQuery) &&
    !options.some((o) => o.value === trimmedQuery);

  const add = (value: string) => {
    onChange([...selected, value]);
    setQuery("");
  };

  return (
    <div className="space-y-2">
      <div className="flex items-baseline justify-between gap-3">
        <span className="section-label">{label}</span>
        {selected.length > 0 && (
          <button
            onClick={() => onChange([])}
            className="btn btn-ghost px-1.5 py-0.5 text-[11px]"
          >
            Clear {selected.length}
          </button>
        )}
      </div>
      {hint && (
        <p className="text-[11px] leading-relaxed text-[color:var(--text-tertiary)]">
          {hint}
        </p>
      )}

      {selected.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {selected.map((v) => (
            <button
              key={v}
              onClick={() => onChange(selected.filter((x) => x !== v))}
              className="mono inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-[11px] transition-colors duration-150"
              style={{
                background: "rgba(57,135,229,0.16)",
                border: "1px solid rgba(57,135,229,0.4)",
                color: "#8fbdf3",
              }}
              title="Remove"
            >
              {v}
              <span style={{ opacity: 0.6 }}>✕</span>
            </button>
          ))}
        </div>
      )}

      <div className="flex gap-1.5">
        <input
          className="field mono flex-1 py-1.5 text-xs"
          placeholder={placeholder ?? "Search or type to add…"}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && canAddManual) {
              e.preventDefault();
              add(trimmedQuery);
            }
          }}
        />
        {canAddManual && (
          <button className="btn btn-secondary px-2.5 py-1.5 text-xs" onClick={() => add(trimmedQuery)}>
            Add
          </button>
        )}
      </div>

      {options.length === 0 && emptyNote && (
        <p className="text-[11px] text-[color:var(--text-tertiary)]">{emptyNote}</p>
      )}

      {visible.length > 0 && (
        <ul className="space-y-0.5">
          {visible.map((o) => (
            <li key={o.value}>
              <button
                onClick={() => add(o.value)}
                className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left transition-colors duration-150 hover:bg-[color:var(--surface-3)]"
              >
                <span className="mono min-w-0 flex-1 truncate text-[11px]">
                  {o.value}
                </span>
                {o.context && (
                  <span className="hidden truncate text-[10px] text-[color:var(--text-tertiary)] sm:block">
                    {o.context}
                  </span>
                )}
                <Chip>{o.count.toLocaleString()}×</Chip>
                <span className="w-14 shrink-0 text-right text-[10px] text-[color:var(--text-tertiary)]">
                  {sinceLabel(o.lastSeen)}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}

      {filtered.length > maxVisible && (
        <button
          onClick={() => setShowAll((v) => !v)}
          className="btn btn-ghost px-1.5 py-0.5 text-[11px]"
        >
          {showAll ? "Show fewer" : `Show all ${filtered.length}`}
        </button>
      )}
    </div>
  );
}
