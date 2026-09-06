package alerts

import (
	"strings"
	"testing"
	"time"
)

var sample = Alert{
	Time: time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC), Severity: "critical",
	Kind: "runtime_enforcement", Title: "Tetragon SIGKILL: /usr/bin/nc",
	Detail: "security_bprm_check", Namespace: "shop", Workload: "cart", Policy: "block-nc",
}

func TestSyslogRFC5424(t *testing.T) {
	line := renderSyslog(Route{Type: SinkSyslog, Format: "rfc5424", Target: "local3"}, sample)
	if !strings.HasPrefix(line, "<") {
		t.Fatalf("missing priority: %q", line)
	}
	// local3 = facility 19, critical = severity 2 -> 19*8+2 = 154
	if !strings.HasPrefix(line, "<154>1 ") {
		t.Fatalf("wrong facility/severity encoding: %q", line)
	}
	for _, want := range []string{`namespace="shop"`, `policy="block-nc"`, "2026-08-18T12:00:00Z"} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %q in %q", want, line)
		}
	}
}

func TestSyslogCEF(t *testing.T) {
	line := renderSyslog(Route{Type: SinkSyslog, Format: "cef"}, sample)
	if !strings.HasPrefix(line, "CEF:0|Isovalent|isovalent-control|") {
		t.Fatalf("not a CEF header: %q", line)
	}
	if !strings.Contains(line, "cs1=block-nc") {
		t.Fatalf("policy not mapped to a custom string field: %q", line)
	}
	// Severity 9 is the CEF encoding of critical.
	if !strings.Contains(line, "|9|") {
		t.Fatalf("critical should map to CEF severity 9: %q", line)
	}
}

func TestWebexUsesTheConfiguredRoom(t *testing.T) {
	body, ct := render(Route{Type: SinkWebex, Target: "ROOMID"}, sample)
	if ct != "application/json" {
		t.Fatalf("unexpected content type %q", ct)
	}
	if !strings.Contains(string(body), `"roomId":"ROOMID"`) {
		t.Fatalf("roomId missing: %s", body)
	}
}

func TestNamespaceScopedRouteFiltersOtherNamespaces(t *testing.T) {
	r := Route{Enabled: true, MinSeverity: "warning", Namespaces: []string{"pay"}}
	if r.matches(sample) {
		t.Fatal("a route scoped to pay must not receive a shop alert")
	}
	r.Namespaces = []string{"shop"}
	if !r.matches(sample) {
		t.Fatal("a route scoped to shop must receive a shop alert")
	}
}

func TestSeverityFloor(t *testing.T) {
	warning := sample
	warning.Severity = "warning"
	r := Route{Enabled: true, MinSeverity: "critical"}
	if r.matches(warning) {
		t.Fatal("a critical-only route must not receive warnings")
	}
}

func TestSpecsCoverEverySinkType(t *testing.T) {
	have := map[SinkType]bool{}
	for _, s := range Specs() {
		have[s.Type] = true
	}
	for _, st := range SinkTypes {
		if !have[st] {
			t.Fatalf("sink %s has no UI spec, so its fields would be unlabelled", st)
		}
	}
}
