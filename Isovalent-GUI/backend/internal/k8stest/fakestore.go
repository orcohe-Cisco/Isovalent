// Package k8stest provides an in-memory PolicyStore for tests.
//
// This is a test double, not a runtime mode. The server has exactly one data
// path — a real cluster — and nothing in this package is reachable from the
// server binary: it is imported only from _test files, so the linker never
// pulls it in. That distinction matters. A console that can serve invented
// data is a console you cannot trust when it says a flow was dropped.
package k8stest

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"sync"

	"github.com/isovalent-control/isovalent-control/backend/internal/k8s"
)

// FakeStore is an in-memory k8s.PolicyStore.
type FakeStore struct {
	mu    sync.RWMutex
	items map[k8s.Kind]map[string]k8s.Policy // kind -> "ns/name" -> policy
}

func key(ns, name string) string {
	if ns == "" {
		return name
	}
	return ns + "/" + name
}

// NewFakeStore returns a store seeded with a couple of representative objects.
func NewFakeStore() *FakeStore {
	s := &FakeStore{items: map[k8s.Kind]map[string]k8s.Policy{}}
	seed := []struct {
		kind     k8s.Kind
		ns, name string
		manifest string
	}{
		{k8s.KindCNP, "shop", "allow-checkout", `{"apiVersion":"cilium.io/v2","kind":"CiliumNetworkPolicy","metadata":{"name":"allow-checkout","namespace":"shop"},"spec":{"endpointSelector":{"matchLabels":{"app":"checkout"}}}}`},
		{k8s.KindCNP, "pay", "payments-ingress", `{"apiVersion":"cilium.io/v2","kind":"CiliumNetworkPolicy","metadata":{"name":"payments-ingress","namespace":"pay"},"spec":{"endpointSelector":{"matchLabels":{"app":"payments"}},"ingress":[{"fromEndpoints":[{"matchLabels":{"app":"checkout"}}],"toPorts":[{"ports":[{"port":"8443","protocol":"TCP"}]}]}]}}`},
		{k8s.KindCCNP, "", "default-deny", `{"apiVersion":"cilium.io/v2","kind":"CiliumClusterwideNetworkPolicy","metadata":{"name":"default-deny"},"spec":{"endpointSelector":{}}}`},
		{k8s.KindTP, "", "block-metadata-service", `{"apiVersion":"cilium.io/v1alpha1","kind":"TracingPolicy","metadata":{"name":"block-metadata-service"},"spec":{"kprobes":[{"call":"tcp_connect","syscall":false,"args":[{"index":0,"type":"sock"}],"selectors":[{"matchArgs":[{"index":0,"operator":"DAddr","values":["169.254.169.254"]}],"matchActions":[{"action":"Post"}]}]}]}}`},
		{k8s.KindTP, "", "watch-sensitive-files", `{"apiVersion":"cilium.io/v1alpha1","kind":"TracingPolicy","metadata":{"name":"watch-sensitive-files"},"spec":{"kprobes":[{"call":"security_file_open","syscall":false,"args":[{"index":0,"type":"file"}],"selectors":[{"matchArgs":[{"index":0,"operator":"Prefix","values":["/etc/shadow"]}],"matchActions":[{"action":"Post"}]}]}]}}`},
	}
	for _, it := range seed {
		if s.items[it.kind] == nil {
			s.items[it.kind] = map[string]k8s.Policy{}
		}
		s.items[it.kind][key(it.ns, it.name)] = k8s.Policy{
			Kind: it.kind, Namespace: it.ns, Name: it.name, Manifest: json.RawMessage(it.manifest),
		}
	}
	return s
}

// List implements k8s.PolicyStore.
func (s *FakeStore) List(_ context.Context, kind k8s.Kind, namespace string) ([]k8s.Policy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []k8s.Policy
	for _, p := range s.items[kind] {
		if namespace != "" && p.Namespace != namespace {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Get implements k8s.PolicyStore.
func (s *FakeStore) Get(_ context.Context, kind k8s.Kind, namespace, name string) (*k8s.Policy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.items[kind][key(namespace, name)]
	if !ok {
		return nil, &k8s.APIError{Status: http.StatusNotFound, Message: "not found"}
	}
	return &p, nil
}

// Apply implements k8s.PolicyStore.
func (s *FakeStore) Apply(_ context.Context, kind k8s.Kind, namespace, name string, manifest json.RawMessage) (*k8s.Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items[kind] == nil {
		s.items[kind] = map[string]k8s.Policy{}
	}
	p := k8s.Policy{Kind: kind, Namespace: namespace, Name: name, Manifest: manifest}
	s.items[kind][key(namespace, name)] = p
	return &p, nil
}

// Delete implements k8s.PolicyStore.
func (s *FakeStore) Delete(_ context.Context, kind k8s.Kind, namespace, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(namespace, name)
	if _, ok := s.items[kind][k]; !ok {
		return &k8s.APIError{Status: http.StatusNotFound, Message: "not found"}
	}
	delete(s.items[kind], k)
	return nil
}
