package server

import (
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/isovalent-control/isovalent-control/backend/internal/hubble"
	"github.com/isovalent-control/isovalent-control/backend/internal/tetragon"
)

// Dimensions of real cluster data the UI offers as exclusion targets. The
// point is that you pick an exclusion from what the agents have actually seen
// rather than typing a namespace name from memory and getting it wrong.
const (
	DimNamespace = "namespaces"
	DimWorkload  = "workloads"
	DimPod       = "pods"
	DimNode      = "nodes"
	DimBinary    = "binaries"
	DimUser      = "users"
	DimPodLabel  = "podLabels"
	DimPath      = "paths"
	DimContainer = "containers"
)

// ObservedDimensions is the canonical order the UI renders them in.
var ObservedDimensions = []string{
	DimNamespace, DimWorkload, DimPodLabel, DimContainer, DimUser,
	DimBinary, DimPath, DimPod, DimNode,
}

// Bounded so a noisy cluster cannot grow this without limit; when full, the
// least-recently-seen value is evicted.
const maxObservedPerDimension = 400

// ObservedValue is one distinct value the platform has seen.
type ObservedValue struct {
	Value    string    `json:"value"`
	Count    int64     `json:"count"`
	LastSeen time.Time `json:"lastSeen"`
	// Context disambiguates values that repeat across scopes — the namespace
	// a workload was seen in, or the workload a binary ran under.
	Context string `json:"context,omitempty"`
}

// ObservedCatalog accumulates distinct values per dimension. It keeps its own
// lock so the aggregator's hot path does not have to hold the big mutex.
type ObservedCatalog struct {
	mu   sync.Mutex
	dims map[string]map[string]*ObservedValue
}

// NewObservedCatalog returns an empty catalog.
func NewObservedCatalog() *ObservedCatalog {
	return &ObservedCatalog{dims: map[string]map[string]*ObservedValue{}}
}

func (o *ObservedCatalog) record(dim, value, context string, at time.Time) {
	if value == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	bucket := o.dims[dim]
	if bucket == nil {
		bucket = map[string]*ObservedValue{}
		o.dims[dim] = bucket
	}
	if v, ok := bucket[value]; ok {
		v.Count++
		if at.After(v.LastSeen) {
			v.LastSeen = at
		}
		if v.Context == "" {
			v.Context = context
		}
		return
	}
	if len(bucket) >= maxObservedPerDimension {
		evictOldest(bucket)
	}
	bucket[value] = &ObservedValue{Value: value, Count: 1, LastSeen: at, Context: context}
}

func evictOldest(bucket map[string]*ObservedValue) {
	var oldestKey string
	var oldest time.Time
	first := true
	for k, v := range bucket {
		if first || v.LastSeen.Before(oldest) {
			oldestKey, oldest, first = k, v.LastSeen, false
		}
	}
	delete(bucket, oldestKey)
}

func (o *ObservedCatalog) recordFlow(f hubble.Flow) {
	for _, e := range []hubble.Endpoint{f.Source, f.Destination} {
		o.record(DimNamespace, e.Namespace, "", f.Time)
		o.record(DimWorkload, e.Workload, e.Namespace, f.Time)
	}
	o.record(DimNode, f.Node, "", f.Time)
}

func (o *ObservedCatalog) recordEvent(e tetragon.Event) {
	o.record(DimNamespace, e.Namespace, "", e.Time)
	o.record(DimWorkload, e.Workload, e.Namespace, e.Time)
	o.record(DimPod, e.Pod, e.Namespace, e.Time)
	o.record(DimNode, e.Node, "", e.Time)
	o.record(DimBinary, e.Binary, e.Workload, e.Time)
	o.record(DimContainer, e.Container, e.Workload, e.Time)
	o.record(DimUser, e.UserLabel(), e.Workload, e.Time)
	for _, path := range filePaths(e.Details) {
		o.record(DimPath, path, e.Function, e.Time)
	}
	for k, v := range e.Labels {
		// Skip the labels Kubernetes generates per-pod; they are useless as
		// an exclusion because they never match a second pod.
		if k == "pod-template-hash" || k == "controller-revision-hash" ||
			k == "pod-template-generation" || strings.HasPrefix(k, "statefulset.kubernetes.io/") {
			continue
		}
		o.record(DimPodLabel, k+"="+v, e.Namespace, e.Time)
	}
}

// filePaths pulls absolute paths out of a kprobe argument string. File and
// path arguments are rendered into Details verbatim, so this is where the
// values a matchArgs NotPrefix exclusion would use actually come from. The
// parent directory is recorded too, because a prefix exclusion is usually
// written against the directory rather than the exact file.
func filePaths(details string) []string {
	if details == "" {
		return nil
	}
	var out []string
	for _, tok := range strings.Fields(details) {
		if len(tok) < 2 || tok[0] != '/' {
			continue
		}
		// Skip things that only look like paths, e.g. "10.0.0.1:5432".
		if strings.ContainsAny(tok, ":,") {
			continue
		}
		out = append(out, tok)
		if i := strings.LastIndexByte(tok, '/'); i > 0 {
			out = append(out, tok[:i+1])
		}
	}
	return out
}

// Snapshot returns every dimension, each sorted by frequency then name.
func (o *ObservedCatalog) Snapshot(limit int) map[string][]ObservedValue {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := map[string][]ObservedValue{}
	for _, dim := range ObservedDimensions {
		bucket := o.dims[dim]
		values := make([]ObservedValue, 0, len(bucket))
		for _, v := range bucket {
			values = append(values, *v)
		}
		sort.Slice(values, func(i, j int) bool {
			if values[i].Count != values[j].Count {
				return values[i].Count > values[j].Count
			}
			return values[i].Value < values[j].Value
		})
		if limit > 0 && len(values) > limit {
			values = values[:limit]
		}
		out[dim] = values
	}
	return out
}

// Observed exposes the catalog for the HTTP handler.
func (a *Aggregator) Observed(limit int) map[string][]ObservedValue {
	return a.observed.Snapshot(limit)
}

// RecordObserved feeds the catalog. Called from the ingest paths.
func (a *Aggregator) recordObservedFlow(f hubble.Flow)     { a.observed.recordFlow(f) }
func (a *Aggregator) recordObservedEvent(e tetragon.Event) { a.observed.recordEvent(e) }

func (s *Server) getObserved(w http.ResponseWriter, req *http.Request) {
	limit := queryInt(req, "limit", 200)
	snap := s.agg.Observed(limit)

	// Optional server-side filter so a big cluster does not ship 400 values
	// per dimension to a picker the user is typing into.
	if q := strings.ToLower(strings.TrimSpace(req.URL.Query().Get("q"))); q != "" {
		for dim, values := range snap {
			kept := values[:0]
			for _, v := range values {
				if strings.Contains(strings.ToLower(v.Value), q) {
					kept = append(kept, v)
				}
			}
			snap[dim] = kept
		}
	}
	if dim := req.URL.Query().Get("dim"); dim != "" {
		snap = map[string][]ObservedValue{dim: snap[dim]}
	}

	counts := map[string]int{}
	for dim, values := range snap {
		counts[dim] = len(values)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"dimensions": ObservedDimensions,
		"values":     snap,
		"counts":     counts,
		"mode":       "live",
	})
}
