package guard

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRejectsProtectedNamespace(t *testing.T) {
	g := New([]string{"isovalent-control", "kube-system"})

	cases := []string{
		// metadata.namespace
		`{"kind":"TracingPolicyNamespaced","metadata":{"name":"p","namespace":"isovalent-control"},"spec":{}}`,
		// Tetragon podSelector naming the namespace
		`{"kind":"TracingPolicy","metadata":{"name":"p"},"spec":{"podSelector":{"matchExpressions":[{"key":"namespace","operator":"In","values":["kube-system"]}]}}}`,
		// Cilium endpointSelector via the namespace label
		`{"kind":"CiliumClusterwideNetworkPolicy","metadata":{"name":"p"},"spec":{"endpointSelector":{"matchLabels":{"k8s:io.kubernetes.pod.namespace":"isovalent-control"}}}}`,
	}
	for i, c := range cases {
		if err := g.CheckManifest(json.RawMessage(c)); err == nil {
			t.Fatalf("case %d: protected namespace was not rejected", i)
		}
	}
}

func TestAllowsExclusionOfProtectedNamespace(t *testing.T) {
	g := New([]string{"isovalent-control"})
	// NotIn is an exclusion, not a selection: it must not trip the guard,
	// otherwise the safest possible policy is the one we reject.
	ok := `{"kind":"TracingPolicy","metadata":{"name":"p"},"spec":{"podSelector":{"matchExpressions":[{"key":"namespace","operator":"NotIn","values":["isovalent-control"]}]}}}`
	if err := g.CheckManifest(json.RawMessage(ok)); err != nil {
		t.Fatalf("exclusion of a protected namespace was rejected: %v", err)
	}
}

func TestAllowsOrdinaryNamespace(t *testing.T) {
	g := New([]string{"isovalent-control"})
	ok := `{"kind":"CiliumNetworkPolicy","metadata":{"name":"p","namespace":"shop"},"spec":{"endpointSelector":{"matchLabels":{"app":"checkout"}}}}`
	if err := g.CheckManifest(json.RawMessage(ok)); err != nil {
		t.Fatalf("ordinary namespace rejected: %v", err)
	}
}

func TestExemptInjectsSelfExclusion(t *testing.T) {
	in := json.RawMessage(`{"kind":"TracingPolicy","metadata":{"name":"p"},"spec":{"kprobes":[]}}`)
	out, changed, err := Exempt(in)
	if err != nil || !changed {
		t.Fatalf("expected an exemption to be injected: changed=%v err=%v", changed, err)
	}
	if !strings.Contains(string(out), SelfLabelValue) {
		t.Fatalf("exemption missing from %s", out)
	}
	// Idempotent: applying it twice must not stack duplicate expressions.
	again, changed2, err := Exempt(out)
	if err != nil {
		t.Fatal(err)
	}
	if changed2 {
		t.Fatalf("second Exempt should be a no-op, got %s", again)
	}
}

func TestExemptIgnoresNetworkPolicies(t *testing.T) {
	// Cilium policies are selected by endpointSelector, not podSelector; a
	// podSelector injected there would be silently meaningless.
	in := json.RawMessage(`{"kind":"CiliumNetworkPolicy","metadata":{"name":"p"},"spec":{}}`)
	if _, changed, _ := Exempt(in); changed {
		t.Fatal("Exempt must not touch Cilium policies")
	}
}
