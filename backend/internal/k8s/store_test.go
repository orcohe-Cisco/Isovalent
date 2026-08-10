package k8s

import (
	"context"
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

func TestMockStoreCRUD(t *testing.T) {
	ctx := context.Background()
	s := NewMockStore()

	cnps, err := s.List(ctx, KindCNP, "")
	if err != nil || len(cnps) == 0 {
		t.Fatalf("seeded CNPs expected, got %d err=%v", len(cnps), err)
	}
	shopOnly, _ := s.List(ctx, KindCNP, "shop")
	for _, p := range shopOnly {
		if p.Namespace != "shop" {
			t.Fatalf("namespace filter leaked %s/%s", p.Namespace, p.Name)
		}
	}

	manifest := json.RawMessage(`{"apiVersion":"cilium.io/v2","kind":"CiliumNetworkPolicy","metadata":{"name":"new-policy","namespace":"web"},"spec":{"endpointSelector":{}}}`)
	if _, err := s.Apply(ctx, KindCNP, "web", "new-policy", manifest); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, err := s.Get(ctx, KindCNP, "web", "new-policy")
	if err != nil || got.Name != "new-policy" {
		t.Fatalf("get after apply: %+v err=%v", got, err)
	}
	if err := s.Delete(ctx, KindCNP, "web", "new-policy"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(ctx, KindCNP, "web", "new-policy"); err == nil {
		t.Fatal("expected 404 after delete")
	}
	var apiErr *APIError
	if err := s.Delete(ctx, KindCNP, "web", "new-policy"); err == nil {
		t.Fatal("expected error")
	} else if !asAPIError(err, &apiErr) || apiErr.Status != 404 {
		t.Fatalf("expected 404 APIError, got %v", err)
	}
}

func asAPIError(err error, target **APIError) bool {
	e, ok := err.(*APIError)
	if ok {
		*target = e
	}
	return ok
}
