package k8s

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TracingAction is the enforcement mode of a TracingPolicy.
type TracingAction string

const (
	// ActionMonitor observes only (Tetragon "Post").
	ActionMonitor TracingAction = "monitor"
	// ActionEnforce kills the offending process (Tetragon "Sigkill").
	ActionEnforce TracingAction = "enforce"
)

const actionAnnotation = "isovalent-control.io/action"
const categoryLabel = "isovalent-control.io/category"

// TracingPolicyInfo is the UI-facing summary of a TracingPolicy.
type TracingPolicyInfo struct {
	Name        string        `json:"name"`
	Namespace   string        `json:"namespace,omitempty"`
	Kind        Kind          `json:"kind"`
	Category    string        `json:"category,omitempty"`
	Description string        `json:"description,omitempty"`
	Action      TracingAction `json:"action"`
	Hooks       []string      `json:"hooks,omitempty"`
	Managed     bool          `json:"managed"`
}

// DescribeTracingPolicy extracts UI metadata (category, action, hooks) from a
// TracingPolicy manifest.
func DescribeTracingPolicy(kind Kind, manifest json.RawMessage) TracingPolicyInfo {
	var m struct {
		Metadata struct {
			Name        string            `json:"name"`
			Namespace   string            `json:"namespace"`
			Labels      map[string]string `json:"labels"`
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
		Spec map[string]any `json:"spec"`
	}
	_ = json.Unmarshal(manifest, &m)
	info := TracingPolicyInfo{
		Name:      m.Metadata.Name,
		Namespace: m.Metadata.Namespace,
		Kind:      kind,
		Category:  m.Metadata.Labels[categoryLabel],
		Managed:   m.Metadata.Labels["app.kubernetes.io/managed-by"] == "isovalent-control",
		Action:    detectAction(manifest),
	}
	if m.Metadata.Annotations != nil {
		info.Description = m.Metadata.Annotations["isovalent-control.io/description"]
	}
	info.Hooks = extractHooks(m.Spec)
	return info
}

func extractHooks(spec map[string]any) []string {
	var hooks []string
	for _, group := range []string{"kprobes", "tracepoints", "uprobes", "lsmhooks"} {
		items, ok := spec[group].([]any)
		if !ok {
			continue
		}
		for _, it := range items {
			m, _ := it.(map[string]any)
			if call, ok := m["call"].(string); ok {
				hooks = append(hooks, call)
			} else if ev, ok := m["event"].(string); ok {
				hooks = append(hooks, ev)
			}
		}
	}
	return hooks
}

// detectAction reports enforce if any matchActions action is Sigkill/Override.
func detectAction(manifest json.RawMessage) TracingAction {
	s := string(manifest)
	if strings.Contains(s, "\"Sigkill\"") || strings.Contains(s, "\"Override\"") {
		return ActionEnforce
	}
	return ActionMonitor
}

// SetTracingAction rewrites every matchActions[].action in the manifest to the
// requested mode (monitor=Post, enforce=Sigkill) and updates the action
// annotation. Returns the mutated manifest ready to re-apply.
func SetTracingAction(manifest json.RawMessage, action TracingAction) (json.RawMessage, error) {
	target := "Post"
	if action == ActionEnforce {
		target = "Sigkill"
	}
	var doc map[string]any
	if err := json.Unmarshal(manifest, &doc); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		return nil, fmt.Errorf("manifest has no spec")
	}
	changed := 0
	for _, group := range []string{"kprobes", "tracepoints", "uprobes", "lsmhooks"} {
		items, ok := spec[group].([]any)
		if !ok {
			continue
		}
		for _, it := range items {
			hook, _ := it.(map[string]any)
			selectors, _ := hook["selectors"].([]any)
			for _, sel := range selectors {
				s, _ := sel.(map[string]any)
				actions, _ := s["matchActions"].([]any)
				for _, act := range actions {
					a, _ := act.(map[string]any)
					if _, ok := a["action"]; ok {
						a["action"] = target
						changed++
					}
				}
				// If a selector had no matchActions, add one so the toggle is
				// meaningful (only when enforcing, to avoid noisy no-op Posts).
				if len(actions) == 0 && action == ActionEnforce {
					s["matchActions"] = []any{map[string]any{"action": target}}
					changed++
				}
			}
		}
	}
	// Update annotation so the UI reflects the new state without re-reading.
	meta, _ := doc["metadata"].(map[string]any)
	if meta != nil {
		ann, _ := meta["annotations"].(map[string]any)
		if ann == nil {
			ann = map[string]any{}
			meta["annotations"] = ann
		}
		ann[actionAnnotation] = string(action)
	}
	if changed == 0 {
		return nil, fmt.Errorf("policy has no matchActions to toggle")
	}
	return json.Marshal(doc)
}
