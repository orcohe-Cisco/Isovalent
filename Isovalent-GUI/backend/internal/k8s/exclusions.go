package k8s

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Exclusions is a set of things a TracingPolicy should stop matching on.
//
// Every field maps onto a documented Tetragon mechanism. Nothing is offered
// here that Tetragon cannot actually express — an exclusion that silently
// does nothing is worse than no exclusion, because you stop looking.
type Exclusions struct {
	// Binaries -> matchBinaries NotIn. Filtered in the kernel; the event is
	// never generated. The most effective of the five.
	Binaries []string `json:"binaries,omitempty"`
	// ParentBinaries -> matchParentBinaries NotIn. Exempts a whole subtree.
	ParentBinaries []string `json:"parentBinaries,omitempty"`
	// Paths -> matchArgs NotPrefix on the argument index the value came from.
	Paths []string `json:"paths,omitempty"`
	// PathArgIndex is the hook argument position the paths apply to. The
	// Exclusions tab fills it from the observed hit, because a NotPrefix on
	// the wrong index matches nothing.
	PathArgIndex *int `json:"pathArgIndex,omitempty"`
	// PodLabels -> podSelector matchExpressions NotIn ("key=value" form).
	PodLabels []string `json:"podLabels,omitempty"`
	// Containers -> containerSelector matchExpressions NotIn (the istio-proxy
	// case: exempt a sidecar by name across every pod).
	Containers []string `json:"containers,omitempty"`
}

// Empty reports whether nothing would be applied.
func (e Exclusions) Empty() bool {
	return len(e.Binaries) == 0 && len(e.ParentBinaries) == 0 && len(e.Paths) == 0 &&
		len(e.PodLabels) == 0 && len(e.Containers) == 0
}

// ExclusionNote records what landed and what did not.
type ExclusionNote struct {
	Dimension string `json:"dimension"`
	Applied   bool   `json:"applied"`
	Detail    string `json:"detail"`
}

// ApplyExclusions injects exclusions into a live TracingPolicy manifest and
// returns the mutated manifest plus notes. Values already excluded are merged
// rather than duplicated, so approving the same exclusion twice is a no-op.
func ApplyExclusions(manifest json.RawMessage, ex Exclusions) (json.RawMessage, []ExclusionNote, error) {
	var doc map[string]any
	if err := json.Unmarshal(manifest, &doc); err != nil {
		return nil, nil, fmt.Errorf("parse manifest: %w", err)
	}
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		return nil, nil, fmt.Errorf("manifest has no spec")
	}
	var notes []ExclusionNote
	note := func(dim string, applied bool, detail string) {
		notes = append(notes, ExclusionNote{Dimension: dim, Applied: applied, Detail: detail})
	}

	if len(ex.PodLabels) > 0 {
		sel := asMap(spec["podSelector"])
		exprs, _ := sel["matchExpressions"].([]any)
		added := 0
		for _, pair := range ex.PodLabels {
			k, v, ok := splitPair(pair)
			if !ok {
				continue
			}
			if hasExpr(exprs, k, "NotIn", v) {
				continue
			}
			exprs = append(exprs, map[string]any{
				"key": k, "operator": "NotIn", "values": []any{v},
			})
			added++
		}
		sel["matchExpressions"] = exprs
		spec["podSelector"] = sel
		note("podLabels", added > 0, fmt.Sprintf("podSelector NotIn on %d label(s)", added))
	}

	if len(ex.Containers) > 0 {
		sel := asMap(spec["containerSelector"])
		exprs, _ := sel["matchExpressions"].([]any)
		merged := mergeNotIn(exprs, "name", ex.Containers)
		sel["matchExpressions"] = merged
		spec["containerSelector"] = sel
		note("containers", true, fmt.Sprintf("containerSelector NotIn on %d name(s)", len(ex.Containers)))
	}

	hooks := hookList(spec)
	if len(hooks) == 0 && (len(ex.Binaries) > 0 || len(ex.ParentBinaries) > 0 || len(ex.Paths) > 0) {
		note("binaries", false, "policy declares no kprobe/tracepoint/lsm hooks to filter")
		out, err := json.Marshal(doc)
		return out, notes, err
	}

	binHits, parentHits, pathHits, pathMiss := 0, 0, 0, 0
	for _, hook := range hooks {
		selectors, ok := hook["selectors"].([]any)
		if !ok || len(selectors) == 0 {
			selectors = []any{map[string]any{}}
			hook["selectors"] = selectors
		}
		idx := ex.PathArgIndex
		if idx == nil {
			if guessed := pathArgIndex(hook); guessed >= 0 {
				g := guessed
				idx = &g
			}
		}
		for _, raw := range selectors {
			sel := asMap(raw)
			if len(ex.Binaries) > 0 {
				sel["matchBinaries"] = mergeBinaryFilter(sel["matchBinaries"], ex.Binaries)
				binHits++
			}
			if len(ex.ParentBinaries) > 0 {
				sel["matchParentBinaries"] = mergeBinaryFilter(sel["matchParentBinaries"], ex.ParentBinaries)
				parentHits++
			}
			if len(ex.Paths) > 0 {
				if idx == nil {
					pathMiss++
				} else if applyPathFilter(sel, *idx, ex.Paths) {
					pathHits++
				} else {
					pathMiss++
				}
			}
		}
		// Selectors are stored by value in the []any, so write them back.
		for i, raw := range selectors {
			selectors[i] = asMap(raw)
		}
	}

	if len(ex.Binaries) > 0 {
		note("binaries", binHits > 0, fmt.Sprintf("matchBinaries NotIn added to %d selector(s)", binHits))
	}
	if len(ex.ParentBinaries) > 0 {
		note("parentBinaries", parentHits > 0, fmt.Sprintf("matchParentBinaries NotIn added to %d selector(s)", parentHits))
	}
	if len(ex.Paths) > 0 {
		detail := fmt.Sprintf("matchArgs NotPrefix added to %d selector(s)", pathHits)
		if pathMiss > 0 {
			detail += fmt.Sprintf("; %d skipped (no file/string argument, or already constrained)", pathMiss)
		}
		note("paths", pathHits > 0, detail)
	}

	out, err := json.Marshal(doc)
	return out, notes, err
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func splitPair(s string) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}

func hasExpr(exprs []any, key, op, value string) bool {
	for _, e := range exprs {
		m, _ := e.(map[string]any)
		if m["key"] != key || m["operator"] != op {
			continue
		}
		for _, v := range toStrings(m["values"]) {
			if v == value {
				return true
			}
		}
	}
	return false
}

// mergeNotIn folds values into an existing NotIn expression for key, or adds
// one. Two NotIn expressions on the same key would be ANDed, which is correct
// but unreadable; one merged list is the same semantics and reviewable.
func mergeNotIn(exprs []any, key string, values []string) []any {
	for _, e := range exprs {
		m, _ := e.(map[string]any)
		if m["key"] == key && m["operator"] == "NotIn" {
			m["values"] = toAny(union(toStrings(m["values"]), values))
			return exprs
		}
	}
	return append(exprs, map[string]any{
		"key": key, "operator": "NotIn", "values": toAny(union(nil, values)),
	})
}

func mergeBinaryFilter(existing any, values []string) []any {
	items, _ := existing.([]any)
	for _, it := range items {
		m, _ := it.(map[string]any)
		if m["operator"] == "NotIn" {
			m["values"] = toAny(union(toStrings(m["values"]), values))
			return items
		}
	}
	return append(items, map[string]any{
		"operator": "NotIn",
		"values":   toAny(union(nil, values)),
		// followChildren:false keeps the exclusion to the named binary rather
		// than everything it ever spawns, which is the safer default.
		"followChildren": false,
	})
}

// applyPathFilter adds a NotPrefix matchArgs at idx. It refuses to touch an
// index that already carries a matchArgs clause: two matchArgs on the same
// index contradict each other and Tetragon resolves that in a way nobody
// predicts correctly.
func applyPathFilter(sel map[string]any, idx int, paths []string) bool {
	args, _ := sel["matchArgs"].([]any)
	for _, a := range args {
		m, _ := a.(map[string]any)
		if intOf(m["index"]) == idx {
			if m["operator"] == "NotPrefix" {
				m["values"] = toAny(union(toStrings(m["values"]), paths))
				return true
			}
			return false
		}
	}
	sel["matchArgs"] = append(args, map[string]any{
		"index": idx, "operator": "NotPrefix", "values": toAny(union(nil, paths)),
	})
	return true
}

// pathArgIndex finds the first hook argument declared as a file/path/string,
// which is the only kind a NotPrefix can filter.
func pathArgIndex(hook map[string]any) int {
	args, _ := hook["args"].([]any)
	for _, a := range args {
		m, _ := a.(map[string]any)
		switch m["type"] {
		case "file", "path", "string", "fd", "dentry":
			return intOf(m["index"])
		}
	}
	return -1
}

func hookList(spec map[string]any) []map[string]any {
	var out []map[string]any
	for _, group := range []string{"kprobes", "tracepoints", "uprobes", "lsmhooks"} {
		items, ok := spec[group].([]any)
		if !ok {
			continue
		}
		for i, it := range items {
			m := asMap(it)
			items[i] = m
			out = append(out, m)
		}
	}
	return out
}

func intOf(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return -1
}

func toStrings(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func toAny(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

func union(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(append([]string{}, a...), b...) {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
