package hubble

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	flowpb "github.com/isovalent-control/isovalent-control/backend/pkg/protos/flow"
	observerpb "github.com/isovalent-control/isovalent-control/backend/pkg/protos/observer"
)

// LiveSource streams flows from a Hubble Relay gRPC endpoint.
type LiveSource struct {
	Addr string
}

// NewLiveSource returns a source connected to the given Hubble Relay address
// (e.g. "hubble-relay.kube-system.svc:80" or a local port-forward "localhost:4245").
func NewLiveSource(addr string) *LiveSource { return &LiveSource{Addr: addr} }

// Flows implements Source. It reconnects with backoff until ctx is cancelled.
func (s *LiveSource) Flows(ctx context.Context) (<-chan Flow, error) {
	conn, err := grpc.NewClient(s.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("hubble relay dial %s: %w", s.Addr, err)
	}
	out := make(chan Flow, 256)
	go func() {
		defer close(out)
		defer conn.Close()
		client := observerpb.NewObserverClient(conn)
		backoff := time.Second
		for ctx.Err() == nil {
			if err := s.stream(ctx, client, out); err != nil && ctx.Err() == nil {
				slog.Warn("hubble stream interrupted; reconnecting", "err", err, "backoff", backoff)
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					return
				}
				if backoff < 30*time.Second {
					backoff *= 2
				}
			} else {
				backoff = time.Second
			}
		}
	}()
	return out, nil
}

func (s *LiveSource) stream(ctx context.Context, client observerpb.ObserverClient, out chan<- Flow) error {
	stream, err := client.GetFlows(ctx, &observerpb.GetFlowsRequest{Follow: true})
	if err != nil {
		return err
	}
	for {
		resp, err := stream.Recv()
		if err != nil {
			return err
		}
		f := resp.GetFlow()
		if f == nil {
			continue // node status / lost events
		}
		select {
		case out <- convertFlow(f):
		case <-ctx.Done():
			return ctx.Err()
		default: // drop rather than backpressure the relay
		}
	}
}

func convertFlow(f *flowpb.Flow) Flow {
	nf := Flow{
		Verdict:     f.GetVerdict().String(),
		Direction:   strings.TrimSuffix(f.GetTrafficDirection().String(), "_DIRECTION_UNKNOWN"),
		Node:        f.GetNodeName(),
		Summary:     f.GetSummary(),
		Source:      convertEndpoint(f.GetSource()),
		Destination: convertEndpoint(f.GetDestination()),
	}
	if t := f.GetTime(); t != nil {
		nf.Time = t.AsTime()
	}
	if f.GetVerdict() == flowpb.Verdict_DROPPED {
		nf.DropReason = f.GetDropReasonDesc().String()
	}
	switch l4 := f.GetL4().GetProtocol().(type) {
	case *flowpb.Layer4_TCP:
		nf.L4 = L4{Protocol: "TCP", SrcPort: l4.TCP.GetSourcePort(), DstPort: l4.TCP.GetDestinationPort()}
	case *flowpb.Layer4_UDP:
		nf.L4 = L4{Protocol: "UDP", SrcPort: l4.UDP.GetSourcePort(), DstPort: l4.UDP.GetDestinationPort()}
	case *flowpb.Layer4_ICMPv4:
		nf.L4 = L4{Protocol: "ICMP"}
	case *flowpb.Layer4_ICMPv6:
		nf.L4 = L4{Protocol: "ICMPv6"}
	}
	if l7 := f.GetL7(); l7 != nil {
		nl7 := &L7{LatencyMs: float64(l7.GetLatencyNs()) / 1e6}
		switch rec := l7.GetRecord().(type) {
		case *flowpb.Layer7_Http:
			nl7.Type = "http"
			nl7.Method = rec.Http.GetMethod()
			nl7.URL = rec.Http.GetUrl()
			nl7.Protocol = rec.Http.GetProtocol()
			nl7.Status = rec.Http.GetCode()
			for _, h := range rec.Http.GetHeaders() {
				nl7.Headers = append(nl7.Headers, Header{Key: h.GetKey(), Value: h.GetValue()})
			}
		case *flowpb.Layer7_Dns:
			nl7.Type = "dns"
			nl7.DNSQuery = rec.Dns.GetQuery()
			nl7.DNSRcode = dnsRcode(rec.Dns.GetRcode())
		case *flowpb.Layer7_Kafka:
			nl7.Type = "kafka"
		}
		nf.L7 = nl7
	}
	return nf
}

func convertEndpoint(e *flowpb.Endpoint) Endpoint {
	if e == nil {
		return Endpoint{}
	}
	ep := Endpoint{
		Namespace: e.GetNamespace(),
		PodName:   e.GetPodName(),
		Identity:  e.GetIdentity(),
		Labels:    e.GetLabels(),
	}
	if w := e.GetWorkloads(); len(w) > 0 {
		ep.Workload = w[0].GetName()
	}
	return ep
}

func dnsRcode(rc uint32) string {
	switch rc {
	case 0:
		return "NoError"
	case 1:
		return "FormErr"
	case 2:
		return "ServFail"
	case 3:
		return "NXDomain"
	case 5:
		return "Refused"
	default:
		return fmt.Sprintf("Rcode(%d)", rc)
	}
}
