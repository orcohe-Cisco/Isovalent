package k8s

import (
	"encoding/json"
	"testing"
)

func TestValidateManifest(t *testing.T) {
	good := json.RawMessage(`{"apiVersion":"cilium.io/v2","kind":"CiliumNetworkPolicy","metadata":{"name":"p1","namespace":"shop"},"spec":{}}`)
	if err := ValidateManifest(KindCNP, "shop", "p1", good); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	cases := []struct {
		kind     Kind
		ns, name string
		manifest string
	}{
		{KindCNP, "shop", "p1", `{"apiVersion":"cilium.io/v2","kind":"CiliumNetworkPolicy","metadata":{"name":"other","namespace":"shop"}}`},
		{KindCNP, "shop", "p1", `{"apiVersion":"cilium.io/v2","kind":"CiliumNetworkPolicy","metadata":{"name":"p1","namespace":"pay"}}`},
		{KindCNP, "shop", "p1", `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"p1","namespace":"shop"}}`},
		{KindTP, "", "t1", `{"apiVersion":"cilium.io/v2","kind":"TracingPolicy","metadata":{"name":"t1"}}`},
		{KindCNP, "shop", "p1", `not json`},
	}
	for i, c := range cases {
		if err := ValidateManifest(c.kind, c.ns, c.name, json.RawMessage(c.manifest)); err == nil {
			t.Fatalf("case %d: expected rejection", i)
		}
	}
}
