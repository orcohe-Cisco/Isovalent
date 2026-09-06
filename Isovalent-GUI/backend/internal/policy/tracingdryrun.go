package policy

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/isovalent-control/isovalent-control/backend/internal/k8s"
	"github.com/isovalent-control/isovalent-control/backend/internal/tetragon"
)

// TracingDryRun answers "if I turn this on, what happens?" by replaying the
// stored event history through the proposed policy.
//
// This is the difference between shipping a Sigkill policy and hoping, and
// knowing it would have killed 1,400 processes in the last hour, 1,390 of them
// your log shipper. Runtime enforcement has no undo, so the dry run is not a
// nicety.
type TracingDryRun struct {
	Total int `json:"total"`
	// Matched is how many stored events the policy would have selected.
	Matched int `json:"matched"`
	// WouldEnforce is the subset that would have been killed, if the policy
	// is in enforce mode.
	WouldEnforce int  `json:"wouldEnforce"`
	Enforcing    bool `json:"enforcing"`

	// ByBinary/ByNamespace/ByWorkload break the matches down so the noisy
	// source is visible without reading 1,400 rows.
	ByBinary    []Count `json:"byBinary"`
	ByNamespace []Count `json:"byNamespace"`
	ByWorkload  []Count `json:"byWorkload"`
	ByHook      []Count `json:"byHook"`

	// Samples are the first matches, with the clause that matched.
	Samples []TracingSample `json:"samples"`

	// Window describes the history considered.
	Window struct {
		Since time.Time `json:"since"`
		Until time.Time `json:"until"`
	} `json:"window"`

	PolicyErr string `json:"policyError,omitempty"`
	// Advice is a plain-language read of the result.
	Advice string `json:"advice,omitempty"`
}

// Count is one breakdown bucket.
type Count struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// TracingSample is one matched event plus why it matched.
type TracingSample struct {
	Event   tetragon.Event  `json:"event"`
	Explain k8s.Explanation `json:"explain"`
}

const maxTracingSamples = 25

// SimulateTracing replays events through a proposed TracingPolicy manifest.
func SimulateTracing(manifest json.RawMessage, events []tetragon.Event) TracingDryRun {
	var res TracingDryRun
	res.Total = len(events)

	var doc map[string]any
	if err := json.Unmarshal(manifest, &doc); err != nil {
		res.PolicyErr = "policy is not valid JSON/YAML: " + err.Error()
		return res
	}
	res.Enforcing = k8s.DetectAction(manifest) == k8s.ActionEnforce

	byBinary, byNS, byWorkload, byHook := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	for _, e := range events {
		if res.Window.Since.IsZero() || e.Time.Before(res.Window.Since) {
			res.Window.Since = e.Time
		}
		if e.Time.After(res.Window.Until) {
			res.Window.Until = e.Time
		}
		// The event's own policy attribution is irrelevant here: we are asking
		// whether *this* proposed policy would have selected it.
		probe := e
		probe.Policy = ""
		ex := k8s.ExplainTracingMatch(k8s.KindTP, manifest, probe)
		if !ex.Confident || len(ex.Clauses) == 0 && ex.Hook == "" {
			continue
		}
		res.Matched++
		if res.Enforcing {
			res.WouldEnforce++
		}
		byBinary[e.Binary]++
		byNS[e.Namespace]++
		byWorkload[e.Workload]++
		byHook[e.Function]++
		if len(res.Samples) < maxTracingSamples {
			res.Samples = append(res.Samples, TracingSample{Event: e, Explain: ex})
		}
	}

	res.ByBinary = topCounts(byBinary, 12)
	res.ByNamespace = topCounts(byNS, 12)
	res.ByWorkload = topCounts(byWorkload, 12)
	res.ByHook = topCounts(byHook, 12)
	res.Advice = advise(res)
	return res
}

func advise(r TracingDryRun) string {
	switch {
	case r.PolicyErr != "":
		return ""
	case r.Total == 0:
		return "No stored events to replay yet. Leave the policy in monitor mode until history has accumulated — a dry run against nothing proves nothing."
	case r.Matched == 0:
		return "This policy would not have matched anything in the stored window. Either the behaviour it looks for has not happened, or the selector does not match what you think it does — check the hook name and pod selector before assuming the former."
	case r.Enforcing && r.WouldEnforce > 0 && len(r.ByBinary) > 0 && r.ByBinary[0].Count*2 > r.Matched:
		return "One process (" + r.ByBinary[0].Value + ") accounts for most of the matches. Exclude it before enabling enforcement, or this policy will spend its time killing that."
	case r.Enforcing:
		return "Enforcing this policy would have killed " + itoa(r.WouldEnforce) + " process(es) in the replayed window. Review the samples before applying."
	default:
		return "Monitor mode: this policy would have produced " + itoa(r.Matched) + " event(s) in the replayed window."
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}

func topCounts(m map[string]int, n int) []Count {
	out := make([]Count, 0, len(m))
	for v, c := range m {
		if v == "" {
			continue
		}
		out = append(out, Count{Value: v, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Value < out[j].Value
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}
