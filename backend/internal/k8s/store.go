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
)

// Namespaced reports whether the kind is namespace-scoped.
func (k Kind) Namespaced() bool { return k == KindCNP || k == KindTPN }

type gvr struct{ group, version, resource string }

var kinds = map[Kind]gvr{
	KindCNP:  {"cilium.io", "v2", "ciliumnetworkpolicies"},
	KindCCNP: {"cilium.io", "v2", "ciliumclusterwidenetworkpolicies"},
	KindTP:   {"cilium.io", "v1alpha1", "tracingpolicies"},
	KindTPN:  {"cilium.io", "v1alpha1", "tracingpoliciesnamespaced"},
}

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

// PolicyStore abstracts policy CRUD so mock mode can run without a cluster.
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
	// Server-side apply rejects read-only/managed fields — a manifest fetched
	// via GET carries metadata.managedFields, resourceVersion, uid, etc., which
	// trigger "metadata.managedFields must be nil". Strip them first.
	clean, err := StripForApply(manifest)
	if err != nil {
		return nil, err
	}
	// Server-side apply: PATCH with apply-patch content type (accepts JSON,
	// since JSON is a YAML subset). force=true takes field ownership.
	p := path(kind, namespace, name) + "?fieldManager=isovalent-control&force=true"
	data, err := s.client.Do(ctx, "PATCH", p, "application/apply-patch+yaml", clean)
	if err != nil {
		return nil, err
	}
	pol := toPolicy(kind, data)
	return &pol, nil
}

// StripForApply removes server-managed / read-only fields that make a
// server-side apply fail (managedFields, resourceVersion, uid,
// creationTimestamp, generation, selfLink) and drops any status subresource.
func StripForApply(manifest json.RawMessage) (json.RawMessage, error) {
	var doc map[string]any
	if err := json.Unmarshal(manifest, &doc); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	delete(doc, "status")
	if meta, ok := doc["metadata"].(map[string]any); ok {
		for _, f := range []string{"managedFields", "resourceVersion", "uid", "creationTimestamp", "generation", "selfLink", "ownerReferences"} {
			delete(meta, f)
		}
	}
	return json.Marshal(doc)
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
	// Hand the UI a clean, re-appliable manifest (no managedFields/status noise).
	if clean, err := StripForApply(manifest); err == nil {
		manifest = clean
	}
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
