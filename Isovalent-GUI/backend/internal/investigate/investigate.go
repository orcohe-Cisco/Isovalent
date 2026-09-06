// Package investigate turns the stored flow and event history into something
// you can actually search.
//
// The two data sources have nothing in common structurally — a Hubble flow is
// a five-tuple with an optional L7 record, a Tetragon event is a process with
// arguments — so the package normalises both onto one Row. That is what makes
// "show me everything in the last hour in namespace shop that was blocked"
// answerable in a single query across Cilium and Tetragon, which is the
// question people actually ask during an incident.
package investigate

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/isovalent-control/isovalent-control/backend/internal/hubble"
	"github.com/isovalent-control/isovalent-control/backend/internal/store"
	"github.com/isovalent-control/isovalent-control/backend/internal/tetragon"
)

// Sources.
const (
	SourceCilium   = "cilium"
	SourceTetragon = "tetragon"
)

// Verdict values, normalised across both sources so one filter covers both.
const (
	VerdictForwarded = "forwarded"
	VerdictDropped   = "dropped"
	VerdictAudit     = "audit"
	VerdictError     = "error"
	VerdictObserved  = "observed" // Tetragon Post / no enforcement
	VerdictEnforced  = "enforced" // Tetragon Sigkill / Override
)

// Row is one normalised record.
type Row struct {
	ID     string    `json:"id"`
	Time   time.Time `json:"time"`
	Source string    `json:"source"`

	Verdict string `json:"verdict"`
	// Blocked is the single boolean the "blocked only" toggle uses.
	Blocked bool `json:"blocked"`

	Namespace  string `json:"namespace,omitempty"`
	Src        string `json:"src,omitempty"`
	Dst        string `json:"dst,omitempty"`
	Node       string `json:"node,omitempty"`
	Workload   string `json:"workload,omitempty"`
	Pod        string `json:"pod,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
	Port       uint32 `json:"port,omitempty"`
	L7Type     string `json:"l7Type,omitempty"`
	L7Method   string `json:"l7Method,omitempty"`
	L7Path     string `json:"l7Path,omitempty"`
	L7Status   uint32 `json:"l7Status,omitempty"`
	DNSQuery   string `json:"dnsQuery,omitempty"`
	Binary     string `json:"binary,omitempty"`
	Args       string `json:"args,omitempty"`
	User       string `json:"user,omitempty"`
	Container  string `json:"container,omitempty"`
	Hook       string `json:"hook,omitempty"`
	Action     string `json:"action,omitempty"`
	Policy     string `json:"policy,omitempty"`
	PolicyNS   string `json:"policyNamespace,omitempty"`
	PolicyKind string `json:"policyKind,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Summary    string `json:"summary"`
	IsReply    bool   `json:"isReply,omitempty"`
	// Raw carries the original record so the drill-down can show everything
	// without a second round trip.
	Raw json.RawMessage `json:"raw,omitempty"`
}

// Filter is a search request. Every list field is an OR within itself and an
// AND against the others, which is what the clickable facet chips imply.
type Filter struct {
	Sources      []string
	Since, Until time.Time
	Namespaces   []string
	Src          []string
	Dst          []string
	Verdicts     []string
	Policies     []string
	Binaries     []string
	Workloads    []string
	Nodes        []string
	L7Types      []string
	L7Methods    []string
	L7Path       string
	Query        string
	BlockedOnly  bool
	HideReplies  bool
	Limit        int
	// Scan bounds how many stored records are considered. Filtering happens
	// in Go so the memory store and Postgres behave identically; this is the
	// knob that keeps that honest on a busy cluster.
	Scan int
}

// Facet is one clickable filter value with its frequency in the result set.
type Facet struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// Result is a search response.
type Result struct {
	Rows      []Row              `json:"rows"`
	Total     int                `json:"total"`
	Scanned   int                `json:"scanned"`
	Truncated bool               `json:"truncated"`
	Facets    map[string][]Facet `json:"facets"`
	Series    []Bucket           `json:"series"`
	Window    Window             `json:"window"`
}

// Window echoes the resolved time range.
type Window struct {
	Since time.Time `json:"since"`
	Until time.Time `json:"until"`
	Label string    `json:"label,omitempty"`
}

// Bucket is one point of the result timeline.
type Bucket struct {
	Time    int64 `json:"t"`
	Total   int   `json:"total"`
	Blocked int   `json:"blocked"`
}

// ParseWindow resolves a relative window label ("5m", "1h", "1d", "7d",
// "30d") to an absolute range ending now.
func ParseWindow(label string, since, until time.Time) Window {
	if !since.IsZero() || !until.IsZero() {
		if until.IsZero() {
			until = time.Now().UTC()
		}
		return Window{Since: since, Until: until, Label: "custom"}
	}
	d := durationOf(label)
	now := time.Now().UTC()
	return Window{Since: now.Add(-d), Until: now, Label: label}
}

func durationOf(label string) time.Duration {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "1h", "":
		return time.Hour
	case "6h":
		return 6 * time.Hour
	case "1d", "24h":
		return 24 * time.Hour
	case "7d", "1w":
		return 7 * 24 * time.Hour
	case "30d":
		return 30 * 24 * time.Hour
	}
	return time.Hour
}

// Search runs the query against the store.
func Search(ctx context.Context, s store.Store, f Filter) (Result, error) {
	if f.Limit <= 0 || f.Limit > 2000 {
		f.Limit = 200
	}
	if f.Scan <= 0 || f.Scan > 50000 {
		f.Scan = 20000
	}
	res := Result{Facets: map[string][]Facet{}, Window: Window{Since: f.Since, Until: f.Until}}
	if s == nil {
		res.Rows = []Row{}
		return res, nil
	}

	want := map[string]bool{}
	for _, src := range f.Sources {
		want[src] = true
	}
	if len(want) == 0 {
		want[SourceCilium] = true
		want[SourceTetragon] = true
	}

	var rows []Row
	if want[SourceCilium] {
		recs, err := s.Query(ctx, store.KindFlow, f.Since, f.Until, f.Scan)
		if err != nil {
			return res, err
		}
		res.Scanned += len(recs)
		for _, r := range recs {
			var fl hubble.Flow
			if json.Unmarshal(r.Payload, &fl) == nil {
				rows = append(rows, flowRow(r, fl))
			}
		}
	}
	if want[SourceTetragon] {
		recs, err := s.Query(ctx, store.KindEvent, f.Since, f.Until, f.Scan)
		if err != nil {
			return res, err
		}
		res.Scanned += len(recs)
		for _, r := range recs {
			var ev tetragon.Event
			if json.Unmarshal(r.Payload, &ev) == nil {
				rows = append(rows, eventRow(r, ev))
			}
		}
	}

	kept := rows[:0]
	for _, row := range rows {
		if matches(row, f) {
			kept = append(kept, row)
		}
	}
	rows = kept
	sort.Slice(rows, func(i, j int) bool { return rows[i].Time.After(rows[j].Time) })

	res.Total = len(rows)
	res.Facets = facets(rows)
	res.Series = series(rows, f)
	if len(rows) > f.Limit {
		rows = rows[:f.Limit]
		res.Truncated = true
	}
	if rows == nil {
		rows = []Row{}
	}
	res.Rows = rows
	return res, nil
}

func flowRow(r store.Record, f hubble.Flow) Row {
	verdict := strings.ToLower(f.Verdict)
	switch verdict {
	case "forwarded", "dropped", "audit", "error":
	default:
		verdict = VerdictForwarded
	}
	row := Row{
		ID: r.Time.Format(time.RFC3339Nano) + "|c", Time: r.Time, Source: SourceCilium,
		Verdict: verdict, Blocked: verdict == VerdictDropped,
		Namespace: f.Source.Namespace,
		Src:       endpointID(f.Source), Dst: endpointID(f.Destination),
		Node: f.Node, Workload: f.Source.Workload, Pod: f.Source.PodName,
		Protocol: f.L4.Protocol, Port: f.L4.DstPort,
		Reason: f.DropReason, Summary: f.Summary, IsReply: f.IsReply,
		Raw: r.Payload,
	}
	if row.Namespace == "" {
		row.Namespace = f.Destination.Namespace
	}
	if f.L7 != nil {
		row.L7Type = f.L7.Type
		row.L7Method = f.L7.Method
		row.L7Path = f.L7.URL
		row.L7Status = f.L7.Status
		row.DNSQuery = f.L7.DNSQuery
	}
	// Prefer the policy that produced the verdict being shown.
	for _, p := range f.Policies {
		if (row.Blocked && p.Effect == "denied") || (!row.Blocked && p.Effect == "allowed") {
			row.Policy, row.PolicyNS, row.PolicyKind = p.Name, p.Namespace, p.Kind
			break
		}
	}
	if row.Policy == "" && len(f.Policies) > 0 {
		row.Policy, row.PolicyNS, row.PolicyKind = f.Policies[0].Name, f.Policies[0].Namespace, f.Policies[0].Kind
	}
	if row.Summary == "" {
		row.Summary = strings.TrimSpace(row.Src + " → " + row.Dst + " " + row.Protocol + ":" + itoa(int(row.Port)))
		if row.L7Method != "" {
			row.Summary += " " + row.L7Method + " " + row.L7Path
		}
	}
	return row
}

func eventRow(r store.Record, e tetragon.Event) Row {
	enforced := e.Action == "SIGKILL" || e.Action == "OVERRIDE" || e.Action == "SIGNAL"
	verdict := VerdictObserved
	if enforced {
		verdict = VerdictEnforced
	}
	row := Row{
		ID: r.Time.Format(time.RFC3339Nano) + "|t", Time: r.Time, Source: SourceTetragon,
		Verdict: verdict, Blocked: enforced,
		Namespace: e.Namespace, Workload: e.Workload, Pod: e.Pod, Node: e.Node,
		Binary: e.Binary, Args: e.Args, User: e.UserLabel(), Container: e.Container,
		Hook: e.Function, Action: e.Action, Policy: e.Policy, PolicyNS: e.PolicyNamespace,
		PolicyKind: "TracingPolicy", Reason: e.Details, Raw: r.Payload,
	}
	row.Src = strings.TrimSuffix(e.Namespace+"/"+e.Workload, "/")
	row.Summary = strings.TrimSpace(e.Binary + " " + e.Args)
	if e.Function != "" {
		row.Summary = e.Function + " ← " + row.Summary
	}
	return row
}

func endpointID(e hubble.Endpoint) string {
	if e.Namespace == "" {
		if e.Workload != "" {
			return e.Workload
		}
		return "world"
	}
	if e.Workload == "" {
		return e.Namespace + "/" + e.PodName
	}
	return e.Namespace + "/" + e.Workload
}

func matches(row Row, f Filter) bool {
	if f.HideReplies && row.IsReply {
		return false
	}
	if f.BlockedOnly && !row.Blocked {
		return false
	}
	if !anyOf(f.Namespaces, row.Namespace) ||
		!anyOf(f.Src, row.Src) ||
		!anyOf(f.Dst, row.Dst) ||
		!anyOf(f.Verdicts, row.Verdict) ||
		!anyOf(f.Binaries, row.Binary) ||
		!anyOf(f.Workloads, row.Workload) ||
		!anyOf(f.Nodes, row.Node) ||
		!anyOf(f.L7Types, row.L7Type) ||
		!anyOfFold(f.L7Methods, row.L7Method) {
		return false
	}
	if len(f.Policies) > 0 {
		key := row.Policy
		if row.PolicyNS != "" {
			key = row.PolicyNS + "/" + row.Policy
		}
		if !anyOf(f.Policies, row.Policy) && !anyOf(f.Policies, key) {
			return false
		}
	}
	if f.L7Path != "" && !strings.Contains(strings.ToLower(row.L7Path), strings.ToLower(f.L7Path)) {
		return false
	}
	if q := strings.ToLower(strings.TrimSpace(f.Query)); q != "" {
		if !strings.Contains(strings.ToLower(searchable(row)), q) {
			return false
		}
	}
	return true
}

func searchable(r Row) string {
	return strings.Join([]string{
		r.Src, r.Dst, r.Namespace, r.Workload, r.Pod, r.Node, r.Binary, r.Args,
		r.User, r.Container, r.Hook, r.Policy, r.Reason, r.Summary,
		r.L7Method, r.L7Path, r.DNSQuery, r.Protocol, r.Verdict,
	}, " ")
}

func anyOf(want []string, have string) bool {
	if len(want) == 0 {
		return true
	}
	for _, w := range want {
		if w == have {
			return true
		}
	}
	return false
}

func anyOfFold(want []string, have string) bool {
	if len(want) == 0 {
		return true
	}
	for _, w := range want {
		if strings.EqualFold(w, have) {
			return true
		}
	}
	return false
}

// facets computes the clickable filter values. Only dimensions with more than
// one distinct value are worth showing, but that decision is the UI's; here we
// return everything, capped.
func facets(rows []Row) map[string][]Facet {
	dims := map[string]map[string]int{
		"namespaces": {}, "sources": {}, "destinations": {}, "verdicts": {},
		"policies": {}, "binaries": {}, "workloads": {}, "nodes": {},
		"l7Types": {}, "l7Methods": {}, "users": {}, "hooks": {},
	}
	add := func(dim, v string) {
		if v != "" {
			dims[dim][v]++
		}
	}
	for _, r := range rows {
		add("namespaces", r.Namespace)
		add("sources", r.Src)
		add("destinations", r.Dst)
		add("verdicts", r.Verdict)
		key := r.Policy
		if r.PolicyNS != "" && r.Policy != "" {
			key = r.PolicyNS + "/" + r.Policy
		}
		add("policies", key)
		add("binaries", r.Binary)
		add("workloads", r.Workload)
		add("nodes", r.Node)
		add("l7Types", r.L7Type)
		add("l7Methods", r.L7Method)
		add("users", r.User)
		add("hooks", r.Hook)
	}
	out := map[string][]Facet{}
	for dim, counts := range dims {
		list := make([]Facet, 0, len(counts))
		for v, c := range counts {
			list = append(list, Facet{Value: v, Count: c})
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].Count != list[j].Count {
				return list[i].Count > list[j].Count
			}
			return list[i].Value < list[j].Value
		})
		if len(list) > 60 {
			list = list[:60]
		}
		out[dim] = list
	}
	return out
}

// series buckets the matched rows into ~60 points so the timeline has a stable
// resolution regardless of the window length.
func series(rows []Row, f Filter) []Bucket {
	if len(rows) == 0 {
		return []Bucket{}
	}
	since, until := f.Since, f.Until
	if since.IsZero() {
		since = rows[len(rows)-1].Time
	}
	if until.IsZero() {
		until = rows[0].Time
	}
	span := until.Sub(since)
	if span <= 0 {
		span = time.Minute
	}
	const buckets = 60
	step := span / buckets
	if step < time.Second {
		step = time.Second
	}
	idx := map[int64]*Bucket{}
	var order []int64
	for _, r := range rows {
		slot := r.Time.Truncate(step).Unix()
		b := idx[slot]
		if b == nil {
			b = &Bucket{Time: slot}
			idx[slot] = b
			order = append(order, slot)
		}
		b.Total++
		if r.Blocked {
			b.Blocked++
		}
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	out := make([]Bucket, 0, len(order))
	for _, slot := range order {
		out = append(out, *idx[slot])
	}
	return out
}

func itoa(i int) string { return strconv.Itoa(i) }
