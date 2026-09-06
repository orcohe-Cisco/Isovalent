package hits

import (
	"strings"
	"testing"
	"time"

	"github.com/isovalent-control/isovalent-control/backend/internal/tetragon"
)

func ev(policy, binary, arg string, enforced bool) tetragon.Event {
	e := tetragon.Event{
		Time: time.Now(), Policy: policy, Binary: binary, Function: "security_file_open",
		Namespace: "logging", Workload: "fluentd", Pod: "fluentd-abc",
		ArgList: []string{arg}, Labels: map[string]string{"app": "fluentd", "pod-template-hash": "abc123"},
	}
	if enforced {
		e.Action = "SIGKILL"
	}
	return e
}

func TestRecordCountsByDimension(t *testing.T) {
	tr := New()
	for i := 0; i < 5; i++ {
		tr.Record(ev("files", "/usr/bin/fluent-bit", "/var/log/app.log", false))
	}
	tr.Record(ev("files", "/bin/cat", "/etc/shadow", true))

	sums := tr.Summaries()
	if len(sums) != 1 || sums[0].Total != 6 || sums[0].Enforced != 1 {
		t.Fatalf("unexpected summary: %+v", sums)
	}
	if sums[0].TopBinary != "/usr/bin/fluent-bit" {
		t.Fatalf("loudest binary should surface in the row, got %q", sums[0].TopBinary)
	}

	d, ok := tr.Detail("", "files")
	if !ok {
		t.Fatal("detail missing")
	}
	if got := d.Values[DimBinary][0]; got.Value != "/usr/bin/fluent-bit" || got.Count != 5 {
		t.Fatalf("binary breakdown wrong: %+v", got)
	}
	// The parent directory is recorded too, because a prefix exclusion is
	// normally written against the directory rather than the exact file.
	var sawDir bool
	for _, v := range d.Values[DimPath] {
		if v.Value == "/var/log/" {
			sawDir = true
			if v.ArgIndex != 0 {
				t.Fatalf("path must carry the argument index it came from, got %d", v.ArgIndex)
			}
		}
	}
	if !sawDir {
		t.Fatalf("parent directory not recorded: %+v", d.Values[DimPath])
	}
}

func TestIgnoresGeneratedPodLabels(t *testing.T) {
	tr := New()
	tr.Record(ev("files", "/bin/cat", "/etc/passwd", false))
	d, _ := tr.Detail("", "files")
	for _, v := range d.Values[DimPodLabel] {
		if strings.HasPrefix(v.Value, "pod-template-hash") {
			t.Fatal("pod-template-hash is useless as an exclusion and must not be offered")
		}
	}
}

func TestEventsWithoutAPolicyAreIgnored(t *testing.T) {
	tr := New()
	tr.Record(tetragon.Event{Binary: "/bin/ls"})
	if p, e, _ := tr.Totals(); p != 0 || e != 0 {
		t.Fatalf("base sensor events must not be attributed to a policy: %d %d", p, e)
	}
}

func TestExcludableDimensions(t *testing.T) {
	// Only the five Tetragon can actually express belong here. Offering one
	// it cannot express produces an exclusion that silently does nothing.
	for _, d := range []string{DimBinary, DimParentBinary, DimPath, DimPodLabel, DimContainer} {
		if !Excludable(d) {
			t.Fatalf("%s should be excludable", d)
		}
	}
	for _, d := range []string{DimUser, DimNamespace, DimNode, DimPod, DimHook, DimWorkload} {
		if Excludable(d) {
			t.Fatalf("%s cannot be written into a TracingPolicy and must not be offered", d)
		}
	}
}
