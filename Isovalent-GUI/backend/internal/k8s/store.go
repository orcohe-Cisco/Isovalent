package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// Kind enumerates the policy CRDs the platform manages.
type Kind string

const (
	KindCNP  Kind = "CiliumNetworkPolicy"
	KindCCNP Kind = "CiliumClusterwideNetworkPolicy"
	KindTP   Kind = "TracingPolicy"
	KindTPN  Kind = "TracingPolicyNamespaced"
	// KindNP is the native Kubernetes NetworkPolicy. It is included so the
	// console can load and edit the network rules a cluster already has,
	// rather than pretending the only policies that exist are the ones it
	// wrote itself.
	KindNP Kind = "NetworkPolicy"
)

// Namespaced reports whether the kind is namespace-scoped.
func (k Kind) Namespaced() bool { return k == KindCNP || k == KindTPN || k == KindNP }

type gvr struct{ group, version, resource string }

var kinds = map[Kind]gvr{
	KindCNP:  {"cilium.io", "v2", "ciliumnetworkpolicies"},
	KindCCNP: {"cilium.io", "v2", "ciliumclusterwidenetworkpolicies"},
	KindTP:   {"cilium.io", "v1alpha1", "tracingpolicies"},
	KindTPN:  {"cilium.io", "v1alpha1", "tracingpoliciesnamespaced"},
	KindNP:   {"networking.k8s.io", "v1", "networkpolicies"},
}

// APIVersion is the apiVersion string a manifest of this kind must carry.
func (k Kind) APIVersion() string {
	g := kinds[k]
	return g.group + "/" + g.version
}

// AllKinds is every kind the console manages, in menu order.
var AllKinds = []Kind{KindCNP, KindCCNP, KindNP, KindTP, KindTPN}

// ParseKind validates a kind string.
func ParseKind(s string) (Kind, error) {
	k := Kind(s)
	if _, ok := kinds[k]; !ok {
		return "", fmt.Errorf("unsupported policy kind %q", s)
	}
	return k, nil
}

// Policy is a stored policy with extracted metadata.
type Policy struct {
	Kind      Kind            `json:"kind"`
	Namespace string          `json:"namespace,omitempty"`
	Name      string          `json:"name"`
	Created   string          `json:"created,omitempty"`
	Manifest  json.RawMessage `json:"manifest"`
}

// PolicyStore abstracts policy CRUD.
type PolicyStore interface {
	List(ctx context.Context, kind Kind, namespace string) ([]Policy, error)
	Get(ctx context.Context, kind Kind, namespace, name string) (*Policy, error)
	// Apply performs a server-side apply (create-or-update) of manifest.
	Apply(ctx context.Context, kind Kind, namespace, name string, manifest json.RawMessage) (*Policy, error)
	Delete(ctx context.Context, kind Kind, namespace, name string) error
}

// LiveStore implements PolicyStore against a real API server.
type LiveStore struct {
	client *Client
}

// NewLiveStore wraps a Client.
func NewLiveStore(c *Client) *LiveStore { return &LiveStore{client: c} }

func path(kind Kind, namespace, name string) string {
	g := kinds[kind]
	p := fmt.Sprintf("/apis/%s/%s", g.group, g.version)
	if kind.Namespaced() && namespace != "" {
		p += "/namespaces/" + url.PathEscape(namespace)
	}
	p += "/" + g.resource
	if name != "" {
		p += "/" + url.PathEscape(name)
	}
	return p
}

func (s *LiveStore) List(ctx context.Context, kind Kind, namespace string) ([]Policy, error) {
	data, err := s.client.Do(ctx, "GET", path(kind, namespace, ""), "", nil)
	if err != nil {
		return nil, err
	}
	var list struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	out := make([]Policy, 0, len(list.Items))
	for _, item := range list.Items {
		out = append(out, toPolicy(kind, item))
	}
	return out, nil
}

func (s *LiveStore) Get(ctx context.Context, kind Kind, namespace, name string) (*Policy, error) {
	data, err := s.client.Do(ctx, "GET", path(kind, namespace, name), "", nil)
	if err != nil {
		return nil, err
	}
	p := toPolicy(kind, data)
	return &p, nil
}

func (s *LiveStore) Apply(ctx context.Context, kind Kind, namespace, name string, manifest json.RawMessage) (*Policy, error) {
	// Server-side apply: PATCH with apply-patch content type (accepts JSON,
	// since JSON is a YAML subset). force=true takes field ownership.
	p := path(kind, namespace, name) + "?fieldManager=isovalent-control&force=true"
	data, err := s.client.Do(ctx, "PATCH", p, "application/apply-patch+yaml", manifest)
	if err != nil {
		return nil, err
	}
	pol := toPolicy(kind, data)
	return &pol, nil
}

func (s *LiveStore) Delete(ctx context.Context, kind Kind, namespace, name string) error {
	_, err := s.client.Do(ctx, "DELETE", path(kind, namespace, name), "", nil)
	return err
}

func toPolicy(kind Kind, manifest json.RawMessage) Policy {
	var meta struct {
		Metadata struct {
			Name              string `json:"name"`
			Namespace         string `json:"namespace"`
			CreationTimestamp string `json:"creationTimestamp"`
		} `json:"metadata"`
	}
	_ = json.Unmarshal(manifest, &meta)
	return Policy{
		Kind:      kind,
		Namespace: meta.Metadata.Namespace,
		Name:      meta.Metadata.Name,
		Created:   meta.Metadata.CreationTimestamp,
		Manifest:  manifest,
	}
}

// ValidateManifest checks that a submitted manifest matches the target
// kind/namespace/name and carries a plausible apiVersion.
func ValidateManifest(kind Kind, namespace, name string, manifest json.RawMessage) error {
	var m struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(manifest, &m); err != nil {
		return fmt.Errorf("manifest is not valid JSON: %w", err)
	}
	g := kinds[kind]
	wantAPI := g.group + "/" + g.version
	if m.APIVersion != wantAPI {
		return fmt.Errorf("apiVersion must be %q (got %q)", wantAPI, m.APIVersion)
	}
	if m.Kind != string(kind) {
		return fmt.Errorf("kind must be %q (got %q)", kind, m.Kind)
	}
	if m.Metadata.Name != name {
		return fmt.Errorf("metadata.name %q does not match URL name %q", m.Metadata.Name, name)
	}
	if kind.Namespaced() && m.Metadata.Namespace != namespace {
		return fmt.Errorf("metadata.namespace %q does not match URL namespace %q", m.Metadata.Namespace, namespace)
	}
	return nil
}
