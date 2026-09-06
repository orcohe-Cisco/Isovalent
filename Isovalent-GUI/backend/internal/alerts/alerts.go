// Package alerts implements the notification router: it fans alerts out to
// configured sinks (Slack, generic webhook, PagerDuty, Splunk/SIEM), applying
// severity filtering and time-based deduplication/suppression first.
package alerts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Alert mirrors the server's alert shape (kept local to avoid an import cycle).
type Alert struct {
	Time      time.Time `json:"time"`
	Severity  string    `json:"severity"` // warning | critical
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Detail    string    `json:"detail,omitempty"`
	Namespace string    `json:"namespace,omitempty"`
	Workload  string    `json:"workload,omitempty"`
	Policy    string    `json:"policy,omitempty"`
}

// SinkType enumerates supported destinations.
type SinkType string

const (
	SinkSlack     SinkType = "slack"
	SinkWebhook   SinkType = "webhook"
	SinkPagerDuty SinkType = "pagerduty"
	SinkSplunk    SinkType = "splunk"
	// SinkWebex posts a markdown message to a Webex room via the Messages
	// API (URL https://webexapis.com/v1/messages, token = bot token,
	// room = target roomId).
	SinkWebex SinkType = "webex"
	// SinkTeams posts a MessageCard to an Incoming Webhook.
	SinkTeams SinkType = "teams"
	// SinkSyslog writes RFC5424 or CEF over UDP/TCP to a collector. URL form:
	// udp://host:514 or tcp://host:601.
	SinkSyslog SinkType = "syslog"
	// SinkElastic posts to an Elasticsearch/OpenSearch _doc endpoint.
	SinkElastic SinkType = "elastic"
	// SinkSentinel posts to a Microsoft Sentinel / Log Analytics DCE.
	SinkSentinel SinkType = "sentinel"
)

// SinkTypes is the list the UI offers, in menu order.
var SinkTypes = []SinkType{
	SinkSlack, SinkTeams, SinkWebex, SinkWebhook,
	SinkSyslog, SinkSplunk, SinkElastic, SinkSentinel, SinkPagerDuty,
}

// SinkSpec describes a sink for the integrations UI: what the fields mean for
// this particular destination, so nobody has to guess what "token" is.
type SinkSpec struct {
	Type        SinkType `json:"type"`
	Label       string   `json:"label"`
	URLLabel    string   `json:"urlLabel"`
	URLHint     string   `json:"urlHint"`
	TokenLabel  string   `json:"tokenLabel,omitempty"`
	TargetLabel string   `json:"targetLabel,omitempty"`
	TargetHint  string   `json:"targetHint,omitempty"`
	FormatOpts  []string `json:"formatOptions,omitempty"`
	Notes       string   `json:"notes"`
}

// Specs describes every sink for the UI.
func Specs() []SinkSpec {
	return []SinkSpec{
		{Type: SinkSlack, Label: "Slack", URLLabel: "Incoming webhook URL", URLHint: "https://hooks.slack.com/services/…",
			Notes: "Create an Incoming Webhook in the Slack app directory; no token needed."},
		{Type: SinkTeams, Label: "Microsoft Teams", URLLabel: "Incoming webhook URL", URLHint: "https://…webhook.office.com/webhookb2/…",
			Notes: "Teams connector webhook. Posts a MessageCard so it renders as a card, not raw JSON."},
		{Type: SinkWebex, Label: "Webex", URLLabel: "Messages API URL", URLHint: "https://webexapis.com/v1/messages",
			TokenLabel: "Bot access token", TargetLabel: "Room ID", TargetHint: "Y2lzY29zcGFyazovL3VzL1JPT00v…",
			Notes: "Create a bot at developer.webex.com, add it to the room, and paste the roomId."},
		{Type: SinkWebhook, Label: "Generic webhook", URLLabel: "Endpoint URL", URLHint: "https://example.internal/hooks/security",
			TokenLabel: "Bearer token (optional)", Notes: "Posts the raw alert JSON. The simplest thing that can possibly work."},
		{Type: SinkSyslog, Label: "Syslog / SIEM", URLLabel: "Collector", URLHint: "udp://siem.internal:514 or tcp://siem.internal:601",
			TargetLabel: "Facility", TargetHint: "local0", FormatOpts: []string{"rfc5424", "cef"},
			Notes: "RFC5424 for a normal syslog collector; CEF for ArcSight, QRadar and most SIEM parsers."},
		{Type: SinkSplunk, Label: "Splunk HEC", URLLabel: "HEC URL", URLHint: "https://splunk.internal:8088/services/collector",
			TokenLabel: "HEC token", Notes: "Events are sent with sourcetype isovalent:control:alert."},
		{Type: SinkElastic, Label: "Elasticsearch / OpenSearch", URLLabel: "Index URL", URLHint: "https://es.internal:9200/security-alerts/_doc",
			TokenLabel: "API key or basic auth", Notes: "Posts one document per alert. Use an API key rather than a user password."},
		{Type: SinkSentinel, Label: "Microsoft Sentinel", URLLabel: "DCE logs ingestion URL", URLHint: "https://….ingest.monitor.azure.com/dataCollectionRules/…/streams/Custom-IsovalentControl?api-version=2023-01-01",
			TokenLabel: "Bearer token", Notes: "Data Collection Rule ingestion endpoint; the token is an Entra access token for monitor.azure.com."},
		{Type: SinkPagerDuty, Label: "PagerDuty", URLLabel: "Events API URL", URLHint: "https://events.pagerduty.com/v2/enqueue",
			TokenLabel: "Routing key", Notes: "Only route critical severities here unless you enjoy being paged."},
	}
}

// Route is one configured destination + filters.
type Route struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Type        SinkType `json:"type"`
	URL         string   `json:"url"`              // webhook / slack / splunk HEC URL
	Token       string   `json:"token,omitempty"`  // PagerDuty routing key / Splunk HEC token / bot token
	Target      string   `json:"target,omitempty"` // Webex roomId, syslog facility
	Format      string   `json:"format,omitempty"` // syslog: rfc5424 | cef
	MinSeverity string   `json:"minSeverity"`      // warning | critical
	Kinds       []string `json:"kinds,omitempty"`  // empty = all kinds
	Enabled     bool     `json:"enabled"`
	// Namespaces optionally restricts a route to specific namespaces, so a
	// team's channel only sees its own noise.
	Namespaces []string `json:"namespaces,omitempty"`
}

func sevRank(s string) int {
	if s == "critical" {
		return 2
	}
	return 1
}

func (r Route) matches(a Alert) bool {
	if !r.Enabled {
		return false
	}
	if sevRank(a.Severity) < sevRank(r.MinSeverity) {
		return false
	}
	if len(r.Kinds) > 0 {
		found := false
		for _, k := range r.Kinds {
			if k == a.Kind {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(r.Namespaces) > 0 && a.Namespace != "" {
		found := false
		for _, ns := range r.Namespaces {
			if ns == a.Namespace {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// Router holds routes, a suppression window, and delivery stats.
type Router struct {
	mu          sync.RWMutex
	routes      []Route
	lastSent    map[string]time.Time // dedup key -> last delivery
	suppressFor time.Duration
	client      *http.Client
	delivered   int64
	suppressed  int64
	failed      int64
}

// NewRouter returns a router with a default 60s suppression window.
func NewRouter() *Router {
	return &Router{
		lastSent:    map[string]time.Time{},
		suppressFor: 60 * time.Second,
		client:      &http.Client{Timeout: 8 * time.Second},
	}
}

// SetRoutes replaces the route table.
func (r *Router) SetRoutes(routes []Route) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes = routes
}

// Routes returns a copy of the current routes.
func (r *Router) Routes() []Route {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Route, len(r.routes))
	copy(out, r.routes)
	return out
}

// Stats returns delivery counters.
func (r *Router) Stats() map[string]int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return map[string]int64{"delivered": r.delivered, "suppressed": r.suppressed, "failed": r.failed}
}

// Dispatch routes one alert to all matching sinks (deduplicated).
func (r *Router) Dispatch(a Alert) {
	r.mu.Lock()
	routes := make([]Route, len(r.routes))
	copy(routes, r.routes)
	key := a.Kind + "|" + a.Title
	if last, ok := r.lastSent[key]; ok && a.Time.Sub(last) < r.suppressFor {
		r.suppressed++
		r.mu.Unlock()
		return
	}
	r.lastSent[key] = a.Time
	r.mu.Unlock()

	for _, route := range routes {
		if !route.matches(a) {
			continue
		}
		go r.deliver(route, a)
	}
}

func (r *Router) deliver(route Route, a Alert) {
	if err := r.send(route, a); err != nil {
		slog.Warn("alert delivery failed", "route", route.Name, "type", route.Type, "err", err)
		r.bump(&r.failed)
		return
	}
	r.bump(&r.delivered)
}

// send performs one delivery synchronously and returns the error, so both the
// async path and the "Test" button share exactly one implementation. A test
// that exercises different code from the real thing is worse than no test.
func (r *Router) send(route Route, a Alert) error {
	if route.Type == SinkSyslog {
		return sendSyslog(route, a)
	}
	payload, contentType := render(route, a)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, route.URL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	switch route.Type {
	case SinkSplunk:
		if route.Token != "" {
			req.Header.Set("Authorization", "Splunk "+route.Token)
		}
	case SinkWebex, SinkWebhook, SinkSentinel:
		if route.Token != "" {
			req.Header.Set("Authorization", "Bearer "+route.Token)
		}
	case SinkElastic:
		if route.Token != "" {
			// An Elasticsearch API key is presented as ApiKey; a base64
			// "user:pass" is Basic. Callers paste one or the other.
			if strings.Contains(route.Token, ":") {
				req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(route.Token)))
			} else {
				req.Header.Set("Authorization", "ApiKey "+route.Token)
			}
		}
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("sink returned %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// sendSyslog writes one datagram/line to a syslog or SIEM collector.
func sendSyslog(route Route, a Alert) error {
	network, addr := "udp", route.URL
	if i := strings.Index(addr, "://"); i > 0 {
		network, addr = addr[:i], addr[i+3:]
	}
	switch network {
	case "udp", "tcp":
	default:
		return fmt.Errorf("syslog collector must be udp://host:port or tcp://host:port (got %q)", route.URL)
	}
	conn, err := net.DialTimeout(network, addr, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	line := renderSyslog(route, a)
	if network == "tcp" {
		// RFC6587 octet counting keeps multi-line messages framed correctly.
		line = fmt.Sprintf("%d %s", len(line), line)
	}
	_, err = io.WriteString(conn, line)
	return err
}

func (r *Router) bump(p *int64) {
	r.mu.Lock()
	*p++
	r.mu.Unlock()
}

// SendTest delivers a synthetic alert to one route synchronously and returns
// any delivery error (used by the "Test" button in the UI).
func (r *Router) SendTest(route Route) error {
	return r.send(route, Alert{
		Time: time.Now(), Severity: "warning", Kind: "test",
		Title:     "isovalent-control test alert",
		Detail:    "If you can read this, the route works.",
		Namespace: "isovalent-control",
	})
}

// render produces the sink-specific payload.
func render(route Route, a Alert) (body []byte, contentType string) {
	switch route.Type {
	case SinkSlack:
		text := fmt.Sprintf("*[%s] %s*\n%s", up(a.Severity), a.Title, a.Detail)
		if a.Namespace != "" {
			text += fmt.Sprintf("\nns=`%s` workload=`%s`", a.Namespace, a.Workload)
		}
		if a.Policy != "" {
			text += fmt.Sprintf(" policy=`%s`", a.Policy)
		}
		b, _ := json.Marshal(map[string]string{"text": text})
		return b, "application/json"
	case SinkPagerDuty:
		b, _ := json.Marshal(map[string]any{
			"routing_key":  route.Token,
			"event_action": "trigger",
			"payload": map[string]any{
				"summary":   a.Title,
				"severity":  pdSeverity(a.Severity),
				"source":    "isovalent-control",
				"component": a.Workload,
				"group":     a.Namespace,
				"class":     a.Kind,
				"custom_details": map[string]string{
					"detail": a.Detail, "policy": a.Policy,
				},
			},
		})
		return b, "application/json"
	case SinkTeams:
		facts := []any{
			map[string]string{"name": "Severity", "value": up(a.Severity)},
			map[string]string{"name": "Kind", "value": a.Kind},
		}
		if a.Namespace != "" {
			facts = append(facts, map[string]string{"name": "Namespace", "value": a.Namespace})
		}
		if a.Workload != "" {
			facts = append(facts, map[string]string{"name": "Workload", "value": a.Workload})
		}
		if a.Policy != "" {
			facts = append(facts, map[string]string{"name": "Policy", "value": a.Policy})
		}
		b, _ := json.Marshal(map[string]any{
			"@type": "MessageCard", "@context": "https://schema.org/extensions",
			"themeColor": themeColor(a.Severity),
			"summary":    a.Title,
			"sections": []any{map[string]any{
				"activityTitle":    a.Title,
				"activitySubtitle": "isovalent-control",
				"text":             a.Detail,
				"facts":            facts,
			}},
		})
		return b, "application/json"
	case SinkWebex:
		md := fmt.Sprintf("**[%s] %s**", up(a.Severity), a.Title)
		if a.Detail != "" {
			md += "\n\n" + a.Detail
		}
		if a.Namespace != "" {
			md += fmt.Sprintf("\n\n`ns=%s` `workload=%s`", a.Namespace, a.Workload)
		}
		if a.Policy != "" {
			md += fmt.Sprintf(" `policy=%s`", a.Policy)
		}
		b, _ := json.Marshal(map[string]string{"roomId": route.Target, "markdown": md})
		return b, "application/json"
	case SinkElastic:
		b, _ := json.Marshal(map[string]any{
			"@timestamp": a.Time.UTC().Format(time.RFC3339Nano),
			"event":      map[string]string{"kind": "alert", "module": "isovalent-control", "severity": a.Severity},
			"message":    a.Title,
			"alert":      a,
		})
		return b, "application/json"
	case SinkSentinel:
		b, _ := json.Marshal([]any{map[string]any{
			"TimeGenerated": a.Time.UTC().Format(time.RFC3339),
			"Severity":      a.Severity, "Kind": a.Kind, "Title": a.Title,
			"Detail": a.Detail, "Namespace": a.Namespace, "Workload": a.Workload, "Policy": a.Policy,
		}})
		return b, "application/json"
	case SinkSplunk:
		// Splunk HEC event envelope.
		b, _ := json.Marshal(map[string]any{
			"sourcetype": "isovalent:control:alert",
			"event":      a,
		})
		return b, "application/json"
	default: // generic webhook / SIEM
		b, _ := json.Marshal(a)
		return b, "application/json"
	}
}

// renderSyslog produces either an RFC5424 line or an ArcSight CEF line.
func renderSyslog(route Route, a Alert) string {
	ts := a.Time.UTC().Format(time.RFC3339)
	host := "isovalent-control"
	if route.Format == "cef" {
		// CEF:0|Vendor|Product|Version|SignatureID|Name|Severity|Extension
		ext := fmt.Sprintf("rt=%d msg=%s cs1Label=policy cs1=%s cs2Label=namespace cs2=%s cs3Label=workload cs3=%s",
			a.Time.UnixMilli(), escapeCEF(a.Detail), escapeCEF(a.Policy), escapeCEF(a.Namespace), escapeCEF(a.Workload))
		return fmt.Sprintf("CEF:0|Isovalent|isovalent-control|1.0|%s|%s|%d|%s\n",
			escapeCEF(a.Kind), escapeCEF(a.Title), cefSeverity(a.Severity), ext)
	}
	// RFC5424: <PRI>1 TIMESTAMP HOST APP PROCID MSGID STRUCTURED-DATA MSG
	pri := 8*localFacility(route.Target) + syslogSeverity(a.Severity)
	sd := fmt.Sprintf(`[isovalent@0 kind="%s" namespace="%s" workload="%s" policy="%s"]`,
		escapeSD(a.Kind), escapeSD(a.Namespace), escapeSD(a.Workload), escapeSD(a.Policy))
	msg := a.Title
	if a.Detail != "" {
		msg += " — " + a.Detail
	}
	return fmt.Sprintf("<%d>1 %s %s isovalent-control - %s %s %s\n", pri, ts, host, a.Kind, sd, msg)
}

func localFacility(name string) int {
	if strings.HasPrefix(name, "local") {
		if n, err := strconv.Atoi(strings.TrimPrefix(name, "local")); err == nil && n >= 0 && n <= 7 {
			return 16 + n
		}
	}
	return 16 // local0
}

func syslogSeverity(s string) int {
	if s == "critical" {
		return 2 // crit
	}
	return 4 // warning
}

func cefSeverity(s string) int {
	if s == "critical" {
		return 9
	}
	return 5
}

func escapeCEF(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "|", "\\|", "=", "\\=", "\n", " ")
	return r.Replace(s)
}

func escapeSD(s string) string {
	r := strings.NewReplacer("\\", "\\\\", `"`, `\"`, "]", "\\]")
	return r.Replace(s)
}

func themeColor(sev string) string {
	if sev == "critical" {
		return "D93025"
	}
	return "E8A33D"
}

func up(s string) string {
	if s == "critical" {
		return "CRITICAL"
	}
	return "WARNING"
}

func pdSeverity(s string) string {
	if s == "critical" {
		return "critical"
	}
	return "warning"
}
