package server

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/isovalent-control/isovalent-control/backend/internal/hubble"
	"github.com/isovalent-control/isovalent-control/backend/internal/k8s"
	"github.com/isovalent-control/isovalent-control/backend/internal/tetragon"
	"github.com/isovalent-control/isovalent-control/backend/internal/version"
)

// ComponentVersion is one row in the version strip.
type ComponentVersion struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	// Source records how we learned the version, because "the agent told us"
	// and "we read the image tag off a DaemonSet" are not equally trustworthy.
	Source  string `json:"source"` // build | grpc | image | apiserver | demo | unavailable
	Detail  string `json:"detail,omitempty"`
	Healthy bool   `json:"healthy"`
}

// VersionInfo is the payload of GET /api/v1/versions.
type VersionInfo struct {
	Cluster    string             `json:"cluster"`
	Mode       string             `json:"mode"`
	CheckedAt  time.Time          `json:"checkedAt"`
	Components []ComponentVersion `json:"components"`
}

const versionCacheTTL = 60 * time.Second

type versionCache struct {
	mu   sync.Mutex
	info *VersionInfo
	at   time.Time
}

func (c *versionCache) get(fresh func() VersionInfo) VersionInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.info != nil && time.Since(c.at) < versionCacheTTL {
		return *c.info
	}
	info := fresh()
	c.info, c.at = &info, time.Now()
	return info
}

func (s *Server) getVersions(w http.ResponseWriter, req *http.Request) {
	// A forced refresh is what you want standing in front of a customer
	// after upgrading the agent mid-demo.
	if req.URL.Query().Get("refresh") == "true" {
		s.versions.mu.Lock()
		s.versions.info = nil
		s.versions.mu.Unlock()
	}
	writeJSON(w, http.StatusOK, s.versions.get(func() VersionInfo {
		return s.probeVersions(req.Context())
	}))
}

func (s *Server) probeVersions(ctx context.Context) VersionInfo {
	info := VersionInfo{
		Cluster:   s.cfg.ClusterName,
		Mode:      "live",
		CheckedAt: time.Now().UTC(),
	}

	self := ComponentVersion{
		ID:      "isovalent-control",
		Name:    "isovalent-control",
		Version: version.Version,
		Source:  "build",
		Detail:  version.String(),
		Healthy: true,
	}

	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		out = make([]ComponentVersion, 4)
	)
	set := func(i int, c ComponentVersion) {
		mu.Lock()
		out[i] = c
		mu.Unlock()
	}

	wg.Add(4)
	go func() { defer wg.Done(); set(0, s.probeKubernetes(ctx)) }()
	go func() { defer wg.Done(); set(1, s.probeCilium(ctx)) }()
	go func() { defer wg.Done(); set(2, s.probeHubble(ctx)) }()
	go func() { defer wg.Done(); set(3, s.probeTetragon(ctx)) }()
	wg.Wait()

	info.Components = append([]ComponentVersion{self}, out...)
	return info
}

func (s *Server) probeKubernetes(ctx context.Context) ComponentVersion {
	c := ComponentVersion{ID: "kubernetes", Name: "Kubernetes"}
	if s.k8s == nil {
		c.Source = "unavailable"
		c.Detail = "no Kubernetes client configured"
		return c
	}
	v, err := s.k8s.ServerVersion(ctx)
	if err != nil {
		c.Source = "unavailable"
		c.Detail = err.Error()
		return c
	}
	c.Version, c.Source, c.Healthy = v, "apiserver", true
	return c
}

// Cilium namespaces, in the order installs actually put them.
var ciliumNamespaces = []string{"kube-system", "cilium", "cilium-system"}
var tetragonNamespaces = []string{"kube-system", "tetragon", "kube-system-tetragon"}

func (s *Server) probeCilium(ctx context.Context) ComponentVersion {
	c := ComponentVersion{ID: "cilium", Name: "Cilium"}

	// The agent DaemonSet image is the most direct answer.
	if s.k8s != nil {
		if image, ns, err := s.k8s.WorkloadImage(ctx, "daemonsets", ciliumNamespaces, "cilium"); err == nil {
			if tag := k8s.ImageTag(image); tag != "" {
				c.Version, c.Source, c.Healthy = tag, "image", true
				c.Detail = ns + "/cilium · " + image
				return c
			}
		}
	}
	// Fall back to whatever Hubble Relay reports — it is compiled from the
	// same tree, so its version is Cilium's version.
	if st, err := hubble.ServerStatus(ctx, s.cfg.HubbleRelayAddr); err == nil && st.Version != "" {
		c.Version, c.Source, c.Healthy = shortVersion(st.Version), "grpc", true
		c.Detail = "reported by Hubble Relay"
		return c
	}
	c.Source = "unavailable"
	c.Detail = "no cilium DaemonSet readable and Hubble Relay unreachable"
	return c
}

func (s *Server) probeHubble(ctx context.Context) ComponentVersion {
	c := ComponentVersion{ID: "hubble", Name: "Hubble Relay"}
	st, err := hubble.ServerStatus(ctx, s.cfg.HubbleRelayAddr)
	if err != nil {
		c.Source = "unavailable"
		c.Detail = err.Error()
		return c
	}
	c.Version, c.Source, c.Healthy = shortVersion(st.Version), "grpc", true
	c.Detail = formatNodes(st)
	return c
}

func (s *Server) probeTetragon(ctx context.Context) ComponentVersion {
	c := ComponentVersion{ID: "tetragon", Name: "Tetragon"}
	if v, err := tetragon.Version(ctx, s.cfg.TetragonAddr); err == nil {
		c.Version, c.Source, c.Healthy = shortVersion(v), "grpc", true
		c.Detail = "reported by the agent at " + s.cfg.TetragonAddr
		return c
	} else {
		c.Detail = err.Error()
	}
	if s.k8s != nil {
		if image, ns, err := s.k8s.WorkloadImage(ctx, "daemonsets", tetragonNamespaces, "tetragon"); err == nil {
			if tag := k8s.ImageTag(image); tag != "" {
				c.Version, c.Source, c.Healthy = tag, "image", true
				c.Detail = ns + "/tetragon · " + image + " (gRPC unreachable)"
				return c
			}
		}
	}
	c.Source = "unavailable"
	return c
}

func formatNodes(st hubble.Status) string {
	b := &strings.Builder{}
	b.WriteString(itoa(int(st.ConnectedNodes)))
	b.WriteString(" node(s) connected")
	if st.UnavailableNodes > 0 {
		b.WriteString(", ")
		b.WriteString(itoa(int(st.UnavailableNodes)))
		b.WriteString(" unavailable")
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// shortVersion trims the build metadata agents append, e.g.
// "v1.16.5 compiled with go1.23.4 on linux/amd64" -> "v1.16.5".
func shortVersion(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.IndexAny(v, " \t"); i > 0 {
		return v[:i]
	}
	return v
}
