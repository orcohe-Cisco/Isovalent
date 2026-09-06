package k8s

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/isovalent-control/isovalent-control/backend/internal/hubble"
	"github.com/isovalent-control/isovalent-control/backend/internal/tetragon"
)

// MatchClause is one condition inside a policy that a specific event met.
// This is the difference between "policy X blocked it" and "the matchBinaries
// In [/usr/bin/nc] clause on selector 0 of the kprobe on tcp_connect blocked
// it" — only the second one tells you what to change.
type MatchClause struct {
	// Path is the JSON path of the clause inside spec, e.g.
	// "kprobes[0].selectors[1].matchBinaries[0]".
	Path string `json:"path"`
	// Kind is the clause type: matchBinaries, matchArgs, podSelector, …
	Kind string `json:"kind"`
	// Operator and Values are the clause as written.
	Operator string   `json:"operator,omitempty"`
	Values   []string `json:"values,omitempty"`
	// Matched is the value from the event that satisfied it.
	Matched string `json:"matched,omitempty"`
	// Excludable dimension this clause could be narrowed with, if any.
	Excludable string `json:"excludable,omitempty"`
	// ArgIndex is the hook argument position for matchArgs clauses.
	ArgIndex *int `json:"argIndex,omitempty"`
}

// Explanation is the full answer to "why did this fire?".
type Explanation struct {
	Policy    string        `json:"policy"`
	Namespace string        `json:"namespace,omitempty"`
	Kind      string        `json:"kind"`
	Hook      string        `json:"hook,omitempty"`
	Action    string        `json:"action,omitempty"`
	Clauses   []MatchClause `json:"clauses"`
	// Summary is a one-line human rendering, for the row that is not expanded.
	Summary string `json:"summary"`
	// Manifest is the policy as it exists in the cluster, so the drill-down
	// can show the YAML with the matched clause highlighted.
	Manifest json.RawMessage `json:"manifest,omitempty"`
	// Confident is false when the event could not be tied to a specific hook
	// (for example the agent reported a policy the console cannot read).
	Confident bool `json:"confident"`
}

// ExplainTracingMatch works out which part of a TracingPolicy an event met.
func ExplainTracingMatch(kind Kind, manifest json.RawMessage, e tetragon.Event) Explanation {
	ex := Explanation{
		Policy: e.Policy, Namespace: e.PolicyNamespace, Kind: string(kind),
		Hook: e.Function, Action: e.Action, Manifest: manifest,
	}
	var doc map[string]any
	if err := json.Unmarshal(manifest, &doc); err != nil {
		ex.Summary = "policy manifest could not be parsed"
		return ex
	}
	if ex.Namespace == "" {
		if meta, ok := doc["metadata"].(map[string]any); ok {
			ex.Namespace, _ = meta["namespace"].(string)
		}
	}
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		ex.Summary = "policy has no spec"
		return ex
	}

	// Spec-level selectors apply to every hook.
	if sel, ok := spec["podSelector"].(map[string]any); ok {
		ex.Clauses = append(ex.Clauses, labelClauses("podSelector", sel, e.Labels)...)
	}
	if sel, ok := spec["containerSelector"].(map[string]any); ok && e.Container != "" {
		ex.Clauses = append(ex.Clauses, labelClauses("containerSelector", sel, map[string]string{"name": e.Container})...)
	}

	for _, group := range []string{"kprobes", "tracepoints", "uprobes", "lsmhooks"} {
		items, ok := spec[group].([]any)
		if !ok {
			continue
		}
		for hi, raw := range items {
			hook := asMap(raw)
			if !hookMatches(hook, e) {
				continue
			}
			ex.Confident = true
			base := fmt.Sprintf("%s[%d]", group, hi)
			selectors, _ := hook["selectors"].([]any)
			if len(selectors) == 0 {
				ex.Clauses = append(ex.Clauses, MatchClause{
					Path: base, Kind: "hook",
					Matched: hookName(hook),
					Values:  []string{hookName(hook)},
				})
			}
			for si, rawSel := range selectors {
				sel := asMap(rawSel)
				clauses, matched := selectorClauses(fmt.Sprintf("%s.selectors[%d]", base, si), sel, e)
				if matched {
					ex.Clauses = append(ex.Clauses, clauses...)
					if a := actionOf(sel); a != "" {
						ex.Action = a
					}
				}
			}
			if ex.Hook == "" {
				ex.Hook = hookName(hook)
			}
		}
	}

	ex.Summary = summarize(ex, e)
	return ex
}

func hookName(hook map[string]any) string {
	if s, ok := hook["call"].(string); ok {
		return s
	}
	if s, ok := hook["event"].(string); ok {
		subsys, _ := hook["subsystem"].(string)
		if subsys != "" {
			return subsys + "/" + s
		}
		return s
	}
	return ""
}

func hookMatches(hook map[string]any, e tetragon.Event) bool {
	name := hookName(hook)
	if name == "" || e.Function == "" {
		return name != "" // no function on the event: report every hook
	}
	return name == e.Function || strings.HasSuffix(e.Function, "/"+name) || strings.HasSuffix(name, "/"+e.Function)
}

func actionOf(sel map[string]any) string {
	actions, _ := sel["matchActions"].([]any)
	for _, a := range actions {
		m := asMap(a)
		if s, ok := m["action"].(string); ok {
			return s
		}
	}
	return ""
}

// selectorClauses reports the clauses of one selector and whether the event
// satisfied all of them. A selector is a conjunction: a clause the event fails
// means the selector did not fire, so we do not report it as the cause.
func selectorClauses(path string, sel map[string]any, e tetragon.Event) ([]MatchClause, bool) {
	var out []MatchClause
	all := true

	check := func(kind, dim string, raw any, candidates []string, argIndex *int) {
		items, _ := raw.([]any)
		for i, it := range items {
			m := asMap(it)
			op, _ := m["operator"].(string)
			values := toStrings(m["values"])
			hit, matchedValue := opMatches(op, values, candidates)
			if !hit {
				all = false
				continue
			}
			c := MatchClause{
				Path:     fmt.Sprintf("%s.%s[%d]", path, kind, i),
				Kind:     kind,
				Operator: op,
				Values:   values,
				Matched:  matchedValue,
				ArgIndex: argIndex,
			}
			// Only positive clauses are worth offering an exclusion against:
			// narrowing a NotIn does not stop the match.
			if isPositiveOp(op) {
				c.Excludable = dim
			}
			out = append(out, c)
		}
	}

	check("matchBinaries", "binaries", sel["matchBinaries"], []string{e.Binary}, nil)
	check("matchParentBinaries", "parentBinaries", sel["matchParentBinaries"], []string{e.Parent}, nil)

	if args, ok := sel["matchArgs"].([]any); ok {
		for i, it := range args {
			m := asMap(it)
			idx := intOf(m["index"])
			op, _ := m["operator"].(string)
			values := toStrings(m["values"])
			var candidate []string
			if idx >= 0 && idx < len(e.ArgList) {
				candidate = []string{e.ArgList[idx]}
			}
			hit, matchedValue := opMatches(op, values, candidate)
			if !hit {
				all = false
				continue
			}
			ai := idx
			c := MatchClause{
				Path: fmt.Sprintf("%s.matchArgs[%d]", path, i), Kind: "matchArgs",
				Operator: op, Values: values, Matched: matchedValue, ArgIndex: &ai,
			}
			if isPositiveOp(op) {
				c.Excludable = "paths"
			}
			out = append(out, c)
		}
	}

	if caps, ok := sel["matchCapabilities"].([]any); ok && len(caps) > 0 {
		out = append(out, MatchClause{Path: path + ".matchCapabilities", Kind: "matchCapabilities"})
	}
	return out, all
}

func isPositiveOp(op string) bool {
	switch op {
	case "NotIn", "NotEqual", "NotPrefix", "NotPostfix", "NotDPort", "NotSPort":
		return false
	}
	return true
}

func opMatches(op string, values, candidates []string) (bool, string) {
	for _, c := range candidates {
		if c == "" {
			continue
		}
		for _, v := range values {
			switch op {
			case "", "In", "Equal", "Postfix":
				if op == "Postfix" {
					if strings.HasSuffix(c, v) {
						return true, c
					}
					continue
				}
				if c == v {
					return true, c
				}
			case "Prefix":
				if strings.HasPrefix(c, v) {
					return true, c
				}
			case "NotIn", "NotEqual":
				if c != v {
					return true, c
				}
			case "NotPrefix":
				if !strings.HasPrefix(c, v) {
					return true, c
				}
			default:
				// Numeric / capability operators are reported but not decided.
				return true, c
			}
		}
	}
	return false, ""
}

func labelClauses(kind string, sel map[string]any, have map[string]string) []MatchClause {
	var out []MatchClause
	if ml, ok := sel["matchLabels"].(map[string]any); ok {
		for k, v := range ml {
			vs, _ := v.(string)
			if have[k] == vs {
				out = append(out, MatchClause{
					Path: kind + ".matchLabels." + k, Kind: kind,
					Operator: "Equal", Values: []string{vs}, Matched: k + "=" + vs,
					Excludable: "podLabels",
				})
			}
		}
	}
	exprs, _ := sel["matchExpressions"].([]any)
	for i, raw := range exprs {
		m := asMap(raw)
		key, _ := m["key"].(string)
		op, _ := m["operator"].(string)
		values := toStrings(m["values"])
		hit, matched := opMatches(op, values, []string{have[key]})
		if !hit {
			continue
		}
		c := MatchClause{
			Path: fmt.Sprintf("%s.matchExpressions[%d]", kind, i), Kind: kind,
			Operator: op, Values: values, Matched: key + "=" + matched,
		}
		if isPositiveOp(op) {
			c.Excludable = "podLabels"
		}
		out = append(out, c)
	}
	return out
}

func summarize(ex Explanation, e tetragon.Event) string {
	if len(ex.Clauses) == 0 {
		if ex.Hook != "" {
			return fmt.Sprintf("hook %s fired; no selector clause narrowed it (the hook itself is the match)", ex.Hook)
		}
		return "policy reported by the agent, but no matching clause could be identified"
	}
	parts := make([]string, 0, 3)
	for _, c := range ex.Clauses {
		if len(parts) == 3 {
			break
		}
		v := c.Matched
		if v == "" && len(c.Values) > 0 {
			v = c.Values[0]
		}
		parts = append(parts, fmt.Sprintf("%s %s %s", c.Kind, strings.ToLower(orDefault(c.Operator, "matched")), v))
	}
	act := ex.Action
	if act == "" {
		act = "matched"
	}
	return fmt.Sprintf("%s on %s: %s", act, orDefault(ex.Hook, e.Function), strings.Join(parts, "; "))
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// --- network side --------------------------------------------------------

// ExplainNetworkMatch reports which ingress/egress rule of a Cilium policy
// covers a flow. Cilium attributes the verdict to a policy by name; this maps
// that back onto the specific rule so the operator sees the clause, not just
// the file.
func ExplainNetworkMatch(kind Kind, manifest json.RawMessage, f hubble.Flow, effect string) Explanation {
	ex := Explanation{Kind: string(kind), Manifest: manifest, Action: effect}
	var doc map[string]any
	if err := json.Unmarshal(manifest, &doc); err != nil {
		ex.Summary = "policy manifest could not be parsed"
		return ex
	}
	if meta, ok := doc["metadata"].(map[string]any); ok {
		ex.Policy, _ = meta["name"].(string)
		ex.Namespace, _ = meta["namespace"].(string)
	}
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		if specs, ok := doc["specs"].([]any); ok && len(specs) > 0 {
			spec = asMap(specs[0])
		}
	}
	if spec == nil {
		ex.Summary = "policy has no spec"
		return ex
	}
	direction := "egress"
	peer := f.Destination
	if f.Direction == "INGRESS" {
		direction = "ingress"
		peer = f.Source
	}
	rules, _ := spec[direction].([]any)
	for ri, raw := range rules {
		rule := asMap(raw)
		peerKey := "toEndpoints"
		if direction == "ingress" {
			peerKey = "fromEndpoints"
		}
		peers, _ := rule[peerKey].([]any)
		for pi, rawPeer := range peers {
			sel := asMap(rawPeer)
			if ml, ok := sel["matchLabels"].(map[string]any); ok && labelsCover(ml, peer.Labels, peer.Namespace) {
				ex.Confident = true
				ex.Clauses = append(ex.Clauses, MatchClause{
					Path:    fmt.Sprintf("%s[%d].%s[%d].matchLabels", direction, ri, peerKey, pi),
					Kind:    peerKey,
					Matched: endpointLabel(peer),
				})
			}
		}
		for pi, rawPorts := range toAnyList(rule["toPorts"]) {
			ports := asMap(rawPorts)
			for _, rawPort := range toAnyList(ports["ports"]) {
				pm := asMap(rawPort)
				port, _ := pm["port"].(string)
				if n, err := strconv.Atoi(port); err == nil && uint32(n) == f.L4.DstPort {
					ex.Confident = true
					ex.Clauses = append(ex.Clauses, MatchClause{
						Path:     fmt.Sprintf("%s[%d].toPorts[%d]", direction, ri, pi),
						Kind:     "toPorts",
						Operator: "port",
						Values:   []string{port},
						Matched:  port,
					})
				}
			}
		}
	}
	if len(ex.Clauses) == 0 {
		ex.Summary = fmt.Sprintf("%s %s: no %s rule covers %s — default-deny applies once a policy selects the endpoint",
			ex.Policy, effect, direction, endpointLabel(peer))
		return ex
	}
	var parts []string
	for _, c := range ex.Clauses {
		parts = append(parts, c.Path)
	}
	ex.Summary = fmt.Sprintf("%s %s via %s", ex.Policy, effect, strings.Join(parts, ", "))
	return ex
}

func toAnyList(v any) []any {
	arr, _ := v.([]any)
	return arr
}

func labelsCover(want map[string]any, have []string, namespace string) bool {
	if len(want) == 0 {
		return false
	}
	for k, v := range want {
		vs, _ := v.(string)
		if k == "io.kubernetes.pod.namespace" || k == "k8s:io.kubernetes.pod.namespace" {
			if namespace == vs {
				continue
			}
			return false
		}
		found := false
		for _, l := range have {
			if l == k+"="+vs || l == "k8s:"+k+"="+vs || strings.HasSuffix(l, ":"+k+"="+vs) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func endpointLabel(e hubble.Endpoint) string {
	if e.Namespace == "" {
		if e.Workload != "" {
			return e.Workload
		}
		return "world"
	}
	return e.Namespace + "/" + e.Workload
}
