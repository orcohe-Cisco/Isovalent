"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { NAV_ITEMS, type NavItem } from "@/components/nav";
import { useConfig } from "@/lib/config";

/**
 * ⌘K.
 *
 * Once a console has fifteen destinations, the menu stops being how people
 * navigate it. Typing three letters is faster than reading a sidebar, and it
 * lets the labels stay short without the product becoming a memory test —
 * every item carries keywords, so "false positive" finds Exclusions.
 */
export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [cursor, setCursor] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const router = useRouter();
  const { config } = useConfig();

  const available = useMemo(
    () =>
      NAV_ITEMS.filter((i) => {
        switch (i.requires) {
          case "hubbleUI":
            return config.features.hubbleUI;
          case "grafana":
            return config.features.grafana;
          case "ai":
            return config.features.ai.enabled;
          default:
            return true;
        }
      }),
    [config],
  );

  const results = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return available;
    return available.filter((i) =>
      [i.label, i.hint, ...(i.keywords ?? [])].join(" ").toLowerCase().includes(q),
    );
  }, [query, available]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen((v) => !v);
        setQuery("");
        setCursor(0);
      }
      if (e.key === "Escape") setOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  useEffect(() => {
    if (open) setTimeout(() => inputRef.current?.focus(), 10);
  }, [open]);

  const go = useCallback(
    (item: NavItem) => {
      setOpen(false);
      router.push(item.href);
    },
    [router],
  );

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center px-4 pt-[12vh] animate-fade"
      style={{ background: "rgba(0,0,0,0.55)", backdropFilter: "blur(3px)" }}
      onClick={() => setOpen(false)}
      role="dialog"
      aria-modal="true"
      aria-label="Command palette"
    >
      <div
        className="w-full max-w-lg overflow-hidden rounded-xl animate-fade-up"
        style={{
          background: "var(--surface-1)",
          border: "1px solid var(--hairline-strong)",
          boxShadow: "var(--shadow-3)",
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <input
          ref={inputRef}
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setCursor(0);
          }}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown") {
              e.preventDefault();
              setCursor((c) => Math.min(c + 1, results.length - 1));
            }
            if (e.key === "ArrowUp") {
              e.preventDefault();
              setCursor((c) => Math.max(c - 1, 0));
            }
            if (e.key === "Enter" && results[cursor]) go(results[cursor]);
          }}
          placeholder="Go to…"
          className="w-full bg-transparent px-4 py-3.5 text-sm outline-none"
          style={{ borderBottom: "1px solid var(--hairline)" }}
        />
        <ul className="max-h-80 overflow-y-auto py-1">
          {results.length === 0 && (
            <li className="px-4 py-3 text-[13px] text-[color:var(--text-tertiary)]">
              Nothing matches “{query}”.
            </li>
          )}
          {results.map((item, i) => (
            <li key={item.href}>
              <button
                onMouseEnter={() => setCursor(i)}
                onClick={() => go(item)}
                className="flex w-full items-center gap-3 px-4 py-2 text-left"
                style={{ background: i === cursor ? "var(--surface-3)" : "transparent" }}
              >
                <span className="w-4 text-center text-[11px] text-[color:var(--text-tertiary)]">
                  {item.icon}
                </span>
                <span className="text-[13px]">{item.label}</span>
                <span className="ml-auto truncate pl-4 text-[11px] text-[color:var(--text-tertiary)]">
                  {item.hint}
                </span>
              </button>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
