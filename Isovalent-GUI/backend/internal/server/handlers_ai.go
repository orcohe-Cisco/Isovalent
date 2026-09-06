package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/isovalent-control/isovalent-control/backend/internal/ai"
	"github.com/isovalent-control/isovalent-control/backend/internal/audit"
	"github.com/isovalent-control/isovalent-control/backend/internal/auth"
	"github.com/isovalent-control/isovalent-control/backend/internal/hits"
	"github.com/isovalent-control/isovalent-control/backend/internal/k8s"
)

func (s *Server) getAIStatus(w http.ResponseWriter, _ *http.Request) {
	if s.ai == nil {
		writeJSON(w, http.StatusOK, ai.Status{Providers: []string{"anthropic", "gemini", "openai", "disabled"}})
		return
	}
	writeJSON(w, http.StatusOK, s.ai.Status())
}

// aiContext is exactly what gets sent to the model. It is returned to the UI
// alongside the answer so the operator can see what the assistant was told —
// an agent whose inputs you cannot inspect is an agent you cannot audit.
type aiContext struct {
	Cluster   string `json:"cluster"`
	Generated string `json:"generatedAt"`
	Overview  struct {
		FlowsPerSec  float64 `json:"flowsPerSec"`
		DropsPerSec  float64 `json:"dropsPerSec"`
		HTTPErrorPct float64 `json:"httpErrorPct"`
		TotalKills   int64   `json:"runtimeEnforcementActions"`
	} `json:"overview"`
	Policies    []aiPolicy    `json:"policies"`
	TopDrops    []aiDropGroup `json:"topNetworkDrops"`
	Protected   []string      `json:"protectedNamespaces"`
	Constraints []string      `json:"constraints"`
}

type aiPolicy struct {
	Name       string           `json:"name"`
	Namespace  string           `json:"namespace,omitempty"`
	Mode       string           `json:"mode"`
	Hooks      []string         `json:"hooks,omitempty"`
	Hits       int64            `json:"hits"`
	Enforced   int64            `json:"enforcedHits"`
	RatePerMin float64          `json:"hitsPerMinute"`
	Top        map[string][]kv  `json:"topMatchedValues,omitempty"`
	Unique     map[string]int   `json:"uniqueValues,omitempty"`
	Samples    []map[string]any `json:"samples,omitempty"`
}

type kv struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

type aiDropGroup struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Dropped     int64  `json:"dropped"`
	Forwarded   int64  `json:"forwarded"`
}

func (s *Server) buildAIContext(req *http.Request) aiContext {
	var c aiContext
	c.Cluster = s.cfg.ClusterName
	c.Generated = time.Now().UTC().Format(time.RFC3339)
	o := s.agg.Overview()
	c.Overview.FlowsPerSec = round2(o.FlowRate)
	c.Overview.DropsPerSec = round2(o.DropRate)
	c.Overview.HTTPErrorPct = round2(o.HTTPErrPct)
	c.Overview.TotalKills = o.TotalKills
	c.Protected = s.guard.Protected()
	c.Constraints = []string{
		"Policies in the protected namespaces listed above cannot be modified and must never be proposed as targets.",
		"Enforcement (Sigkill) is irreversible for the killed process; prefer monitor plus an exclusion unless the evidence is unambiguous.",
		"Only the exclusion dimensions binaries, parentBinaries, paths, podLabels and containers can be written into a TracingPolicy.",
	}

	index := map[string]k8s.TracingPolicyInfo{}
	for _, kind := range []k8s.Kind{k8s.KindTP, k8s.KindTPN} {
		items, err := s.policies.List(req.Context(), kind, "")
		if err != nil {
			continue
		}
		for _, p := range items {
			info := k8s.DescribeTracingPolicy(kind, p.Manifest)
			key := p.Name
			if p.Namespace != "" {
				key = p.Namespace + "/" + p.Name
			}
			index[key] = info
		}
	}

	if s.hits != nil {
		for _, sum := range s.hits.Summaries() {
			key := sum.Policy
			if sum.Namespace != "" {
				key = sum.Namespace + "/" + sum.Policy
			}
			p := aiPolicy{
				Name: sum.Policy, Namespace: sum.Namespace,
				Mode: string(index[key].Action), Hooks: index[key].Hooks,
				Hits: sum.Total, Enforced: sum.Enforced, RatePerMin: round2(sum.RatePerMin),
				Unique: sum.Unique, Top: map[string][]kv{},
			}
			if detail, ok := s.hits.Detail(sum.Namespace, sum.Policy); ok {
				for _, dim := range hits.Dimensions {
					if !hits.Excludable(dim) && dim != hits.DimUser && dim != hits.DimNamespace {
						continue
					}
					vals := detail.Values[dim]
					if len(vals) > 6 {
						vals = vals[:6]
					}
					for _, v := range vals {
						p.Top[dim] = append(p.Top[dim], kv{Value: v.Value, Count: v.Count})
					}
				}
				for i, ev := range detail.Samples {
					if i >= 3 {
						break
					}
					p.Samples = append(p.Samples, map[string]any{
						"binary": ev.Binary, "args": ev.Args, "hook": ev.Function,
						"namespace": ev.Namespace, "workload": ev.Workload,
						"user": ev.UserLabel(), "action": ev.Action,
					})
				}
			}
			c.Policies = append(c.Policies, p)
		}
	}

	_, edges := s.agg.ServiceMap()
	for _, e := range edges {
		if e.Dropped == 0 {
			continue
		}
		c.TopDrops = append(c.TopDrops, aiDropGroup{
			Source: e.Source, Destination: e.Target, Dropped: e.Dropped, Forwarded: e.Forwarded,
		})
	}
	if len(c.TopDrops) > 20 {
		c.TopDrops = c.TopDrops[:20]
	}
	return c
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}

// postAIAnalyze asks the configured model to review the posture.
func (s *Server) postAIAnalyze(w http.ResponseWriter, req *http.Request) {
	if s.ai == nil || !s.ai.Enabled() {
		reason := "AI assistant is not configured"
		if s.ai != nil {
			reason = s.ai.Status().Reason
		}
		writeErr(w, http.StatusServiceUnavailable, errors.New(reason))
		return
	}
	var body struct {
		Question string `json:"question"`
	}
	_ = json.NewDecoder(io.LimitReader(req.Body, 1<<16)).Decode(&body)

	ctxData := s.buildAIContext(req)
	raw, err := json.Marshal(ctxData)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	resp, err := s.ai.Suggest(req.Context(), raw, body.Question)
	if err != nil {
		s.record(req, "ai.analyze", s.cfg.ClusterName, audit.OutcomeError, err.Error(), nil, nil)
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	s.record(req, "ai.analyze", s.cfg.ClusterName, audit.OutcomeSuccess,
		"asked "+resp.Provider+"/"+resp.Model, nil, nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"response": resp,
		"context":  ctxData,
	})
}

// postAIApply turns one approved suggestion into a real change.
//
// It deliberately does not accept a manifest from the model. The suggestion
// names a policy and an action, and the server rebuilds the change itself from
// the same code path the UI buttons use — so an AI-proposed change is subject
// to the identical validation, self-protection and audit as a human one.
func (s *Server) postAIApply(w http.ResponseWriter, req *http.Request) {
	id := auth.FromContext(req.Context())
	var body struct {
		Kind       string         `json:"kind"` // enforce | monitor | exclude | disable
		Policy     string         `json:"policy"`
		Namespace  string         `json:"namespace"`
		Exclusions k8s.Exclusions `json:"exclusions"`
	}
	if err := json.NewDecoder(io.LimitReader(req.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.Policy == "" {
		writeErr(w, http.StatusBadRequest, errors.New("policy is required"))
		return
	}
	ns := body.Namespace
	kind := k8s.KindTP
	if ns != "" {
		kind = k8s.KindTPN
	}
	if ns == "" && !id.IsAdmin() {
		writeErr(w, http.StatusForbidden, errors.New("cluster-scoped TracingPolicies require the admin role"))
		return
	}
	if ns != "" && !id.CanEdit(ns) {
		writeErr(w, http.StatusForbidden, errors.New("not authorized to edit namespace "+ns))
		return
	}
	if s.guard.IsProtected(ns) {
		writeErr(w, http.StatusForbidden, errors.New("namespace "+ns+" is protected"))
		return
	}

	cur, err := s.policies.Get(req.Context(), kind, ns, body.Policy)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}

	var mutated json.RawMessage
	action := "ai.apply." + body.Kind
	switch body.Kind {
	case "enforce", "monitor":
		mode := k8s.ActionMonitor
		if body.Kind == "enforce" {
			mode = k8s.ActionEnforce
		}
		mutated, err = k8s.SetTracingAction(cur.Manifest, mode)
	case "exclude":
		if body.Exclusions.Empty() {
			writeErr(w, http.StatusBadRequest, errors.New("suggestion carried no exclusions"))
			return
		}
		mutated, _, err = k8s.ApplyExclusions(cur.Manifest, body.Exclusions)
	case "disable":
		if err := s.policies.Delete(req.Context(), kind, ns, body.Policy); err != nil {
			s.record(req, action, string(kind)+" "+ns+"/"+body.Policy, audit.OutcomeError, err.Error(), cur.Manifest, nil)
			writeErr(w, statusOf(err), err)
			return
		}
		s.record(req, action, string(kind)+" "+ns+"/"+body.Policy, audit.OutcomeSuccess, "approved AI suggestion", cur.Manifest, nil)
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
		return
	default:
		writeErr(w, http.StatusBadRequest, errors.New("kind must be enforce, monitor, exclude or disable"))
		return
	}
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	if err := s.guard.CheckManifest(mutated); err != nil {
		s.record(req, action, string(kind)+" "+ns+"/"+body.Policy, audit.OutcomeDenied, err.Error(), nil, nil)
		writeErr(w, http.StatusForbidden, err)
		return
	}
	if req.URL.Query().Get("dryRun") == "true" {
		writeJSON(w, http.StatusOK, map[string]any{
			"dryRun": true, "manifest": mutated,
			"diff": audit.DiffManifests(cur.Manifest, mutated),
		})
		return
	}
	applied, err := s.policies.Apply(req.Context(), kind, ns, body.Policy, mutated)
	if err != nil {
		s.record(req, action, string(kind)+" "+ns+"/"+body.Policy, audit.OutcomeError, err.Error(), cur.Manifest, mutated)
		writeErr(w, statusOf(err), err)
		return
	}
	s.record(req, action, string(kind)+" "+ns+"/"+body.Policy, audit.OutcomeSuccess, "approved AI suggestion", cur.Manifest, applied.Manifest)
	writeJSON(w, http.StatusOK, applied)
}
