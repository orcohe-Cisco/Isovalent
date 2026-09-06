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
  status?: number;
  latencyMs?: number;
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
  argList?: string[];
  policyNamespace?: string;
  user?: string;
  uid?: number;
  container?: string;
  labels?: Record<string, string>;
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
  title: string;
  detail?: string;
  namespace?: string;
  workload?: string;
  policy?: string;
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
  type:
    | "slack"
    | "teams"
    | "webex"
    | "webhook"
    | "syslog"
    | "splunk"
    | "elastic"
    | "sentinel"
    | "pagerduty";
  url: string;
  token?: string;
  /** Webex roomId, or the syslog facility. */
  target?: string;
  /** syslog only: rfc5424 | cef. */
  format?: string;
  minSeverity: "warning" | "critical";
  kinds?: string[];
  namespaces?: string[];
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


/* ------------------------------------------------------- exclusions tab --- */

/** One distinct value that fired a policy. */
export interface HitValue {
  value: string;
  count: number;
  enforced: number;
  firstSeen: string;
  lastSeen: string;
  /** Hook argument index a path came from; a NotPrefix on the wrong index matches nothing. */
  argIndex: number;
  excludable: boolean;
}

export interface HitRow {
  policy: string;
  namespace?: string;
  total: number;
  enforced: number;
  unique: Record<string, number>;
  firstSeen: string;
  lastSeen: string;
  topBinary?: string;
  ratePerMin: number;
  /* joined from the cluster */
  kind?: string;
  action?: "monitor" | "enforce" | "";
  category?: string;
  hooks?: string[];
  managed?: boolean;
  present?: boolean;
}

export interface HitsResponse {
  policies: HitRow[];
  dimensions: string[];
  excludable: string[];
}

export interface MatchClause {
  path: string;
  kind: string;
  operator?: string;
  values?: string[];
  matched?: string;
  excludable?: string;
  argIndex?: number;
}

export interface Explanation {
  policy: string;
  namespace?: string;
  kind: string;
  hook?: string;
  action?: string;
  clauses: MatchClause[];
  summary: string;
  manifest?: Record<string, unknown>;
  confident: boolean;
}

export interface HitDetailResponse {
  detail: {
    policy: string;
    namespace?: string;
    total: number;
    enforced: number;
    unique: Record<string, number>;
    firstSeen: string;
    lastSeen: string;
    topBinary?: string;
    ratePerMin: number;
    values: Record<string, HitValue[]>;
    samples: TetragonEvent[];
  };
  kind: string;
  manifest?: Record<string, unknown>;
  info?: TracingPolicyInfo;
  explain?: Explanation;
  policyError?: string;
  /** False when the policy that produced this hit history no longer exists in the cluster. */
  present?: boolean;
}

/** The five dimensions Tetragon can actually express in a policy. */
export interface ExclusionRequest {
  binaries?: string[];
  parentBinaries?: string[];
  paths?: string[];
  pathArgIndex?: number;
  podLabels?: string[];
  containers?: string[];
}

export interface ExclusionNote {
  dimension: string;
  applied: boolean;
  detail: string;
}

/* --------------------------------------------------------- investigation --- */

export interface InvestigateRow {
  id: string;
  time: string;
  source: "cilium" | "tetragon";
  verdict: string;
  blocked: boolean;
  namespace?: string;
  src?: string;
  dst?: string;
  node?: string;
  workload?: string;
  pod?: string;
  protocol?: string;
  port?: number;
  l7Type?: string;
  l7Method?: string;
  l7Path?: string;
  l7Status?: number;
  dnsQuery?: string;
  binary?: string;
  args?: string;
  user?: string;
  container?: string;
  hook?: string;
  action?: string;
  policy?: string;
  policyNamespace?: string;
  policyKind?: string;
  reason?: string;
  summary: string;
  isReply?: boolean;
  raw?: unknown;
}

export interface Facet {
  value: string;
  count: number;
}

export interface InvestigateResult {
  rows: InvestigateRow[];
  total: number;
  scanned: number;
  truncated: boolean;
  facets: Record<string, Facet[]>;
  series: { t: number; total: number; blocked: number }[];
  window: { since: string; until: string; label?: string };
}

/* ------------------------------------------------------------- operations --- */

export interface AuditEntry {
  id: number;
  time: string;
  actor: string;
  roles?: string[];
  sourceIP?: string;
  action: string;
  target?: string;
  outcome: "success" | "denied" | "error";
  message?: string;
  diff?: string[];
  before?: unknown;
  after?: unknown;
}

export interface LogLine {
  time: string;
  level: string;
  origin: "backend" | "frontend";
  message: string;
  fields?: Record<string, string>;
}

export interface DiagnosticCheck {
  id: string;
  name: string;
  status: "ok" | "degraded" | "down" | "disabled";
  latencyMs?: number;
  detail?: string;
  hint?: string;
  target?: string;
}

export interface Diagnostics {
  status: string;
  checks: DiagnosticCheck[];
  checkedAt: string;
  logCounts: Record<string, number>;
  uptimeSeconds: number;
}

export interface ApiKey {
  id: string;
  name: string;
  role: string;
  prefix: string;
  created: string;
  lastUsed?: string;
  uses: number;
  createdBy?: string;
  static?: boolean;
  expires?: string;
}

export interface NamespaceInfo {
  name: string;
  phase?: string;
  labels?: Record<string, string>;
  protected: boolean;
}

/* --------------------------------------------------------------- assistant --- */

export interface AiSuggestion {
  kind: "enforce" | "monitor" | "exclude" | "disable" | "tune" | "investigate";
  policy: string;
  namespace?: string;
  title: string;
  rationale: string;
  confidence?: string;
  risk?: string;
  exclusions?: ExclusionRequest;
}

export interface AiResponse {
  summary: string;
  suggestions: AiSuggestion[];
  raw?: string;
  model: string;
  provider: string;
}

/* --------------------------------------------------------------- dry runs --- */

export interface TracingDryRun {
  total: number;
  matched: number;
  wouldEnforce: number;
  enforcing: boolean;
  byBinary: { value: string; count: number }[];
  byNamespace: { value: string; count: number }[];
  byWorkload: { value: string; count: number }[];
  byHook: { value: string; count: number }[];
  samples: { event: TetragonEvent; explain: Explanation }[];
  window: { since: string; until: string };
  policyError?: string;
  advice?: string;
}
