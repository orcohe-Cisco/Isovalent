package investigate

import (
	"context"
	"testing"
	"time"

	"github.com/isovalent-control/isovalent-control/backend/internal/hubble"
	"github.com/isovalent-control/isovalent-control/backend/internal/store"
	"github.com/isovalent-control/isovalent-control/backend/internal/tetragon"
)

func seed(t *testing.T) store.Store {
	t.Helper()
	s := store.NewMemoryStore(1000)
	now := time.Now().UTC()
	flows := []hubble.Flow{
		{Time: now, Verdict: "DROPPED", DropReason: "POLICY_DENIED",
			Source:      hubble.Endpoint{Namespace: "shop", Workload: "cart"},
			Destination: hubble.Endpoint{Namespace: "pay", Workload: "payments"},
			L4:          hubble.L4{Protocol: "TCP", DstPort: 8443},
			Policies:    []hubble.PolicyRef{{Name: "payments-ingress", Namespace: "pay", Direction: "ingress", Effect: "denied"}}},
		{Time: now, Verdict: "FORWARDED",
			Source:      hubble.Endpoint{Namespace: "shop", Workload: "checkout"},
			Destination: hubble.Endpoint{Namespace: "pay", Workload: "payments"},
			L4:          hubble.L4{Protocol: "TCP", DstPort: 8443},
			L7:          &hubble.L7{Type: "http", Method: "POST", URL: "/v1/charge", Status: 200}},
		{Time: now, Verdict: "FORWARDED", IsReply: true,
			Source:      hubble.Endpoint{Namespace: "pay", Workload: "payments"},
			Destination: hubble.Endpoint{Namespace: "shop", Workload: "checkout"}},
	}
	for _, f := range flows {
		if err := s.Save(context.Background(), store.KindFlow, f.Time, f); err != nil {
			t.Fatal(err)
		}
	}
	ev := tetragon.Event{Time: now, Namespace: "shop", Workload: "cart", Binary: "/usr/bin/curl",
		Function: "tcp_connect", Action: "SIGKILL", Policy: "block-metadata"}
	if err := s.Save(context.Background(), store.KindEvent, ev.Time, ev); err != nil {
		t.Fatal(err)
	}
	return s
}

func search(t *testing.T, s store.Store, f Filter) Result {
	t.Helper()
	f.Since = time.Now().Add(-time.Hour)
	f.Until = time.Now().Add(time.Minute)
	res, err := Search(context.Background(), s, f)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestSearchSpansBothSources(t *testing.T) {
	res := search(t, seed(t), Filter{HideReplies: true})
	var cilium, tetra int
	for _, r := range res.Rows {
		switch r.Source {
		case SourceCilium:
			cilium++
		case SourceTetragon:
			tetra++
		}
	}
	if cilium == 0 || tetra == 0 {
		t.Fatalf("expected both sources, got cilium=%d tetragon=%d", cilium, tetra)
	}
}

func TestHideRepliesIsTheDefaultBehaviour(t *testing.T) {
	with := search(t, seed(t), Filter{})
	without := search(t, seed(t), Filter{HideReplies: true})
	if with.Total <= without.Total {
		t.Fatalf("reply filtering had no effect: %d vs %d", with.Total, without.Total)
	}
}

func TestBlockedOnlyCoversBothSources(t *testing.T) {
	res := search(t, seed(t), Filter{BlockedOnly: true, HideReplies: true})
	if res.Total != 2 {
		t.Fatalf("expected the dropped flow and the SIGKILL, got %d", res.Total)
	}
	for _, r := range res.Rows {
		if !r.Blocked {
			t.Fatalf("unblocked row leaked through: %+v", r)
		}
	}
}

func TestL7MethodFilter(t *testing.T) {
	res := search(t, seed(t), Filter{L7Methods: []string{"post"}, HideReplies: true})
	if res.Total != 1 || res.Rows[0].L7Path != "/v1/charge" {
		t.Fatalf("method filter should be case-insensitive and exact: %+v", res.Rows)
	}
}

func TestPolicyAttributionSurvivesNormalisation(t *testing.T) {
	res := search(t, seed(t), Filter{Verdicts: []string{VerdictDropped}})
	if res.Total != 1 {
		t.Fatalf("expected one dropped row, got %d", res.Total)
	}
	if res.Rows[0].Policy != "payments-ingress" {
		t.Fatalf("the denying policy must be carried through: %+v", res.Rows[0])
	}
}

func TestFacetsAreCounted(t *testing.T) {
	res := search(t, seed(t), Filter{HideReplies: true})
	ns := res.Facets["namespaces"]
	if len(ns) == 0 {
		t.Fatal("namespace facet empty")
	}
	total := 0
	for _, f := range ns {
		total += f.Count
	}
	if total == 0 {
		t.Fatal("facet counts are all zero")
	}
}

func TestWindowParsing(t *testing.T) {
	w := ParseWindow("5m", time.Time{}, time.Time{})
	if d := w.Until.Sub(w.Since); d < 4*time.Minute || d > 6*time.Minute {
		t.Fatalf("5m window resolved to %v", d)
	}
	if ParseWindow("nonsense", time.Time{}, time.Time{}).Label != "nonsense" {
		t.Fatal("label should be echoed back even when it falls back to the default duration")
	}
}
