package k8s

import (
	"encoding/json"
	"strings"
	"testing"
)

// A manifest as returned by a GET from the API server: full of server-managed
// fields that make server-side apply fail with
// "metadata.managedFields must be nil".
const liveManifest = `{
  "apiVersion": "cilium.io/v1alpha1",
  "kind": "TracingPolicy",
  "metadata": {
    "name": "file-integrity",
    "uid": "3f0a-4c1b",
    "resourceVersion": "184922",
    "generation": 3,
    "creationTimestamp": "2026-08-09T10:00:00Z",
    "managedFields": [{"manager": "kubectl", "operation": "Apply"}],
    "labels": {"isovalent-control.io/category": "file"},
    "annotations": {"isovalent-control.io/action": "monitor"}
  },
  "spec": {
    "kprobes": [{
      "call": "security_file_permission",
      "selectors": [{
        "matchArgs": [{"index": 0, "operator": "Prefix", "values": ["/etc/shadow"]}],
        "matchActions": [{"action": "Post"}]
      }]
    }]
  },
  "status": {"conditions": []}
}`

func TestStripForApply(t *testing.T) {
	clean, err := StripForApply(json.RawMessage(liveManifest))
	if err != nil {
		t.Fatalf("strip: %v", err)
	}
	s := string(clean)
	for _, bad := range []string{"managedFields", "resourceVersion", `"uid"`, "creationTimestamp", "generation", `"status"`} {
		if strings.Contains(s, bad) {
			t.Fatalf("%s survived the strip: %s", bad, s)
		}
	}
	// The parts that matter must remain.
	for _, want := range []string{"file-integrity", "security_file_permission", "isovalent-control.io/category"} {
		if !strings.Contains(s, want) {
			t.Fatalf("strip removed %q", want)
		}
	}
}

func TestSetTracingActionRoundTrip(t *testing.T) {
	// monitor -> enforce
	enforced, err := SetTracingAction(json.RawMessage(liveManifest), ActionEnforce)
	if err != nil {
		t.Fatalf("to enforce: %v", err)
	}
	if !strings.Contains(string(enforced), `"Sigkill"`) {
		t.Fatalf("expected Sigkill: %s", enforced)
	}
	if got := DescribeTracingPolicy(KindTP, enforced); got.Action != ActionEnforce {
		t.Fatalf("describe says %s", got.Action)
	}

	// enforce -> monitor
	monitored, err := SetTracingAction(enforced, ActionMonitor)
	if err != nil {
		t.Fatalf("to monitor: %v", err)
	}
	if strings.Contains(string(monitored), `"Sigkill"`) {
		t.Fatalf("Sigkill survived downgrade: %s", monitored)
	}
	if got := DescribeTracingPolicy(KindTP, monitored); got.Action != ActionMonitor {
		t.Fatalf("describe says %s", got.Action)
	}
}

func TestDescribeTracingPolicyMetadata(t *testing.T) {
	info := DescribeTracingPolicy(KindTP, json.RawMessage(liveManifest))
	if info.Name != "file-integrity" || info.Category != "file" {
		t.Fatalf("bad metadata: %+v", info)
	}
	if len(info.Hooks) != 1 || info.Hooks[0] != "security_file_permission" {
		t.Fatalf("bad hooks: %+v", info.Hooks)
	}
}
