"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

const links = [
  { href: "/", label: "Overview", icon: "◧" },
  { href: "/network", label: "Service Map", icon: "◉" },
  { href: "/flows", label: "Flows", icon: "≋" },
  { href: "/runtime", label: "Runtime Security", icon: "⬡" },
  { href: "/rules", label: "Runtime Policies", icon: "⛨" },
  { href: "/policies", label: "Policy Management", icon: "▤" },
  { href: "/alerts", label: "Security Events", icon: "◈" },
  { href: "/dashboards", label: "Dashboards", icon: "▦" },
  { href: "/history", label: "History", icon: "◷" },
];

export function NavLinks() {
  const pathname = usePathname();
  return (
    <nav className="flex flex-col gap-1 px-3">
      {links.map((l) => {
        const active =
          l.href === "/" ? pathname === "/" : pathname.startsWith(l.href);
        return (
          <Link
            key={l.href}
            href={l.href}
            className={`flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors ${
              active
                ? "bg-neutral-800 text-white"
                : "text-neutral-400 hover:bg-neutral-800/60 hover:text-neutral-200"
            }`}
          >
            <span className="w-4 text-center text-xs">{l.icon}</span>
            {l.label}
          </Link>
        );
      })}
    </nav>
  );
}
