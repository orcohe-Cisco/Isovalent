package tetragon

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	tetragonpb "github.com/isovalent-control/isovalent-control/backend/pkg/protos/tetragon"
)

// LiveSource streams events from a Tetragon gRPC endpoint.
type LiveSource struct {
	Addr string
}

// NewLiveSource returns a source connected to the given Tetragon address
// (e.g. "tetragon.kube-system.svc:54321").
func NewLiveSource(addr string) *LiveSource { return &LiveSource{Addr: addr} }

// Events implements Source. It reconnects with backoff until ctx is cancelled.
func (s *LiveSource) Events(ctx context.Context) (<-chan Event, error) {
	conn, err := grpc.NewClient(s.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("tetragon dial %s: %w", s.Addr, err)
	}
	out := make(chan Event, 256)
	go func() {
		defer close(out)
		defer conn.Close()
		client := tetragonpb.NewFineGuidanceSensorsClient(conn)
		backoff := time.Second
		for ctx.Err() == nil {
			if err := s.stream(ctx, client, out); err != nil && ctx.Err() == nil {
				slog.Warn("tetragon stream interrupted; reconnecting", "err", err, "backoff", backoff)
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

func (s *LiveSource) stream(ctx context.Context, client tetragonpb.FineGuidanceSensorsClient, out chan<- Event) error {
	stream, err := client.GetEvents(ctx, &tetragonpb.GetEventsRequest{})
	if err != nil {
		return err
	}
	for {
		resp, err := stream.Recv()
		if err != nil {
			return err
		}
		ev, ok := convertEvent(resp)
		if !ok {
			continue
		}
		select {
		case out <- ev:
		case <-ctx.Done():
			return ctx.Err()
		default: // drop rather than backpressure the agent
		}
	}
}

func convertEvent(resp *tetragonpb.GetEventsResponse) (Event, bool) {
	ev := Event{Node: resp.GetNodeName()}
	if t := resp.GetTime(); t != nil {
		ev.Time = t.AsTime()
	}
	switch e := resp.GetEvent().(type) {
	case *tetragonpb.GetEventsResponse_ProcessExec:
		ev.Type = "process_exec"
		fillProcess(&ev, e.ProcessExec.GetProcess(), e.ProcessExec.GetParent())
	case *tetragonpb.GetEventsResponse_ProcessExit:
		ev.Type = "process_exit"
		fillProcess(&ev, e.ProcessExit.GetProcess(), e.ProcessExit.GetParent())
		if sig := e.ProcessExit.GetSignal(); sig != "" {
			ev.Details = "signal " + sig
		}
	case *tetragonpb.GetEventsResponse_ProcessKprobe:
		k := e.ProcessKprobe
		ev.Type = "process_kprobe"
		fillProcess(&ev, k.GetProcess(), k.GetParent())
		ev.Function = k.GetFunctionName()
		ev.Action = strings.TrimPrefix(k.GetAction().String(), "KPROBE_ACTION_")
		ev.Policy = k.GetPolicyName()
		ev.Details = kprobeArgs(k.GetArgs())
	case *tetragonpb.GetEventsResponse_ProcessTracepoint:
		t := e.ProcessTracepoint
		ev.Type = "process_tracepoint"
		fillProcess(&ev, t.GetProcess(), t.GetParent())
		ev.Function = t.GetSubsys() + "/" + t.GetEvent()
		ev.Policy = t.GetPolicyName()
	default:
		return ev, false
	}
	return ev, true
}

func fillProcess(ev *Event, p, parent *tetragonpb.Process) {
	if p != nil {
		ev.Binary = p.GetBinary()
		ev.Args = p.GetArguments()
		if pod := p.GetPod(); pod != nil {
			ev.Namespace = pod.GetNamespace()
			ev.Pod = pod.GetName()
			ev.Workload = pod.GetWorkload()
		}
	}
	if parent != nil {
		ev.Parent = parent.GetBinary()
	}
}

func kprobeArgs(args []*tetragonpb.KprobeArgument) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		switch v := a.GetArg().(type) {
		case *tetragonpb.KprobeArgument_StringArg:
			parts = append(parts, v.StringArg)
		case *tetragonpb.KprobeArgument_FileArg:
			parts = append(parts, v.FileArg.GetPath())
		case *tetragonpb.KprobeArgument_PathArg:
			parts = append(parts, v.PathArg.GetPath())
		case *tetragonpb.KprobeArgument_IntArg:
			parts = append(parts, fmt.Sprintf("%d", v.IntArg))
		case *tetragonpb.KprobeArgument_SockArg:
			parts = append(parts, fmt.Sprintf("%s:%d", v.SockArg.GetDaddr(), v.SockArg.GetDport()))
		case *tetragonpb.KprobeArgument_SkbArg:
			parts = append(parts, fmt.Sprintf("%s:%d", v.SkbArg.GetDaddr(), v.SkbArg.GetDport()))
		}
	}
	return strings.Join(parts, " ")
}
