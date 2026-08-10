// Package alerts implements the notification router: it fans alerts out to
// configured sinks (Slack, generic webhook, PagerDuty, Splunk/SIEM), applying
// severity filtering and time-based deduplication/suppression first.
package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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
)

// Route is one configured destination + filters.
type Route struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Type        SinkType `json:"type"`
	URL         string   `json:"url"`             // webhook / slack / splunk HEC URL
	Token       string   `json:"token,omitempty"` // PagerDuty routing key / Splunk HEC token
	MinSeverity string   `json:"minSeverity"`     // warning | critical
	Kinds       []string `json:"kinds,omitempty"` // empty = all kinds
	Enabled     bool     `json:"enabled"`
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
	payload, contentType := render(route, a)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, route.URL, bytes.NewReader(payload))
	if err != nil {
		r.bump(&r.failed)
		return
	}
	req.Header.Set("Content-Type", contentType)
	if route.Type == SinkSplunk && route.Token != "" {
		req.Header.Set("Authorization", "Splunk "+route.Token)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		slog.Warn("alert delivery failed", "route", route.Name, "err", err)
		r.bump(&r.failed)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		slog.Warn("alert sink non-2xx", "route", route.Name, "status", resp.StatusCode)
		r.bump(&r.failed)
		return
	}
	r.bump(&r.delivered)
}

func (r *Router) bump(p *int64) {
	r.mu.Lock()
	*p++
	r.mu.Unlock()
}

// SendTest delivers a synthetic alert to one route synchronously and returns
// any delivery error (used by the "Test" button in the UI).
func (r *Router) SendTest(route Route) error {
	a := Alert{
		Time: time.Now(), Severity: "warning", Kind: "test",
		Title:  "isovalent-control test alert",
		Detail: "If you can read this, the route works.",
	}
	payload, contentType := render(route, a)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, route.URL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	if route.Type == SinkSplunk && route.Token != "" {
		req.Header.Set("Authorization", "Splunk "+route.Token)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("sink returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
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
