// Package policy provides a simplified CiliumNetworkPolicy simulator used by the
// dry-run feature: it classifies recent Hubble flows as allowed or blocked
// under a proposed policy, before that policy is applied to the cluster.
//
// This is an approximation, not a reimplementation of Cilium's policy engine.
// It models the core rule most people care about: once an endpoint is selected
// by a policy, traffic is default-deny except what the ingress/egress rules
// explicitly allow. It is intended to catch "this policy would break X" before
// you apply it, not to be authoritative.
package policy

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/isovalent-control/isovalent-control/backend/internal/hubble"
)

// Verdict is the simulated outcome for one flow under the proposed policy.
type Verdict struct {
	Flow    hubble.Flow `json:"flow"`
	Applies bool        `json:"applies"` // did the policy select an endpoint of this flow?
	Allowed bool        `json:"allowed"` // if it applies, would the flow be permitted?
	Reason  string      `json:"reason,omitempty"`
}

// Result summarizes a simulation.
type Result struct {
	Total     int       `json:"total"`
	Applied   int       `json:"applied"`
	Allowed   int       `json:"allowed"`
	Blocked   int       `json:"blocked"`
	Verdicts  []Verdict `json:"verdicts"`
	PolicyErr string    `json:"policyError,omitempty"`
}

type selector map[string]string

type portRule struct {
	port  string
	proto string
}

type rule struct {
	peers []selector // fromEndpoints (ingress) / toEndpoints (egress)
	ports []portRule
}

type parsedPolicy struct {
	endpoint selector
	ingress  []rule
	egress   []rule
}

// Simulate evaluates a CiliumNetworkPolicy/CCNP manifest against flows.
func Simulate(manifest json.RawMessage, flows []hubble.Flow) Result {
	pp, err := parse(manifest)
	if err != nil {
		return Result{PolicyErr: err.Error()}
	}
	res := Result{Total: len(flows)}
	for _, f := range flows {
		v := evaluate(pp, f)
		if v.Applies {
			res.Applied++
			if v.Allowed {
				res.Allowed++
			} else {
				res.Blocked++
			}
		}
		res.Verdicts = append(res.Verdicts, v)
	}
	return res
}

func parse(manifest json.RawMessage) (parsedPolicy, error) {
	var doc struct {
		Spec struct {
			EndpointSelector struct {
				MatchLabels map[string]string `json:"matchLabels"`
			} `json:"endpointSelector"`
			Ingress []rawRule `json:"ingress"`
			Egress  []rawRule `json:"egress"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(manifest, &doc); err != nil {
		return parsedPolicy{}, fmt.Errorf("parse policy: %w", err)
	}
	pp := parsedPolicy{endpoint: doc.Spec.EndpointSelector.MatchLabels}
	for _, r := range doc.Spec.Ingress {
		pp.ingress = append(pp.ingress, r.toRule(true))
	}
	for _, r := range doc.Spec.Egress {
		pp.egress = append(pp.egress, r.toRule(false))
	}
	return pp, nil
}

type rawRule struct {
	FromEndpoints []struct {
		MatchLabels map[string]string `json:"matchLabels"`
	} `json:"fromEndpoints"`
	ToEndpoints []struct {
		MatchLabels map[string]string `json:"matchLabels"`
	} `json:"toEndpoints"`
	ToPorts []struct {
		Ports []struct {
			Port     string `json:"port"`
			Protocol string `json:"protocol"`
		} `json:"ports"`
	} `json:"toPorts"`
}

func (r rawRule) toRule(ingress bool) rule {
	var out rule
	peers := r.ToEndpoints
	if ingress {
		peers = r.FromEndpoints
	}
	for _, p := range peers {
		out.peers = append(out.peers, p.MatchLabels)
	}
	for _, tp := range r.ToPorts {
		for _, p := range tp.Ports {
			out.ports = append(out.ports, portRule{port: p.Port, proto: strings.ToUpper(p.Protocol)})
		}
	}
	return out
}

func evaluate(pp parsedPolicy, f hubble.Flow) Verdict {
	// Ingress: policy selects the destination.
	if endpointMatches(f.Destination, pp.endpoint) && len(pp.ingress) > 0 {
		if allowed, reason := ruleAllows(pp.ingress, f.Source, f); allowed {
			return Verdict{Flow: f, Applies: true, Allowed: true, Reason: reason}
		}
		return Verdict{Flow: f, Applies: true, Allowed: false, Reason: "no ingress rule permits " + endpointID(f.Source)}
	}
	// Egress: policy selects the source.
	if endpointMatches(f.Source, pp.endpoint) && len(pp.egress) > 0 {
		if allowed, reason := ruleAllows(pp.egress, f.Destination, f); allowed {
			return Verdict{Flow: f, Applies: true, Allowed: true, Reason: reason}
		}
		return Verdict{Flow: f, Applies: true, Allowed: false, Reason: "no egress rule permits " + endpointID(f.Destination)}
	}
	return Verdict{Flow: f, Applies: false}
}

func ruleAllows(rules []rule, peer hubble.Endpoint, f hubble.Flow) (bool, string) {
	for _, r := range rules {
		peerOK := len(r.peers) == 0
		for _, sel := range r.peers {
			if endpointMatches(peer, sel) {
				peerOK = true
				break
			}
		}
		if !peerOK {
			continue
		}
		if len(r.ports) == 0 {
			return true, "allowed by rule (any port)"
		}
		for _, pr := range r.ports {
			if portMatches(pr, f) {
				return true, "allowed on " + pr.proto + "/" + pr.port
			}
		}
	}
	return false, ""
}

func portMatches(pr portRule, f hubble.Flow) bool {
	if pr.proto != "" && pr.proto != f.L4.Protocol {
		return false
	}
	if pr.port == "" {
		return true
	}
	n, err := strconv.Atoi(pr.port)
	if err != nil {
		return false
	}
	return uint32(n) == f.L4.DstPort
}

// endpointMatches reports whether an endpoint's labels satisfy all matchLabels.
// Flow labels look like "k8s:app=frontend"; matchLabels keys may omit prefixes,
// so we match flexibly (exact, k8s: prefixed, or any-prefix suffix).
func endpointMatches(e hubble.Endpoint, sel selector) bool {
	if len(sel) == 0 {
		return false
	}
	for k, v := range sel {
		if !hasLabel(e, k, v) {
			return false
		}
	}
	return true
}

func hasLabel(e hubble.Endpoint, key, val string) bool {
	// Fast paths for the two most common selector keys.
	switch key {
	case "k8s:io.kubernetes.pod.namespace", "io.kubernetes.pod.namespace":
		if e.Namespace == val {
			return true
		}
	}
	want := key + "=" + val
	for _, l := range e.Labels {
		if l == want || l == "k8s:"+want || strings.HasSuffix(l, ":"+want) {
			return true
		}
		// bare "app=frontend" match when selector key has no prefix
		if strings.HasSuffix(l, "="+val) {
			if lk := labelKey(l); lk == key || strings.HasSuffix(lk, ":"+key) {
				return true
			}
		}
	}
	return false
}

func labelKey(l string) string {
	if i := strings.Index(l, "="); i >= 0 {
		return l[:i]
	}
	return l
}

func endpointID(e hubble.Endpoint) string {
	if e.Namespace == "" {
		if e.Workload != "" {
			return e.Workload
		}
		return "world"
	}
	return e.Namespace + "/" + e.Workload
}
