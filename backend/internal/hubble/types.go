// Package hubble normalizes Hubble flows into a compact JSON shape shared by
// the live gRPC client and the mock generator, so the frontend never needs to
// know which one is running.
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

// Header is one HTTP header (L7 deep-investigation).
type Header struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// L7 carries application-layer details when the flow was proxied.
type L7 struct {
	Type      string   `json:"type,omitempty"` // http | dns | kafka
	Method    string   `json:"method,omitempty"`
	URL       string   `json:"url,omitempty"`
	Protocol  string   `json:"protocol,omitempty"` // HTTP/1.1, HTTP/2
	Status    uint32   `json:"status,omitempty"`
	LatencyMs float64  `json:"latencyMs,omitempty"`
	Headers   []Header `json:"headers,omitempty"` // request/response HTTP headers
	DNSQuery  string   `json:"dnsQuery,omitempty"`
	DNSRcode  string   `json:"dnsRcode,omitempty"`
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
}

// Source is a stream of normalized flows. Implementations: live gRPC client
// (live.go) and the demo generator (internal/mock).
type Source interface {
	// Flows returns a channel that is closed when ctx is cancelled.
	Flows(ctx context.Context) (<-chan Flow, error)
}
