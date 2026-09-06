// Package tetragon normalizes Tetragon runtime-security events into a compact
// JSON shape the rest of the console works with.
package tetragon

import (
	"context"
	"strconv"
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
	Policy string `json:"policy,omitempty"`
	// PolicyNamespace is set for TracingPolicyNamespaced.
	PolicyNamespace string `json:"policyNamespace,omitempty"`
	Details         string `json:"details,omitempty"`
	// Args are the hook arguments in order. Kept indexed (rather than only
	// joined into Details) because a matchArgs exclusion is written against
	// an argument *index*, so the UI needs to know which position a value
	// came from to propose a correct exclusion.
	ArgList []string `json:"argList,omitempty"`

	// UID is the user the process ran as. A pointer because 0 (root) is the
	// most interesting value there is, and must not be confused with absent.
	UID *uint32 `json:"uid,omitempty"`
	// User is the resolved account name when Tetragon could look it up.
	User string `json:"user,omitempty"`
	// Labels are the pod's labels. Kept so exclusions can be expressed as a
	// label selector the user picks from real values rather than types.
	Labels map[string]string `json:"labels,omitempty"`
	// Container is the container name within the pod, for containerSelector
	// exclusions (the istio-proxy case).
	Container string `json:"container,omitempty"`
}

// UserLabel renders the process user for display and for exclusion pickers:
// the resolved name when known, "root" for UID 0, else "uid:<n>".
func (e Event) UserLabel() string {
	if e.User != "" {
		return e.User
	}
	if e.UID == nil {
		return ""
	}
	if *e.UID == 0 {
		return "root"
	}
	return "uid:" + strconv.FormatUint(uint64(*e.UID), 10)
}

// Source is a stream of normalized events.
type Source interface {
	Events(ctx context.Context) (<-chan Event, error)
}
