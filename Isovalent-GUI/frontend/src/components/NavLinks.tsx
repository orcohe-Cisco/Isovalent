"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useConfig } from "@/lib/config";
import { NAV, type NavItem } from "@/components/nav";

/**
 * The sidebar.
 *
 * Two rules, both borrowed from software that gets this right: never show a
 * destination that cannot work (an entry whose integration is missing is
 * hidden, not greyed out and certainly not a dead end), and give the current
 * location one unambiguous marker rather than three competing ones.
 */
export function NavLinks() {
  const pathname = usePathname();
  const { config } = useConfig();

  const enabled = (item: NavItem) => {
    switch (item.requires) {
      case "hubbleUI":
        return config.features.hubbleUI;
      case "grafana":
        return config.features.grafana;
      case "ai":
        return config.features.ai.enabled;
      default:
        return true;
    }
  };

  return (
    <nav className="flex flex-col gap-5 px-3">
      {NAV.map((group) => {
        const items = group.items.filter(enabled);
        if (!items.length) return null;
        return (
          <div key={group.label}>
            <div className="section-label px-3 pb-1.5">{group.label}</div>
            <div className="flex flex-col gap-0.5">
              {items.map((l) => {
                const active =
                  l.href === "/" ? pathname === "/" : pathname.startsWith(l.href);
                return (
                  <Link
                    key={l.href}
                    href={l.href}
                    title={l.hint}
                    aria-current={active ? "page" : undefined}
                    className="group relative flex items-center gap-3 rounded-lg px-3 py-[7px] text-[13px]"
                    style={{
                      color: active ? "var(--text-primary)" : "var(--text-secondary)",
                      background: active ? "var(--surface-3)" : "transparent",
                      transition:
                        "background-color var(--dur-fast) var(--ease-out), color var(--dur-fast) var(--ease-out)",
                    }}
                  >
                    {active && (
                      <span
                        aria-hidden
                        className="absolute left-0 top-1/2 h-4 w-[3px] -translate-y-1/2 rounded-r-full"
                        style={{ background: "var(--accent)" }}
                      />
                    )}
                    <span
                      className="w-4 text-center text-[11px]"
                      style={{ color: active ? "var(--accent)" : "var(--text-tertiary)" }}
                    >
                      {l.icon}
                    </span>
                    {l.label}
                  </Link>
                );
              })}
            </div>
          </div>
        );
      })}
    </nav>
  );
}
