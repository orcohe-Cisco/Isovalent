export interface Endpoint {
  namespace?: string;
  podName?: string;
  workload?: string;
  identity?: number;
  labels?: string[];
}

export interface L7 {
  type?: string;
  method?: string;
  url?: string;
  protocol?: string;
  status?: number;
  latencyMs?: number;
  headers?: { key: string; value: string }[];
  dnsQuery?: string;
  dnsRcode?: string;
}

export interface Flow {
  time: string;
  verdict: string;
  dropReason?: string;
  direction?: string;
  source: Endpoint;
  destination: Endpoint;
  l4: { protocol?: string; srcPort?: number; dstPort?: number };
  l7?: L7;
  node?: string;
  summary?: string;
}

export interface TetragonEvent {
  time: string;
  type: string;
  namespace?: string;
  pod?: string;
  workload?: string;
  node?: string;
  binary?: string;
  args?: string;
  parent?: string;
  function?: string;
  action?: string;
  policy?: string;
  details?: string;
}

export interface Alert {
  time: string;
  severity: "warning" | "critical";
  kind: string;
  category?: "network" | "runtime";
  verdict?: "blocked" | "killed" | "monitored";
  engine?: "cilium" | "tetragon";
  title: string;
  detail?: string;
  rule?: string;
  event?: string;
  namespace?: string;
  workload?: string;
  policy?: string;
}

export interface AppConfig {
  cluster: string;
  mode: string;
  hubbleUiUrl?: string;
  grafanaUrl?: string;
  grafanaDashboardUid?: string;
  retentionDays?: number;
  gitops?: { enabled: boolean; repo: string };
}

export interface TimePoint {
  t: number;
  flows: number;
  drops: number;
  httpReq: number;
  httpErr: number;
  dnsErr: number;
  kills: number;
}

export interface Overview {
  totalFlows: number;
  totalDrops: number;
  totalEvents: number;
  totalKills: number;
  flowRate: number;
  dropRate: number;
  httpErrPct: number;
  dnsErrors: number;
  series: TimePoint[];
}

export interface OverviewResponse {
  cluster: string;
  mode: string;
  overview: Overview;
  alerts: Alert[];
}

export interface ServiceMapNode {
  id: string;
  namespace?: string;
  workload: string;
  external: boolean;
  drops: number;
  kills: number;
}

export interface ServiceMapEdge {
  source: string;
  target: string;
  forwarded: number;
  dropped: number;
  http: boolean;
  dns: boolean;
  ports: number[];
}

export interface Policy {
  kind: string;
  namespace?: string;
  name: string;
  created?: string;
  manifest: Record<string, unknown>;
}

export interface TracingPolicyInfo {
  name: string;
  namespace?: string;
  kind: string;
  category?: string;
  description?: string;
  action: "monitor" | "enforce";
  hooks?: string[];
  managed: boolean;
}

export interface AlertRoute {
  id: string;
  name: string;
  type: "slack" | "webhook" | "pagerduty" | "splunk";
  url: string;
  token?: string;
  minSeverity: "warning" | "critical";
  kinds?: string[];
  enabled: boolean;
}

export interface DryRunVerdict {
  flow: Flow;
  applies: boolean;
  allowed: boolean;
  reason?: string;
}

export interface DryRunResult {
  total: number;
  applied: number;
  allowed: number;
  blocked: number;
  verdicts: DryRunVerdict[];
  policyError?: string;
}

export interface HistoryRecord {
  time: string;
  payload: unknown;
}

