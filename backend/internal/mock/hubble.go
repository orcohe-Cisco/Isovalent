// Package mock generates a realistic demo topology so every screen of the UI
// works without a cluster. The generated data intentionally includes policy
// drops, HTTP 5xx, DNS failures and Tetragon enforcement (SIGKILL) events.
package mock

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/isovalent-control/isovalent-control/backend/internal/hubble"
)

type service struct {
	ns, workload string
}

type edge struct {
	src, dst service
	port     uint32
	proto    string  // TCP | UDP
	l7       string  // "", "http", "dns"
	dropProb float64 // probability a given flow is policy-dropped
	errProb  float64 // probability of HTTP 5xx / DNS NXDOMAIN
	weight   int     // relative traffic volume
}

var (
	frontend  = service{"web", "frontend"}
	gateway   = service{"web", "gateway"}
	catalog   = service{"shop", "productcatalog"}
	cart      = service{"shop", "cart"}
	checkout  = service{"shop", "checkout"}
	redisCart = service{"shop", "redis-cart"}
	payments  = service{"pay", "payments"}
	postgres  = service{"pay", "postgres"}
	coredns   = service{"kube-system", "coredns"}
	world     = service{"", "world"} // reserved:world
	miner     = service{"default", "crypto-miner"}
)

var edges = []edge{
	{world, frontend, 443, "TCP", "http", 0, 0.02, 8},
	{frontend, gateway, 8080, "TCP", "http", 0, 0.01, 8},
	{gateway, catalog, 3550, "TCP", "http", 0, 0.005, 6},
	{gateway, cart, 7070, "TCP", "http", 0, 0.01, 5},
	{gateway, checkout, 5050, "TCP", "http", 0, 0.03, 4},
	{cart, redisCart, 6379, "TCP", "", 0, 0, 5},
	{checkout, payments, 8443, "TCP", "http", 0, 0.05, 3},
	{payments, postgres, 5432, "TCP", "", 0, 0, 3},
	{checkout, catalog, 3550, "TCP", "http", 0, 0.005, 2},
	// DNS lookups
	{frontend, coredns, 53, "UDP", "dns", 0, 0.04, 4},
	{checkout, coredns, 53, "UDP", "dns", 0, 0.08, 2},
	// Policy violations (dropped by CiliumNetworkPolicy)
	{cart, payments, 8443, "TCP", "", 1.0, 0, 1},     // cart may not talk to payments
	{miner, world, 4444, "TCP", "", 1.0, 0, 2},       // egress lockdown
	{world, redisCart, 6379, "TCP", "", 1.0, 0, 1},   // no external ingress to redis
	{frontend, postgres, 5432, "TCP", "", 0.9, 0, 1}, // frontend must not reach DB
}

var httpPaths = []string{"/api/products", "/api/cart", "/api/checkout", "/api/user", "/healthz", "/api/orders", "/api/recommendations"}
var dnsNames = []string{"payments.pay.svc.cluster.local", "api.stripe.com", "productcatalog.shop.svc.cluster.local", "telemetry.example.com", "cdn.shop-assets.io"}

// HubbleSource generates demo flows.
type HubbleSource struct {
	rng *rand.Rand
}

// NewHubbleSource returns a demo flow generator.
func NewHubbleSource() *HubbleSource {
	return &HubbleSource{rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
}

// Flows implements hubble.Source.
func (s *HubbleSource) Flows(ctx context.Context) (<-chan hubble.Flow, error) {
	out := make(chan hubble.Flow, 256)
	// weighted edge picker
	var pool []int
	for i, e := range edges {
		for j := 0; j < e.weight; j++ {
			_ = j
			pool = append(pool, i)
		}
	}
	go func() {
		defer close(out)
		ticker := time.NewTicker(120 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				n := 1 + s.rng.Intn(3)
				for i := 0; i < n; i++ {
					select {
					case out <- s.flow(edges[pool[s.rng.Intn(len(pool))]]):
					default:
					}
				}
			}
		}
	}()
	return out, nil
}

func (s *HubbleSource) flow(e edge) hubble.Flow {
	f := hubble.Flow{
		Time:        time.Now(),
		Verdict:     "FORWARDED",
		Direction:   "EGRESS",
		Source:      s.endpoint(e.src),
		Destination: s.endpoint(e.dst),
		L4:          hubble.L4{Protocol: e.proto, SrcPort: 32768 + uint32(s.rng.Intn(28000)), DstPort: e.port},
		Node:        fmt.Sprintf("node-%d", 1+s.rng.Intn(3)),
	}
	if s.rng.Float64() < e.dropProb {
		f.Verdict = "DROPPED"
		f.DropReason = "POLICY_DENIED"
		f.Summary = fmt.Sprintf("Policy denied %s/%s -> %s/%s:%d", ns(e.src), e.src.workload, ns(e.dst), e.dst.workload, e.port)
		return f
	}
	switch e.l7 {
	case "http":
		l7 := &hubble.L7{
			Type:      "http",
			Method:    pick(s.rng, "GET", "GET", "GET", "POST", "PUT"),
			URL:       httpPaths[s.rng.Intn(len(httpPaths))],
			Protocol:  "HTTP/1.1",
			Status:    200,
			LatencyMs: 2 + s.rng.Float64()*90,
			Headers: []hubble.Header{
				{Key: "Host", Value: e.dst.workload + "." + e.dst.ns + ".svc"},
				{Key: "User-Agent", Value: pick(s.rng, "curl/8.4.0", "Go-http-client/2.0", "Mozilla/5.0", "kube-probe/1.30")},
				{Key: "X-Forwarded-For", Value: fmt.Sprintf("10.0.%d.%d", s.rng.Intn(4), s.rng.Intn(255))},
				{Key: "X-Request-Id", Value: fmt.Sprintf("%08x", s.rng.Int31())},
			},
		}
		if s.rng.Float64() < e.errProb {
			l7.Status = pick(s.rng, uint32(500), 502, 503)
			l7.LatencyMs = 200 + s.rng.Float64()*1800
		} else if s.rng.Float64() < 0.06 {
			l7.Status = pick(s.rng, uint32(404), 401, 429)
		}
		f.L7 = l7
	case "dns":
		l7 := &hubble.L7{Type: "dns", DNSQuery: dnsNames[s.rng.Intn(len(dnsNames))], DNSRcode: "NoError", LatencyMs: 0.3 + s.rng.Float64()*4}
		if s.rng.Float64() < e.errProb {
			l7.DNSRcode = pick(s.rng, "NXDomain", "ServFail")
		}
		f.L7 = l7
	}
	return f
}

func (s *HubbleSource) endpoint(sv service) hubble.Endpoint {
	if sv == world {
		return hubble.Endpoint{Labels: []string{"reserved:world"}, Workload: "world", Identity: 2}
	}
	return hubble.Endpoint{
		Namespace: sv.ns,
		Workload:  sv.workload,
		PodName:   fmt.Sprintf("%s-%04x-%c%c", sv.workload, s.rng.Intn(3)+0x6b00, 'a'+rune(s.rng.Intn(26)), 'a'+rune(s.rng.Intn(26))),
		Identity:  10000 + uint32(len(sv.workload)*137%5000),
		Labels:    []string{"k8s:app=" + sv.workload, "k8s:io.kubernetes.pod.namespace=" + sv.ns},
	}
}

func ns(sv service) string {
	if sv.ns == "" {
		return "world"
	}
	return sv.ns
}

func pick[T any](r *rand.Rand, vals ...T) T { return vals[r.Intn(len(vals))] }
