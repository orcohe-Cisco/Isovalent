package k8s

import (
	"encoding/json"
	"testing"

	"github.com/isovalent-control/isovalent-control/backend/internal/tetragon"
)

const execPolicy = `{
 "apiVersion":"cilium.io/v1alpha1","kind":"TracingPolicy","metadata":{"name":"nc"},
 "spec":{"kprobes":[{"call":"security_bprm_check","args":[{"index":0,"type":"linux_binprm"}],
   "selectors":[{"matchBinaries":[{"operator":"In","values":["/usr/bin/nc"]}],
                 "matchActions":[{"action":"Sigkill"}]}]}]}}`

func TestExplainIdentifiesTheMatchingClause(t *testing.T) {
	ev := tetragon.Event{Function: "security_bprm_check", Binary: "/usr/bin/nc", Action: "SIGKILL"}
	ex := ExplainTracingMatch(KindTP, json.RawMessage(execPolicy), ev)
	if !ex.Confident {
		t.Fatal("hook should have been identified")
	}
	if len(ex.Clauses) == 0 {
		t.Fatal("no clause reported")
	}
	c := ex.Clauses[0]
	if c.Kind != "matchBinaries" || c.Matched != "/usr/bin/nc" {
		t.Fatalf("wrong clause: %+v", c)
	}
	if c.Excludable != "binaries" {
		t.Fatalf("a positive matchBinaries should be offered as an exclusion, got %q", c.Excludable)
	}
	if ex.Summary == "" {
		t.Fatal("summary should render something readable")
	}
}

func TestExplainDoesNotClaimAMatchForAnotherBinary(t *testing.T) {
	ev := tetragon.Event{Function: "security_bprm_check", Binary: "/bin/ls"}
	ex := ExplainTracingMatch(KindTP, json.RawMessage(execPolicy), ev)
	for _, c := range ex.Clauses {
		if c.Kind == "matchBinaries" {
			t.Fatalf("reported a matchBinaries clause for a non-matching binary: %+v", c)
		}
	}
}

func TestExplainSkipsUnrelatedHooks(t *testing.T) {
	ev := tetragon.Event{Function: "tcp_connect", Binary: "/usr/bin/nc"}
	ex := ExplainTracingMatch(KindTP, json.RawMessage(execPolicy), ev)
	if ex.Confident {
		t.Fatal("a policy hooking a different function must not be reported as the cause")
	}
}
