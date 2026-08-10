package server

import (
	"testing"
	"time"

	"github.com/isovalent-control/isovalent-control/backend/internal/hubble"
	"github.com/isovalent-control/isovalent-control/backend/internal/stream"
	"github.com/isovalent-control/isovalent-control/backend/internal/tetragon"
)

func testFlow(verdict string) hubble.Flow {
	return hubble.Flow{
		Time:        time.Now(),
		Verdict:     verdict,
		DropReason:  map[bool]string{true: "POLICY_DENIED"}[verdict == "DROPPED"],
		Source:      hubble.Endpoint{Namespace: "web", Workload: "frontend"},
		Destination: hubble.Endpoint{Namespace: "pay", Workload: "payments"},
		L4:          hubble.L4{Protocol: "TCP", DstPort: 8443},
		L7:          &hubble.L7{Type: "http", Status: 200},
	}
}

func TestAggregator(t *testing.T) {
	a := NewAggregator(stream.NewHub("*"))

	for i := 0; i < 10; i++ {
		a.ingestFlow(testFlow("FORWARDED"))
	}
	a.ingestFlow(testFlow("DROPPED"))
	a.ingestEvent(tetragon.Event{
		Time: time.Now(), Type: "process_kprobe", Namespace: "web", Workload: "frontend",
		Binary: "/bin/cat", Action: "SIGKILL", Policy: "file-integrity-monitoring",
	})

	o := a.Overview()
	if o.TotalFlows != 11 || o.TotalDrops != 1 || o.TotalKills != 1 {
		t.Fatalf("overview totals wrong: %+v", o)
	}
	if len(o.Series) == 0 {
		t.Fatal("expected time series buckets")
	}

	nodes, edges := a.ServiceMap()
	if len(nodes) != 2 || len(edges) != 1 {
		t.Fatalf("servicemap: %d nodes %d edges", len(nodes), len(edges))
	}
	if edges[0].Forwarded != 10 || edges[0].Dropped != 1 {
		t.Fatalf("edge counters: %+v", edges[0])
	}

	alerts := a.RecentAlerts(10)
	if len(alerts) != 2 { // one drop + one enforcement
		t.Fatalf("expected 2 alerts, got %d", len(alerts))
	}

	// duplicate drop within 5s is suppressed
	a.ingestFlow(testFlow("DROPPED"))
	if got := len(a.RecentAlerts(10)); got != 2 {
		t.Fatalf("suppression failed: %d alerts", got)
	}

	if got := len(a.RecentFlows(5)); got != 5 {
		t.Fatalf("RecentFlows limit: %d", got)
	}
}
