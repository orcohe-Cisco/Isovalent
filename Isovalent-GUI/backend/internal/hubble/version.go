package hubble

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	observerpb "github.com/isovalent-control/isovalent-control/backend/pkg/protos/observer"
)

// Status is what Hubble Relay reports about itself. The version string is
// Cilium's own version, which makes this the cheapest reliable way to answer
// "what Cilium am I actually talking to" without any Kubernetes RBAC.
type Status struct {
	Version            string
	NumFlows           uint64
	MaxFlows           uint64
	ConnectedNodes     uint32
	UnavailableNodes   uint32
	UnavailableDetails []string
}

// ServerStatus queries Hubble Relay.
func ServerStatus(ctx context.Context, addr string) (Status, error) {
	var out Status
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return out, fmt.Errorf("hubble relay dial %s: %w", addr, err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	resp, err := observerpb.NewObserverClient(conn).
		ServerStatus(ctx, &observerpb.ServerStatusRequest{})
	if err != nil {
		return out, err
	}
	out = Status{
		Version:            resp.GetVersion(),
		NumFlows:           resp.GetNumFlows(),
		MaxFlows:           resp.GetMaxFlows(),
		UnavailableDetails: resp.GetUnavailableNodes(),
	}
	if n := resp.GetNumConnectedNodes(); n != nil {
		out.ConnectedNodes = n.GetValue()
	}
	if n := resp.GetNumUnavailableNodes(); n != nil {
		out.UnavailableNodes = n.GetValue()
	}
	return out, nil
}
