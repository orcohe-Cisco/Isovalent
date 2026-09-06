// Package hubble normalizes Hubble flows into a compact JSON shape the rest of
// the console works with, so nothing above this layer has to know what a
// flowpb.Flow looks like.
package hubble

import (
	"context"
	"time"
)

// Endpoint identifies one side of a flow.
type Endpoint struct {
	Namespace string   `json:"namespace,omitempty"`
	PodName   string   `json:"podName,omitempty"`
	Workload  string   `json:"workload,omitempty"`
	Identity  uint32   `json:"identity,omitempty"`
	Labels    []string `json:"labels,omitempty"`
}

// L4 carries transport-layer details.
type L4 struct {
	Protocol string `json:"protocol,omitempty"` // TCP | UDP | ICMP
	SrcPort  uint32 `json:"srcPort,omitempty"`
	DstPort  uint32 `json:"dstPort,omitempty"`
}

// L7 carries application-layer details when the flow was proxied.
type L7 struct {
	Type      string  `json:"type,omitempty"` // http | dns | kafka
	Method    string  `json:"method,omitempty"`
	URL       string  `json:"url,omitempty"`
	Status    uint32  `json:"status,omitempty"`
	LatencyMs float64 `json:"latencyMs,omitempty"`
	DNSQuery  string  `json:"dnsQuery,omitempty"`
	DNSRcode  string  `json:"dnsRcode,omitempty"`
}

// PolicyRef attributes a flow to the Cilium policy that allowed or denied it.
// This is what turns "something was dropped" into "this rule dropped it",
// which is the only version of the answer anyone can act on.
type PolicyRef struct {
	Name      string   `json:"name"`
	Namespace string   `json:"namespace,omitempty"`
	Kind      string   `json:"kind,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	// Direction is ingress or egress.
	Direction string `json:"direction"`
	// Effect is allowed or denied.
	Effect string `json:"effect"`
}

// Flow is the normalized representation of a Hubble flow.
type Flow struct {
	Time        time.Time `json:"time"`
	Verdict     string    `json:"verdict"` // FORWARDED | DROPPED | AUDIT | ERROR
	DropReason  string    `json:"dropReason,omitempty"`
	Direction   string    `json:"direction,omitempty"` // INGRESS | EGRESS
	Source      Endpoint  `json:"source"`
	Destination Endpoint  `json:"destination"`
	L4          L4        `json:"l4"`
	L7          *L7       `json:"l7,omitempty"`
	Node        string    `json:"node,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	// Policies attributes the verdict to named CiliumNetworkPolicies when the
	// agent reports it (Cilium 1.15+ populates *AllowedBy / *DeniedBy).
	Policies []PolicyRef `json:"policies,omitempty"`
	// IsReply marks return traffic, which is noise in most investigations.
	IsReply bool `json:"isReply,omitempty"`
}

// PolicyNames returns the distinct policy names attributed to this flow.
func (f Flow) PolicyNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range f.Policies {
		key := p.Namespace + "/" + p.Name
		if p.Namespace == "" {
			key = p.Name
		}
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

// Source is a stream of normalized flows. The only implementation is the live
// gRPC client in live.go — there is deliberately no generated-data path.
type Source interface {
	// Flows returns a channel that is closed when ctx is cancelled.
	Flows(ctx context.Context) (<-chan Flow, error)
}
