package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

// MockStore is an in-memory PolicyStore seeded with sample policies so demo
// mode has realistic content in the policy screens.
type MockStore struct {
	mu       sync.RWMutex
	policies map[string]Policy // key: kind/ns/name
}

func key(kind Kind, ns, name string) string { return fmt.Sprintf("%s/%s/%s", kind, ns, name) }

// NewMockStore returns a seeded in-memory store.
func NewMockStore() *MockStore {
	s := &MockStore{policies: map[string]Policy{}}
	for _, seed := range seedPolicies {
		p := toPolicy(seed.kind, json.RawMessage(seed.manifest))
		p.Created = time.Now().Add(-seed.age).UTC().Format(time.RFC3339)
		s.policies[key(p.Kind, p.Namespace, p.Name)] = p
	}
	return s
}

func (s *MockStore) List(_ context.Context, kind Kind, namespace string) ([]Policy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Policy
	for _, p := range s.policies {
		if p.Kind != kind {
			continue
		}
		if namespace != "" && kind.Namespaced() && p.Namespace != namespace {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *MockStore) Get(_ context.Context, kind Kind, namespace, name string) (*Policy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.policies[key(kind, namespace, name)]
	if !ok {
		return nil, &APIError{Status: 404, Message: fmt.Sprintf("%s %q not found", kind, name)}
	}
	return &p, nil
}

func (s *MockStore) Apply(_ context.Context, kind Kind, namespace, name string, manifest json.RawMessage) (*Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := toPolicy(kind, manifest)
	k := key(kind, namespace, name)
	if prev, ok := s.policies[k]; ok {
		p.Created = prev.Created
	} else {
		p.Created = time.Now().UTC().Format(time.RFC3339)
	}
	s.policies[k] = p
	return &p, nil
}

func (s *MockStore) Delete(_ context.Context, kind Kind, namespace, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(kind, namespace, name)
	if _, ok := s.policies[k]; !ok {
		return &APIError{Status: 404, Message: fmt.Sprintf("%s %q not found", kind, name)}
	}
	delete(s.policies, k)
	return nil
}

var seedPolicies = []struct {
	kind     Kind
	age      time.Duration
	manifest string
}{
	{KindCNP, 40 * 24 * time.Hour, `{
	  "apiVersion": "cilium.io/v2",
	  "kind": "CiliumNetworkPolicy",
	  "metadata": {"name": "allow-gateway-to-catalog", "namespace": "shop"},
	  "spec": {
	    "endpointSelector": {"matchLabels": {"app": "productcatalog"}},
	    "ingress": [{
	      "fromEndpoints": [{"matchLabels": {"k8s:io.kubernetes.pod.namespace": "web", "app": "gateway"}}],
	      "toPorts": [{"ports": [{"port": "3550", "protocol": "TCP"}],
	        "rules": {"http": [{"method": "GET", "path": "/api/.*"}]}}]
	    }]
	  }
	}`},
	{KindCNP, 33 * 24 * time.Hour, `{
	  "apiVersion": "cilium.io/v2",
	  "kind": "CiliumNetworkPolicy",
	  "metadata": {"name": "cart-redis-only", "namespace": "shop"},
	  "spec": {
	    "endpointSelector": {"matchLabels": {"app": "cart"}},
	    "egress": [
	      {"toEndpoints": [{"matchLabels": {"app": "redis-cart"}}],
	       "toPorts": [{"ports": [{"port": "6379", "protocol": "TCP"}]}]},
	      {"toEndpoints": [{"matchLabels": {"k8s:io.kubernetes.pod.namespace": "kube-system", "k8s-app": "kube-dns"}}],
	       "toPorts": [{"ports": [{"port": "53", "protocol": "UDP"}], "rules": {"dns": [{"matchPattern": "*"}]}}]}
	    ]
	  }
	}`},
	{KindCNP, 12 * 24 * time.Hour, `{
	  "apiVersion": "cilium.io/v2",
	  "kind": "CiliumNetworkPolicy",
	  "metadata": {"name": "payments-ingress-checkout", "namespace": "pay"},
	  "spec": {
	    "endpointSelector": {"matchLabels": {"app": "payments"}},
	    "ingress": [{
	      "fromEndpoints": [{"matchLabels": {"k8s:io.kubernetes.pod.namespace": "shop", "app": "checkout"}}],
	      "toPorts": [{"ports": [{"port": "8443", "protocol": "TCP"}]}]
	    }]
	  }
	}`},
	{KindCCNP, 60 * 24 * time.Hour, `{
	  "apiVersion": "cilium.io/v2",
	  "kind": "CiliumClusterwideNetworkPolicy",
	  "metadata": {"name": "lockdown-external-egress"},
	  "spec": {
	    "endpointSelector": {"matchLabels": {"k8s:io.kubernetes.pod.namespace": "default"}},
	    "egressDeny": [{"toEntities": ["world"]}]
	  }
	}`},
	{KindTP, 25 * 24 * time.Hour, `{
	  "apiVersion": "cilium.io/v1alpha1",
	  "kind": "TracingPolicy",
	  "metadata": {
	    "name": "file-integrity-monitoring",
	    "labels": {"isovalent-control.io/category": "file"},
	    "annotations": {"isovalent-control.io/action": "enforce", "isovalent-control.io/description": "Kill processes reading /etc/shadow or /etc/sudoers."}
	  },
	  "spec": {
	    "kprobes": [{
	      "call": "security_file_permission",
	      "syscall": false,
	      "args": [{"index": 0, "type": "file"}, {"index": 1, "type": "int"}],
	      "selectors": [{
	        "matchArgs": [{"index": 0, "operator": "Prefix", "values": ["/etc/shadow", "/etc/sudoers"]}],
	        "matchActions": [{"action": "Sigkill"}]
	      }]
	    }]
	  }
	}`},
	{KindTP, 9 * 24 * time.Hour, `{
	  "apiVersion": "cilium.io/v1alpha1",
	  "kind": "TracingPolicy",
	  "metadata": {
	    "name": "block-metadata-service",
	    "labels": {"isovalent-control.io/category": "egress"},
	    "annotations": {"isovalent-control.io/action": "monitor", "isovalent-control.io/description": "Detect connections to the cloud metadata IP (SSRF)."}
	  },
	  "spec": {
	    "kprobes": [{
	      "call": "tcp_connect",
	      "syscall": false,
	      "args": [{"index": 0, "type": "sock"}],
	      "selectors": [{
	        "matchArgs": [{"index": 0, "operator": "DAddr", "values": ["169.254.169.254"]}],
	        "matchActions": [{"action": "Post"}]
	      }]
	    }]
	  }
	}`},
}
