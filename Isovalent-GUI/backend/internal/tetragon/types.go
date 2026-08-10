// Package tetragon normalizes Tetragon runtime-security events into a compact
// JSON shape shared by the live gRPC client and the mock generator.
package tetragon

import (
	"context"
	"time"
)

// Event is the normalized representation of a Tetragon event.
type Event struct {
	Time      time.Time `json:"time"`
	Type      string    `json:"type"` // process_exec | process_exit | process_kprobe | process_tracepoint
	Namespace string    `json:"namespace,omitempty"`
	Pod       string    `json:"pod,omitempty"`
	Workload  string    `json:"workload,omitempty"`
	Node      string    `json:"node,omitempty"`
	Binary    string    `json:"binary,omitempty"`
	Args      string    `json:"args,omitempty"`
	Parent    string    `json:"parent,omitempty"`
	// Function is the hooked kernel function for kprobe/tracepoint events.
	Function string `json:"function,omitempty"`
	// Action is the enforcement action taken by the kernel, if any
	// (e.g. "SIGKILL", "OVERRIDE", "POST").
	Action string `json:"action,omitempty"`
	// Policy is the TracingPolicy that generated the event.
	Policy  string `json:"policy,omitempty"`
	Details string `json:"details,omitempty"`
}

// Source is a stream of normalized events.
type Source interface {
	Events(ctx context.Context) (<-chan Event, error)
}
