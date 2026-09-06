/**
 * The navigation model.
 *
 * Kept as data rather than markup so the sidebar, the command palette and the
 * page headers all describe the same thing. Four groups, in the order the work
 * actually happens: look at what is happening, write a rule, find out why
 * something fired, then keep the thing running.
 */
export interface NavItem {
  href: string;
  label: string;
  icon: string;
  /** One line, shown in the palette and as the sidebar tooltip. */
  hint: string;
  /** Feature flag from /api/v1/config that must be true for this to appear. */
  requires?: "hubbleUI" | "grafana" | "ai";
  keywords?: string[];
}

export interface NavGroup {
  label: string;
  items: NavItem[];
}

export const NAV: NavGroup[] = [
  {
    label: "Monitor",
    items: [
      { href: "/", label: "Overview", icon: "◧", hint: "Cluster posture at a glance", keywords: ["home", "dashboard"] },
      { href: "/map", label: "Service Map", icon: "◉", hint: "The Hubble UI, embedded", requires: "hubbleUI", keywords: ["hubble", "graph", "topology"] },
      { href: "/flows", label: "Flows", icon: "≋", hint: "Live network flows", keywords: ["hubble", "traffic", "l7"] },
      { href: "/runtime", label: "Runtime", icon: "⬡", hint: "Live Tetragon process events", keywords: ["tetragon", "process", "exec"] },
      { href: "/dashboards", label: "Dashboards", icon: "▦", hint: "Grafana, embedded", requires: "grafana", keywords: ["grafana", "metrics", "prometheus"] },
    ],
  },
  {
    label: "Protect",
    items: [
      { href: "/create", label: "Create Policy", icon: "✦", hint: "Author a network or runtime policy, with a dry run", keywords: ["new", "network", "tetragon", "cilium", "editor"] },
      { href: "/library", label: "Policy Library", icon: "⛊", hint: "Curated Tetragon policies, ready to apply", keywords: ["templates", "catalog"] },
      { href: "/rules", label: "Active Policies", icon: "⛨", hint: "What is applied, and in which mode", keywords: ["enforce", "monitor", "list"] },
      { href: "/exclusions", label: "Exclusions", icon: "⊘", hint: "What is firing each policy, and how to quieten it", keywords: ["hits", "noise", "tuning", "false positive"] },
    ],
  },
  {
    label: "Investigate",
    items: [
      { href: "/investigate", label: "Investigation", icon: "◷", hint: "Search everything Cilium and Tetragon recorded", keywords: ["search", "timeline", "forensics", "logs", "history"] },
      { href: "/assistant", label: "Assistant", icon: "✧", hint: "Ask an external model to review the posture", requires: "ai", keywords: ["ai", "claude", "gemini", "suggest"] },
    ],
  },
  {
    label: "Operate",
    items: [
      { href: "/alerts", label: "Integrations", icon: "◈", hint: "Where alerts go: Slack, Teams, Webex, syslog, SIEM", keywords: ["slack", "webhook", "splunk", "siem", "notify", "alerting"] },
      { href: "/diagnostics", label: "Diagnostics", icon: "◎", hint: "Connectivity checks and console logs", keywords: ["health", "logs", "debug", "broken"] },
      { href: "/audit", label: "Audit", icon: "▤", hint: "Who changed what, and when", keywords: ["history", "compliance", "changes"] },
      { href: "/deploy", label: "Deployment", icon: "⌘", hint: "Commands to deploy Cilium, Tetragon and this console", keywords: ["install", "helm", "aks", "eks", "gke", "kind"] },
      { href: "/api", label: "API", icon: "⧉", hint: "Tokens and the full API reference", keywords: ["tokens", "openapi", "docs", "integrate"] },
    ],
  },
];

/** Flattened, for the command palette and breadcrumb lookups. */
export const NAV_ITEMS: NavItem[] = NAV.flatMap((g) => g.items);

export function findNavItem(pathname: string): NavItem | undefined {
  if (pathname === "/") return NAV_ITEMS[0];
  return NAV_ITEMS.filter((i) => i.href !== "/").find((i) => pathname.startsWith(i.href));
}
