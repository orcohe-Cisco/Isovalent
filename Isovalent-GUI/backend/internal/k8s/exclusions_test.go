package k8s

import (
	"encoding/json"
	"strings"
	"testing"
)

const filePolicy = `{
 "apiVersion":"cilium.io/v1alpha1","kind":"TracingPolicy","metadata":{"name":"files"},
 "spec":{"kprobes":[{"call":"security_file_open","args":[{"index":0,"type":"file"}],
   "selectors":[{"matchArgs":[{"index":0,"operator":"Prefix","values":["/etc/"]}],
                 "matchActions":[{"action":"Post"}]}]}]}}`

func TestApplyExclusionsBinaries(t *testing.T) {
	out, notes, err := ApplyExclusions(json.RawMessage(filePolicy), Exclusions{
		Binaries: []string{"/usr/bin/fluent-bit"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "matchBinaries") || !strings.Contains(string(out), "fluent-bit") {
		t.Fatalf("matchBinaries not injected: %s", out)
	}
	if len(notes) != 1 || !notes[0].Applied {
		t.Fatalf("expected one applied note, got %+v", notes)
	}
}

func TestApplyExclusionsMergesRatherThanDuplicates(t *testing.T) {
	once, _, _ := ApplyExclusions(json.RawMessage(filePolicy), Exclusions{Binaries: []string{"/a"}})
	twice, _, _ := ApplyExclusions(once, Exclusions{Binaries: []string{"/b"}})
	if strings.Count(string(twice), `"matchBinaries"`) != 1 {
		t.Fatalf("expected one merged matchBinaries clause, got %s", twice)
	}
	if !strings.Contains(string(twice), `"/a"`) || !strings.Contains(string(twice), `"/b"`) {
		t.Fatalf("merge lost a value: %s", twice)
	}
}

func TestApplyExclusionsRefusesContradictoryPathIndex(t *testing.T) {
	// The selector already constrains index 0 with Prefix. Adding a NotPrefix
	// on the same index contradicts it, so it must be skipped and reported.
	_, notes, err := ApplyExclusions(json.RawMessage(filePolicy), Exclusions{
		Paths: []string{"/etc/ssl/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range notes {
		if n.Dimension == "paths" {
			found = true
			if n.Applied {
				t.Fatalf("path exclusion should not have applied over an existing matchArgs: %+v", n)
			}
			if !strings.Contains(n.Detail, "already constrained") {
				t.Fatalf("note should explain why: %q", n.Detail)
			}
		}
	}
	if !found {
		t.Fatal("no note for the paths dimension")
	}
}

func TestApplyExclusionsPathOnFreeIndex(t *testing.T) {
	policy := `{"apiVersion":"cilium.io/v1alpha1","kind":"TracingPolicy","metadata":{"name":"f"},
	  "spec":{"kprobes":[{"call":"security_file_open","args":[{"index":0,"type":"file"}],
	  "selectors":[{"matchActions":[{"action":"Post"}]}]}]}}`
	out, notes, err := ApplyExclusions(json.RawMessage(policy), Exclusions{Paths: []string{"/var/log/"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "NotPrefix") {
		t.Fatalf("NotPrefix not added: %s", out)
	}
	for _, n := range notes {
		if n.Dimension == "paths" && !n.Applied {
			t.Fatalf("expected the path exclusion to apply: %+v", n)
		}
	}
}

func TestApplyExclusionsContainerAndPodLabel(t *testing.T) {
	out, _, err := ApplyExclusions(json.RawMessage(filePolicy), Exclusions{
		Containers: []string{"istio-proxy"},
		PodLabels:  []string{"app=fluentd"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "containerSelector") || !strings.Contains(s, "istio-proxy") {
		t.Fatalf("containerSelector missing: %s", s)
	}
	if !strings.Contains(s, "podSelector") || !strings.Contains(s, "fluentd") {
		t.Fatalf("podSelector missing: %s", s)
	}
}
