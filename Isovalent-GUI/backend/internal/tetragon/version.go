package tetragon

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	tetragonpb "github.com/isovalent-control/isovalent-control/backend/pkg/protos/tetragon"
)

// Version asks the Tetragon agent for its own version over gRPC. This is the
// authoritative answer — better than reading a DaemonSet image tag, which only
// tells you what was requested, not what is running.
func Version(ctx context.Context, addr string) (string, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return "", fmt.Errorf("tetragon dial %s: %w", addr, err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	resp, err := tetragonpb.NewFineGuidanceSensorsClient(conn).
		GetVersion(ctx, &tetragonpb.GetVersionRequest{})
	if err != nil {
		return "", err
	}
	if resp.GetVersion() == "" {
		return "", fmt.Errorf("tetragon returned an empty version")
	}
	return resp.GetVersion(), nil
}
