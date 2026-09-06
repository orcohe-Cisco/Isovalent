package server

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/isovalent-control/isovalent-control/backend/internal/alerts"
	"github.com/isovalent-control/isovalent-control/backend/internal/hits"
	"github.com/isovalent-control/isovalent-control/backend/internal/hubble"
	"github.com/isovalent-control/isovalent-control/backend/internal/store"
	"github.com/isovalent-control/isovalent-control/backend/internal/stream"
	"github.com/isovalent-control/isovalent-control/backend/internal/tetragon"
)

const (
	flowRingSize  = 500
	eventRingSize = 300
	alertRingSize = 200
	bucketSeconds = 10
	bucketCount   = 90 // 15 minutes of history
)

// Alert is a security-relevant occurrence surfaced to the UI and (later)
// routed to external sinks.
type Alert struct {
	Time      time.Time `json:"time"`
	Severity  string    `json:"severity"` // warning | critical
	Kind      string    `json:"kind"`     // network_drop | runtime_enforcement | http_error
	Title     string    `json:"title"`
	Detail    string    `json:"detail,omitempty"`
	Namespace string    `json:"namespace,omitempty"`
	Workload  string    `json:"workload,omitempty"`
	Policy    string    `json:"policy,omitempty"`
}

type bucket struct {
	start   int64
	Flows   int64 `json:"flows"`
	Drops   int64 `json:"drops"`
	HTTPReq int64 `json:"httpReq"`
	HTTPErr int64 `json:"httpErr"`
	DNSErr  int64 `json:"dnsErr"`
	Kills   int64 `json:"kills"`
}

type edgeStat struct {
	Source    string          `json:"source"`
	Target    string          `json:"target"`
	Forwarded int64           `json:"forwarded"`
	Dropped   int64           `json:"dropped"`
	HTTP      bool            `json:"http"`
	DNS       bool            `json:"dns"`
	Ports     map[uint32]bool `json:"-"`
}

type nodeStat struct {
	ID        string `json:"id"`
	Namespace string `json:"namespace"`
	Workload  string `json:"workload"`
	External  bool   `json:"external"`
	Drops     int64  `json:"drops"`
	Kills     int64  `json:"kills"`
}

// Aggregator consumes flow/event streams, maintains rolling state for the
// dashboards and republishes everything to the WebSocket hub.
type Aggregator struct {
	hub *stream.Hub

	mu      sync.RWMutex
	flows   []hubble.Flow
	events  []tetragon.Event
	alerts  []Alert
	edges   map[string]*edgeStat
	nodes   map[string]*nodeStat
	buckets [bucketCount]bucket

	totalFlows  int64
	totalDrops  int64
	totalEvents int64
	totalKills  int64

	lastAlert map[string]time.Time // naive suppression

	store  store.Store    // historical persistence
	router *alerts.Router // external alert routing (optional)
	hits   *hits.Tracker  // per-policy hit accounting for the Exclusions tab

	// observed keeps the distinct namespaces/workloads/binaries/users the
	// platform has seen, so the UI can offer exclusions from real data
	// instead of a free-text box. Independently locked.
	observed *ObservedCatalog
}

// NewAggregator returns an empty aggregator publishing to hub.
func NewAggregator(hub *stream.Hub) *Aggregator {
	return &Aggregator{
		hub:       hub,
		edges:     map[string]*edgeStat{},
		nodes:     map[string]*nodeStat{},
		lastAlert: map[string]time.Time{},
		observed:  NewObservedCatalog(),
	}
}

// SetStore attaches a historical store; flows/events/alerts are persisted to it.
func (a *Aggregator) SetStore(s store.Store) { a.store = s }

// SetRouter attaches the external alert router.
func (a *Aggregator) SetRouter(r *alerts.Router) { a.router = r }

// SetHits attaches the per-policy hit tracker.
func (a *Aggregator) SetHits(t *hits.Tracker) { a.hits = t }

// Run consumes both channels until ctx is cancelled.
func (a *Aggregator) Run(ctx context.Context, flows <-chan hubble.Flow, events <-chan tetragon.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case f, ok := <-flows:
			if !ok {
				flows = nil
				continue
			}
			a.ingestFlow(f)
		case e, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			a.ingestEvent(e)
		}
		if flows == nil && events == nil {
			return
		}
	}
}

func endpointID(e hubble.Endpoint) string {
	if e.Namespace == "" {
		if e.Workload != "" {
			return "external/" + e.Workload
		}
		return "external/world"
	}
	return e.Namespace + "/" + e.Workload
}

func (a *Aggregator) ingestFlow(f hubble.Flow) {
	a.mu.Lock()
	a.totalFlows++
	a.flows = append(a.flows, f)
	if len(a.flows) > flowRingSize {
		a.flows = a.flows[len(a.flows)-flowRingSize:]
	}

	b := a.bucketLocked(f.Time)
	b.Flows++
	if f.Verdict == "DROPPED" {
		a.totalDrops++
		b.Drops++
	}
	if f.L7 != nil {
		switch f.L7.Type {
		case "http":
			b.HTTPReq++
			if f.L7.Status >= 500 {
				b.HTTPErr++
			}
		case "dns":
			if f.L7.DNSRcode != "" && f.L7.DNSRcode != "NoError" {
				b.DNSErr++
			}
		}
	}

	src, dst := endpointID(f.Source), endpointID(f.Destination)
	a.touchNodeLocked(src, f.Source)
	dn := a.touchNodeLocked(dst, f.Destination)
	ek := src + "->" + dst
	es := a.edges[ek]
	if es == nil {
		es = &edgeStat{Source: src, Target: dst, Ports: map[uint32]bool{}}
		a.edges[ek] = es
	}
	es.Ports[f.L4.DstPort] = true
	if f.Verdict == "DROPPED" {
		es.Dropped++
		dn.Drops++
	} else {
		es.Forwarded++
	}
	if f.L7 != nil {
		if f.L7.Type == "http" {
			es.HTTP = true
		}
		if f.L7.Type == "dns" {
			es.DNS = true
		}
	}

	var alert *Alert
	if f.Verdict == "DROPPED" {
		alert = &Alert{
			Time: f.Time, Severity: "warning", Kind: "network_drop",
			Title:     "Traffic dropped: " + src + " → " + dst,
			Detail:    f.DropReason,
			Namespace: f.Source.Namespace, Workload: f.Source.Workload,
		}
		// Name the rule when Cilium attributed one: "dropped" is an
		// observation, "dropped by <policy>" is something you can act on.
		if names := f.PolicyNames(); len(names) > 0 {
			alert.Policy = names[0]
		}
	}
	a.mu.Unlock()

	a.recordObservedFlow(f)
	a.hub.Publish("flows", f)
	if a.store != nil {
		_ = a.store.Save(context.Background(), store.KindFlow, f.Time, f)
	}
	if alert != nil {
		a.publishAlert(*alert)
	}
}

func (a *Aggregator) ingestEvent(e tetragon.Event) {
	a.mu.Lock()
	a.totalEvents++
	a.events = append(a.events, e)
	if len(a.events) > eventRingSize {
		a.events = a.events[len(a.events)-eventRingSize:]
	}
	enforced := e.Action == "SIGKILL" || e.Action == "OVERRIDE"
	if enforced {
		a.totalKills++
		a.bucketLocked(e.Time).Kills++
		if e.Namespace != "" && e.Workload != "" {
			id := e.Namespace + "/" + e.Workload
			if n := a.nodes[id]; n != nil {
				n.Kills++
			}
		}
	}
	a.mu.Unlock()

	a.recordObservedEvent(e)
	if a.hits != nil {
		a.hits.Record(e)
	}
	a.hub.Publish("events", e)
	if a.store != nil {
		_ = a.store.Save(context.Background(), store.KindEvent, e.Time, e)
	}
	if enforced {
		a.publishAlert(Alert{
			Time: e.Time, Severity: "critical", Kind: "runtime_enforcement",
			Title:     "Tetragon " + e.Action + ": " + e.Binary,
			Detail:    e.Function + " " + e.Details,
			Namespace: e.Namespace, Workload: e.Workload, Policy: e.Policy,
		})
	}
}

func (a *Aggregator) publishAlert(al Alert) {
	key := al.Kind + "/" + al.Title
	a.mu.Lock()
	if last, ok := a.lastAlert[key]; ok && al.Time.Sub(last) < 5*time.Second {
		a.mu.Unlock()
		return // suppress duplicates
	}
	a.lastAlert[key] = al.Time
	a.alerts = append(a.alerts, al)
	if len(a.alerts) > alertRingSize {
		a.alerts = a.alerts[len(a.alerts)-alertRingSize:]
	}
	a.mu.Unlock()
	a.hub.Publish("alerts", al)
	if a.store != nil {
		_ = a.store.Save(context.Background(), store.KindAlert, al.Time, al)
	}
	if a.router != nil {
		a.router.Dispatch(alerts.Alert{
			Time: al.Time, Severity: al.Severity, Kind: al.Kind, Title: al.Title,
			Detail: al.Detail, Namespace: al.Namespace, Workload: al.Workload, Policy: al.Policy,
		})
	}
}

// WriteMetrics emits the golden-signal counters in Prometheus text format.
func (a *Aggregator) WriteMetrics(w io.Writer) {
	a.mu.RLock()
	tf, td, te, tk := a.totalFlows, a.totalDrops, a.totalEvents, a.totalKills
	nEdges, nNodes := len(a.edges), len(a.nodes)
	a.mu.RUnlock()

	metric := func(name, help, typ string, val int64) {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n%s %d\n", name, help, name, typ, name, val)
	}
	metric("isovalent_control_flows_total", "Total Hubble flows observed.", "counter", tf)
	metric("isovalent_control_flow_drops_total", "Total flows dropped by policy.", "counter", td)
	metric("isovalent_control_tetragon_events_total", "Total Tetragon events observed.", "counter", te)
	metric("isovalent_control_enforcement_kills_total", "Total Tetragon enforcement actions (SIGKILL/OVERRIDE).", "counter", tk)
	metric("isovalent_control_servicemap_nodes", "Nodes in the service dependency map.", "gauge", int64(nNodes))
	metric("isovalent_control_servicemap_edges", "Edges in the service dependency map.", "gauge", int64(nEdges))
	if a.router != nil {
		for k, v := range a.router.Stats() {
			metric("isovalent_control_alerts_"+k+"_total", "Alert router "+k+" count.", "counter", v)
		}
	}

	// Per-policy hit counts. These are the series that make "which rule is
	// noisy?" answerable in Grafana as well as in the Exclusions tab — and
	// noise is the metric that decides whether a policy ever reaches
	// enforcement.
	if a.hits != nil {
		sums := a.hits.Summaries()
		if len(sums) > 0 {
			fmt.Fprint(w, "# HELP isovalent_control_policy_hits_total Events attributed to a TracingPolicy.\n")
			fmt.Fprint(w, "# TYPE isovalent_control_policy_hits_total counter\n")
			for _, s := range sums {
				fmt.Fprintf(w, "isovalent_control_policy_hits_total{policy=%q,namespace=%q} %d\n",
					s.Policy, s.Namespace, s.Total)
			}
			fmt.Fprint(w, "# HELP isovalent_control_policy_enforced_total Enforcement actions attributed to a TracingPolicy.\n")
			fmt.Fprint(w, "# TYPE isovalent_control_policy_enforced_total counter\n")
			for _, s := range sums {
				fmt.Fprintf(w, "isovalent_control_policy_enforced_total{policy=%q,namespace=%q} %d\n",
					s.Policy, s.Namespace, s.Enforced)
			}
		}
	}
}

func (a *Aggregator) touchNodeLocked(id string, e hubble.Endpoint) *nodeStat {
	n := a.nodes[id]
	if n == nil {
		n = &nodeStat{ID: id, Namespace: e.Namespace, Workload: e.Workload, External: e.Namespace == ""}
		a.nodes[id] = n
	}
	return n
}

func (a *Aggregator) bucketLocked(t time.Time) *bucket {
	if t.IsZero() {
		t = time.Now()
	}
	start := t.Unix() / bucketSeconds * bucketSeconds
	idx := int(start/bucketSeconds) % bucketCount
	if a.buckets[idx].start != start {
		a.buckets[idx] = bucket{start: start}
	}
	return &a.buckets[idx]
}

// RecentFlows returns up to limit most recent flows (newest last).
func (a *Aggregator) RecentFlows(limit int) []hubble.Flow {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return tail(a.flows, limit)
}

// RecentEvents returns up to limit most recent events.
func (a *Aggregator) RecentEvents(limit int) []tetragon.Event {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return tail(a.events, limit)
}

// RecentAlerts returns up to limit most recent alerts.
func (a *Aggregator) RecentAlerts(limit int) []Alert {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return tail(a.alerts, limit)
}

func tail[T any](s []T, limit int) []T {
	if limit <= 0 || limit > len(s) {
		limit = len(s)
	}
	out := make([]T, limit)
	copy(out, s[len(s)-limit:])
	return out
}

// ServiceMapNode / ServiceMapEdge are the UI graph payloads.
type ServiceMapNode struct {
	ID        string `json:"id"`
	Namespace string `json:"namespace,omitempty"`
	Workload  string `json:"workload"`
	External  bool   `json:"external"`
	Drops     int64  `json:"drops"`
	Kills     int64  `json:"kills"`
}

type ServiceMapEdge struct {
	Source    string   `json:"source"`
	Target    string   `json:"target"`
	Forwarded int64    `json:"forwarded"`
	Dropped   int64    `json:"dropped"`
	HTTP      bool     `json:"http"`
	DNS       bool     `json:"dns"`
	Ports     []uint32 `json:"ports"`
}

// ServiceMap returns the aggregated dependency graph.
func (a *Aggregator) ServiceMap() (nodes []ServiceMapNode, edges []ServiceMapEdge) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, n := range a.nodes {
		nodes = append(nodes, ServiceMapNode{ID: n.ID, Namespace: n.Namespace, Workload: n.Workload, External: n.External, Drops: n.Drops, Kills: n.Kills})
	}
	for _, e := range a.edges {
		ports := make([]uint32, 0, len(e.Ports))
		for p := range e.Ports {
			ports = append(ports, p)
		}
		sort.Slice(ports, func(i, j int) bool { return ports[i] < ports[j] })
		edges = append(edges, ServiceMapEdge{Source: e.Source, Target: e.Target, Forwarded: e.Forwarded, Dropped: e.Dropped, HTTP: e.HTTP, DNS: e.DNS, Ports: ports})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(edges, func(i, j int) bool {
		return edges[i].Source+edges[i].Target < edges[j].Source+edges[j].Target
	})
	return nodes, edges
}

// TimePoint is one 10s bucket of the overview time series.
type TimePoint struct {
	Time    int64 `json:"t"`
	Flows   int64 `json:"flows"`
	Drops   int64 `json:"drops"`
	HTTPReq int64 `json:"httpReq"`
	HTTPErr int64 `json:"httpErr"`
	DNSErr  int64 `json:"dnsErr"`
	Kills   int64 `json:"kills"`
}

// Overview is the payload behind GET /api/v1/overview.
type Overview struct {
	TotalFlows  int64       `json:"totalFlows"`
	TotalDrops  int64       `json:"totalDrops"`
	TotalEvents int64       `json:"totalEvents"`
	TotalKills  int64       `json:"totalKills"`
	FlowRate    float64     `json:"flowRate"` // flows/s over the last minute
	DropRate    float64     `json:"dropRate"`
	HTTPErrPct  float64     `json:"httpErrPct"` // last 15m
	DNSErrors   int64       `json:"dnsErrors"`  // last 15m
	Series      []TimePoint `json:"series"`
}

// Overview computes the dashboard summary.
func (a *Aggregator) Overview() Overview {
	a.mu.RLock()
	defer a.mu.RUnlock()
	now := time.Now().Unix() / bucketSeconds * bucketSeconds
	oldest := now - int64(bucketCount-1)*bucketSeconds

	o := Overview{TotalFlows: a.totalFlows, TotalDrops: a.totalDrops, TotalEvents: a.totalEvents, TotalKills: a.totalKills}
	var httpReq, httpErr int64
	var lastMinFlows, lastMinDrops int64
	for start := oldest; start <= now; start += bucketSeconds {
		idx := int(start/bucketSeconds) % bucketCount
		b := a.buckets[idx]
		tp := TimePoint{Time: start}
		if b.start == start {
			tp = TimePoint{Time: start, Flows: b.Flows, Drops: b.Drops, HTTPReq: b.HTTPReq, HTTPErr: b.HTTPErr, DNSErr: b.DNSErr, Kills: b.Kills}
			httpReq += b.HTTPReq
			httpErr += b.HTTPErr
			o.DNSErrors += b.DNSErr
			if start > now-60 {
				lastMinFlows += b.Flows
				lastMinDrops += b.Drops
			}
		}
		o.Series = append(o.Series, tp)
	}
	o.FlowRate = float64(lastMinFlows) / 60
	o.DropRate = float64(lastMinDrops) / 60
	if httpReq > 0 {
		o.HTTPErrPct = 100 * float64(httpErr) / float64(httpReq)
	}
	return o
}
