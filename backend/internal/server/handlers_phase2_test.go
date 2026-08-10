package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/isovalent-control/isovalent-control/backend/internal/alerts"
	"github.com/isovalent-control/isovalent-control/backend/internal/config"
	"github.com/isovalent-control/isovalent-control/backend/internal/hubble"
	"github.com/isovalent-control/isovalent-control/backend/internal/k8s"
	"github.com/isovalent-control/isovalent-control/backend/internal/store"
	"github.com/isovalent-control/isovalent-control/backend/internal/stream"
)

func newTestServer() (http.Handler, *Aggregator) {
	hub := stream.NewHub("*")
	agg := NewAggregator(hub)
	st := store.NewMemoryStore(1000)
	agg.SetStore(st)
	agg.SetRouter(alerts.NewRouter())
	s := New(config.Config{Mode: config.ModeMock, ClusterName: "test"}, hub, agg,
		k8s.NewMockStore(), nil, Deps{Router: alerts.NewRouter(), Store: st})
	return s.Router(), agg
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestMetricsEndpoint(t *testing.T) {
	h, agg := newTestServer()
	agg.ingestFlow(hubble.Flow{Time: time.Now(), Verdict: "DROPPED", DropReason: "POLICY_DENIED",
		Source: hubble.Endpoint{Namespace: "a", Workload: "x"}, Destination: hubble.Endpoint{Namespace: "b", Workload: "y"},
		L4: hubble.L4{Protocol: "TCP", DstPort: 80}})
	w := do(t, h, "GET", "/metrics", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "isovalent_control_flows_total") {
		t.Fatalf("metrics missing: %d %s", w.Code, w.Body.String())
	}
}

func TestTracingListAndToggle(t *testing.T) {
	h, _ := newTestServer()
	w := do(t, h, "GET", "/api/v1/tracingpolicies", "")
	if w.Code != 200 {
		t.Fatalf("list: %d", w.Code)
	}
	var infos []k8s.TracingPolicyInfo
	json.Unmarshal(w.Body.Bytes(), &infos)
	if len(infos) == 0 {
		t.Fatal("expected seeded tracing policies")
	}
	// block-metadata-service seeds as monitor; toggle to enforce.
	w = do(t, h, "POST", "/api/v1/tracingpolicies/-/block-metadata-service/action", `{"action":"enforce"}`)
	if w.Code != 200 {
		t.Fatalf("toggle: %d %s", w.Code, w.Body.String())
	}
	var info k8s.TracingPolicyInfo
	json.Unmarshal(w.Body.Bytes(), &info)
	if info.Action != k8s.ActionEnforce {
		t.Fatalf("expected enforce, got %s", info.Action)
	}
	// bad action rejected
	if w := do(t, h, "POST", "/api/v1/tracingpolicies/-/block-metadata-service/action", `{"action":"nope"}`); w.Code != 400 {
		t.Fatalf("expected 400 for bad action, got %d", w.Code)
	}
}

func TestDryRun(t *testing.T) {
	h, agg := newTestServer()
	// A forwarded flow checkout->payments:8443 and a stray cart->payments:8443.
	agg.ingestFlow(hubble.Flow{Time: time.Now(), Verdict: "FORWARDED",
		Source:      hubble.Endpoint{Namespace: "shop", Workload: "checkout", Labels: []string{"k8s:app=checkout"}},
		Destination: hubble.Endpoint{Namespace: "pay", Workload: "payments", Labels: []string{"k8s:app=payments"}},
		L4:          hubble.L4{Protocol: "TCP", DstPort: 8443}})
	agg.ingestFlow(hubble.Flow{Time: time.Now(), Verdict: "FORWARDED",
		Source:      hubble.Endpoint{Namespace: "shop", Workload: "cart", Labels: []string{"k8s:app=cart"}},
		Destination: hubble.Endpoint{Namespace: "pay", Workload: "payments", Labels: []string{"k8s:app=payments"}},
		L4:          hubble.L4{Protocol: "TCP", DstPort: 8443}})

	body := `{"apiVersion":"cilium.io/v2","kind":"CiliumNetworkPolicy","metadata":{"name":"lp","namespace":"pay"},
	  "spec":{"endpointSelector":{"matchLabels":{"app":"payments"}},
	  "ingress":[{"fromEndpoints":[{"matchLabels":{"app":"checkout"}}],"toPorts":[{"ports":[{"port":"8443","protocol":"TCP"}]}]}]}}`
	w := do(t, h, "POST", "/api/v1/policies/dryrun", body)
	if w.Code != 200 {
		t.Fatalf("dryrun: %d %s", w.Code, w.Body.String())
	}
	var res struct{ Applied, Allowed, Blocked int }
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Allowed < 1 || res.Blocked < 1 {
		t.Fatalf("expected checkout allowed + cart blocked, got %+v", res)
	}
}

func TestAlertRoutesAndHistory(t *testing.T) {
	h, agg := newTestServer()
	w := do(t, h, "PUT", "/api/v1/alerts/routes",
		`[{"id":"r1","name":"wh","type":"webhook","url":"http://127.0.0.1:1/x","minSeverity":"warning","enabled":true}]`)
	if w.Code != 200 {
		t.Fatalf("set routes: %d %s", w.Code, w.Body.String())
	}
	if w := do(t, h, "GET", "/api/v1/alerts/routes", ""); !strings.Contains(w.Body.String(), "\"wh\"") {
		t.Fatalf("routes not persisted: %s", w.Body.String())
	}

	agg.ingestFlow(hubble.Flow{Time: time.Now(), Verdict: "FORWARDED",
		Source: hubble.Endpoint{Namespace: "a", Workload: "x"}, Destination: hubble.Endpoint{Namespace: "b", Workload: "y"},
		L4: hubble.L4{Protocol: "TCP", DstPort: 80}})
	w = do(t, h, "GET", "/api/v1/history/flow?limit=10", "")
	if w.Code != 200 {
		t.Fatalf("history: %d", w.Code)
	}
	var recs []store.Record
	json.Unmarshal(w.Body.Bytes(), &recs)
	if len(recs) < 1 {
		t.Fatalf("expected stored flow, got %d", len(recs))
	}
}

func TestGitOpsStatusDisabled(t *testing.T) {
	h, _ := newTestServer()
	w := do(t, h, "GET", "/api/v1/gitops/status", "")
	if !strings.Contains(w.Body.String(), "\"enabled\":false") {
		t.Fatalf("expected gitops disabled, got %s", w.Body.String())
	}
}
