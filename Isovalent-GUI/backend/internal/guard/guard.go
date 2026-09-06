// Package guard stops the console from writing a policy that could take the
// console (or another protected component) down.
//
// The failure mode this exists for is specific and easy to hit during a demo:
// you build a cluster-wide TracingPolicy that kills, say, any process reading
// a service-account token, apply it, and the first thing it kills is the
// console's own backend — which is the process that would have let you undo
// it. Runtime enforcement has no undo button, so the check has to happen
// before the apply, not after.
//
// The rule is deliberately blunt: a policy authored here may not *select* a
// protected namespace, and may not be cluster-wide-and-enforcing without
// carrying an explicit exemption for them. We would rather reject a policy
// that was actually safe than let one through that is not.
package guard

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// SelfLabel is stamped on every workload of the console by the deployment
// manifests, and is what the auto-injected exemption keys off.
const SelfLabel = "app.kubernetes.io/part-of"

// SelfLabelValue is the value of SelfLabel on console workloads.
const SelfLabelValue = "isovalent-control"

// Guard holds the protected set.
type Guard struct {
	namespaces map[string]bool
	order      []string
}

// New builds a Guard over the given protected namespaces.
func New(namespaces []string) *Guard {
	g := &Guard{namespaces: map[string]bool{}}
	for _, ns := range namespaces {
		ns = strings.TrimSpace(ns)
		if ns == "" || g.namespaces[ns] {
			continue
		}
		g.namespaces[ns] = true
		g.order = append(g.order, ns)
	}
	sort.Strings(g.order)
	return g
}

// Protected reports the protected namespaces, sorted.
func (g *Guard) Protected() []string {
	out := make([]string, len(g.order))
	copy(out, g.order)
	return out
}

// IsProtected reports whether ns may not be targeted.
func (g *Guard) IsProtected(ns string) bool { return g != nil && g.namespaces[ns] }

// Violation describes why a manifest was rejected.
type Violation struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

func (v Violation) Error() string { return v.Field + ": " + v.Reason }

// CheckManifest rejects a policy manifest that targets a protected namespace.
//
// It looks at the three places a namespace can be named across the CRDs we
// manage: metadata.namespace, a Tetragon podSelector/namespace expression, and
// a Cilium endpointSelector matching io.kubernetes.pod.namespace.
func (g *Guard) CheckManifest(manifest json.RawMessage) error {
	if g == nil {
		return nil
	}
	var doc map[string]any
	if err := json.Unmarshal(manifest, &doc); err != nil {
		return nil // validation elsewhere reports the parse error
	}
	meta, _ := doc["metadata"].(map[string]any)
	if meta != nil {
		if ns, _ := meta["namespace"].(string); g.IsProtected(ns) {
			return Violation{
				Field:  "metadata.namespace",
				Reason: fmt.Sprintf("%q is protected: policies authored in this console cannot target the console's own components", ns),
			}
		}
	}
	for _, ns := range namespacesNamedIn(doc) {
		if g.IsProtected(ns) {
			return Violation{
				Field:  "spec",
				Reason: fmt.Sprintf("selector names the protected namespace %q", ns),
			}
		}
	}
	return nil
}

// namespacesNamedIn walks the manifest for values that positively select a
// namespace. Values under a NotIn/NotEquals/DoesNotExist operator are
// exclusions, not selections, so they are skipped.
func namespacesNamedIn(doc map[string]any) []string {
	var out []string
	var walk func(node any, key string)
	walk = func(node any, key string) {
		switch v := node.(type) {
		case map[string]any:
			// matchExpressions entry: {key, operator, values}
			if k, ok := v["key"].(string); ok {
				if isNamespaceKey(k) && isPositive(v["operator"]) {
					out = append(out, stringsOf(v["values"])...)
				}
			}
			for kk, vv := range v {
				if kk == "matchLabels" || kk == "matchNames" {
					if m, ok := vv.(map[string]any); ok {
						for mk, mv := range m {
							if isNamespaceKey(mk) {
								if s, ok := mv.(string); ok {
									out = append(out, s)
								}
							}
						}
						continue
					}
					if arr, ok := vv.([]any); ok {
						out = append(out, stringsOf(arr)...)
						continue
					}
				}
				walk(vv, kk)
			}
		case []any:
			for _, item := range v {
				walk(item, key)
			}
		}
	}
	walk(doc, "")
	return out
}

func isPositive(op any) bool {
	s, _ := op.(string)
	switch s {
	case "NotIn", "NotEquals", "DoesNotExist", "NotPrefix", "NotPostfix":
		return false
	}
	return true
}

func isNamespaceKey(k string) bool {
	switch k {
	case "namespace", "namespaces",
		"io.kubernetes.pod.namespace",
		"k8s:io.kubernetes.pod.namespace",
		"kubernetes.io/metadata.name":
		return true
	}
	return false
}

func stringsOf(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// Exempt injects a "not the console's own pods" clause into a Tetragon spec's
// podSelector, so even a cluster-wide enforcing policy leaves the console
// alone. Returns true if the spec was modified.
//
// This is belt-and-braces on top of CheckManifest: CheckManifest catches a
// policy that names a protected namespace, and this catches one that selects
// the console by accident because it selects everything.
func Exempt(manifest json.RawMessage) (json.RawMessage, bool, error) {
	var doc map[string]any
	if err := json.Unmarshal(manifest, &doc); err != nil {
		return manifest, false, err
	}
	kind, _ := doc["kind"].(string)
	if kind != "TracingPolicy" && kind != "TracingPolicyNamespaced" {
		return manifest, false, nil
	}
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		return manifest, false, nil
	}
	sel, _ := spec["podSelector"].(map[string]any)
	if sel == nil {
		sel = map[string]any{}
	}
	exprs, _ := sel["matchExpressions"].([]any)
	for _, e := range exprs {
		m, _ := e.(map[string]any)
		if m["key"] == SelfLabel && m["operator"] == "NotIn" {
			return manifest, false, nil // already exempt
		}
	}
	sel["matchExpressions"] = append(exprs, map[string]any{
		"key":      SelfLabel,
		"operator": "NotIn",
		"values":   []any{SelfLabelValue},
	})
	spec["podSelector"] = sel
	out, err := json.Marshal(doc)
	if err != nil {
		return manifest, false, err
	}
	return out, true, nil
}
