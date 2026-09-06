// Package hits answers the question the Exclusions tab exists for: this
// TracingPolicy has fired 4,812 times — what fired it?
//
// Tetragon tells you a policy matched, and it tells you the process, the
// arguments, the pod and the user. It does not tell you which of those is the
// one you want to stop matching on. So we accumulate every event a policy
// produced, broken down along exactly the dimensions Tetragon can express an
// exclusion on, and let the operator approve one. An exclusion offered from a
// value that has actually been seen 4,000 times is a very different object
// from a free-text box.
package hits

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/isovalent-control/isovalent-control/backend/internal/tetragon"
)

// Dimensions, in the order the UI renders them. Each maps onto a documented
// Tetragon exclusion mechanism — see internal/k8s/exclusions.go.
const (
	DimBinary       = "binaries"
	DimParentBinary = "parentBinaries"
	DimPath         = "paths"
	DimUser         = "users"
	DimPodLabel     = "podLabels"
	DimContainer    = "containers"
	DimNamespace    = "namespaces"
	DimWorkload     = "workloads"
	DimPod          = "pods"
	DimNode         = "nodes"
	DimHook         = "hooks"
)

// Dimensions is the canonical render order.
var Dimensions = []string{
	DimBinary, DimParentBinary, DimPath, DimUser, DimPodLabel,
	DimContainer, DimNamespace, DimWorkload, DimPod, DimNode, DimHook,
}

// Excludable reports whether a dimension can be turned into a policy-level
// exclusion. The others are shown for diagnosis only, and the UI says so.
func Excludable(dim string) bool {
	switch dim {
	case DimBinary, DimParentBinary, DimPath, DimPodLabel, DimContainer:
		return true
	}
	return false
}

const (
	maxValuesPerDimension = 500
	samplesPerPolicy      = 40
)

// Value is one distinct value seen under a dimension for a policy.
type Value struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
	// Enforced counts the subset that resulted in a kill/override rather than
	// an observation. Excluding a value with a non-zero enforced count is a
	// materially bigger decision, so it is surfaced separately.
	Enforced  int64     `json:"enforced"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
	// ArgIndex is the hook argument position a path value came from, which is
	// what a matchArgs exclusion needs. -1 when not applicable.
	ArgIndex int `json:"argIndex"`
	// Excludable mirrors Excludable(dim) for the UI.
	Excludable bool `json:"excludable"`
}

// Summary is one policy's headline row in the Exclusions tab.
type Summary struct {
	Policy    string `json:"policy"`
	Namespace string `json:"namespace,omitempty"`
	Total     int64  `json:"total"`
	Enforced  int64  `json:"enforced"`
	// Unique counts distinct values per dimension.
	Unique    map[string]int `json:"unique"`
	FirstSeen time.Time      `json:"firstSeen"`
	LastSeen  time.Time      `json:"lastSeen"`
	// TopBinary is the single loudest process, shown inline in the list.
	TopBinary string `json:"topBinary,omitempty"`
	// RatePerMin over the tracked window, so "noisy" is a number.
	RatePerMin float64 `json:"ratePerMin"`
}

// Detail is the drill-down for one policy.
type Detail struct {
	Summary
	Values  map[string][]Value `json:"values"`
	Samples []tetragon.Event   `json:"samples"`
}

type policyState struct {
	namespace string
	total     int64
	enforced  int64
	first     time.Time
	last      time.Time
	dims      map[string]map[string]*Value
	samples   []tetragon.Event
}

// Tracker accumulates per-policy hits. Safe for concurrent use.
type Tracker struct {
	mu       sync.RWMutex
	policies map[string]*policyState
}

// New returns an empty tracker.
func New() *Tracker { return &Tracker{policies: map[string]*policyState{}} }

func key(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}

// Record ingests one Tetragon event. Events without a policy name come from
// the base sensors rather than a TracingPolicy and are ignored here.
func (t *Tracker) Record(e tetragon.Event) {
	if e.Policy == "" {
		return
	}
	enforced := e.Action == "SIGKILL" || e.Action == "OVERRIDE" || e.Action == "SIGNAL"
	at := e.Time
	if at.IsZero() {
		at = time.Now().UTC()
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	k := key(e.PolicyNamespace, e.Policy)
	p := t.policies[k]
	if p == nil {
		p = &policyState{namespace: e.PolicyNamespace, dims: map[string]map[string]*Value{}, first: at}
		t.policies[k] = p
	}
	p.total++
	if enforced {
		p.enforced++
	}
	if p.first.IsZero() || at.Before(p.first) {
		p.first = at
	}
	if at.After(p.last) {
		p.last = at
	}

	p.record(DimBinary, e.Binary, -1, enforced, at)
	p.record(DimParentBinary, e.Parent, -1, enforced, at)
	p.record(DimUser, e.UserLabel(), -1, enforced, at)
	p.record(DimContainer, e.Container, -1, enforced, at)
	p.record(DimNamespace, e.Namespace, -1, enforced, at)
	p.record(DimWorkload, e.Workload, -1, enforced, at)
	p.record(DimPod, e.Pod, -1, enforced, at)
	p.record(DimNode, e.Node, -1, enforced, at)
	p.record(DimHook, e.Function, -1, enforced, at)
	for k, v := range e.Labels {
		if noisyLabel(k) {
			continue
		}
		p.record(DimPodLabel, k+"="+v, -1, enforced, at)
	}
	for i, arg := range e.ArgList {
		for _, path := range pathsIn(arg) {
			p.record(DimPath, path, i, enforced, at)
		}
	}

	p.samples = append(p.samples, e)
	if len(p.samples) > samplesPerPolicy {
		p.samples = p.samples[len(p.samples)-samplesPerPolicy:]
	}
}

// noisyLabel filters the labels Kubernetes generates per-ReplicaSet or
// per-pod. They are useless as an exclusion: they never match a second pod.
func noisyLabel(k string) bool {
	switch k {
	case "pod-template-hash", "controller-revision-hash", "pod-template-generation":
		return true
	}
	return strings.HasPrefix(k, "statefulset.kubernetes.io/")
}

// pathsIn extracts the absolute path from an argument, plus its parent
// directory — a prefix exclusion is usually written against the directory.
func pathsIn(arg string) []string {
	arg = strings.TrimSpace(arg)
	if len(arg) < 2 || arg[0] != '/' || strings.ContainsAny(arg, " :,") {
		return nil
	}
	out := []string{arg}
	if i := strings.LastIndexByte(arg, '/'); i > 0 {
		out = append(out, arg[:i+1])
	}
	return out
}

func (p *policyState) record(dim, value string, argIndex int, enforced bool, at time.Time) {
	if value == "" {
		return
	}
	bucket := p.dims[dim]
	if bucket == nil {
		bucket = map[string]*Value{}
		p.dims[dim] = bucket
	}
	v := bucket[value]
	if v == nil {
		if len(bucket) >= maxValuesPerDimension {
			evictQuietest(bucket)
		}
		v = &Value{Value: value, FirstSeen: at, ArgIndex: argIndex, Excludable: Excludable(dim)}
		bucket[value] = v
	}
	v.Count++
	if enforced {
		v.Enforced++
	}
	if at.After(v.LastSeen) {
		v.LastSeen = at
	}
	if argIndex >= 0 {
		v.ArgIndex = argIndex
	}
}

// evictQuietest drops the least-frequent value. Frequency is the right axis
// here: the whole feature is about finding the loud ones.
func evictQuietest(bucket map[string]*Value) {
	var worstKey string
	var worst int64 = -1
	for k, v := range bucket {
		if worst < 0 || v.Count < worst {
			worstKey, worst = k, v.Count
		}
	}
	delete(bucket, worstKey)
}

// Summaries returns one row per policy that has produced at least one event.
func (t *Tracker) Summaries() []Summary {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]Summary, 0, len(t.policies))
	for k, p := range t.policies {
		name := k
		if p.namespace != "" {
			name = strings.TrimPrefix(k, p.namespace+"/")
		}
		out = append(out, p.summary(name))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Total != out[j].Total {
			return out[i].Total > out[j].Total
		}
		return out[i].Policy < out[j].Policy
	})
	return out
}

func (p *policyState) summary(name string) Summary {
	s := Summary{
		Policy: name, Namespace: p.namespace, Total: p.total, Enforced: p.enforced,
		Unique: map[string]int{}, FirstSeen: p.first, LastSeen: p.last,
	}
	for _, dim := range Dimensions {
		s.Unique[dim] = len(p.dims[dim])
	}
	var top string
	var topCount int64
	for value, v := range p.dims[DimBinary] {
		if v.Count > topCount {
			top, topCount = value, v.Count
		}
	}
	s.TopBinary = top
	if window := p.last.Sub(p.first).Minutes(); window > 0.1 {
		s.RatePerMin = float64(p.total) / window
	} else {
		s.RatePerMin = float64(p.total)
	}
	return s
}

// Detail returns the full breakdown for one policy, or false if unseen.
func (t *Tracker) Detail(namespace, name string) (Detail, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	p := t.policies[key(namespace, name)]
	if p == nil {
		return Detail{}, false
	}
	d := Detail{Summary: p.summary(name), Values: map[string][]Value{}}
	for _, dim := range Dimensions {
		bucket := p.dims[dim]
		vals := make([]Value, 0, len(bucket))
		for _, v := range bucket {
			vals = append(vals, *v)
		}
		sort.Slice(vals, func(i, j int) bool {
			if vals[i].Count != vals[j].Count {
				return vals[i].Count > vals[j].Count
			}
			return vals[i].Value < vals[j].Value
		})
		d.Values[dim] = vals
	}
	d.Samples = make([]tetragon.Event, len(p.samples))
	copy(d.Samples, p.samples)
	// Newest first: the last thing that happened is the thing being chased.
	for i, j := 0, len(d.Samples)-1; i < j; i, j = i+1, j-1 {
		d.Samples[i], d.Samples[j] = d.Samples[j], d.Samples[i]
	}
	return d, true
}

// Reset clears counters for one policy (used after an exclusion is applied, so
// the operator can see whether it actually worked).
func (t *Tracker) Reset(namespace, name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.policies, key(namespace, name))
}

// Totals returns aggregate counters for the overview.
func (t *Tracker) Totals() (policies int, events int64, enforced int64) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, p := range t.policies {
		policies++
		events += p.total
		enforced += p.enforced
	}
	return
}
