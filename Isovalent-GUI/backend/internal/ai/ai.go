// Package ai connects the console to an external LLM so it can read the
// current posture and propose changes.
//
// Two rules shape the design:
//
//  1. The model never applies anything. It returns structured suggestions;
//     every one of them has to be approved in the UI, and approval goes
//     through exactly the same audited endpoints a human click does. An agent
//     with write access to runtime enforcement is a bad idea in a lab and a
//     worse one in production.
//
//  2. The API key lives on the server, read from a Kubernetes Secret. It is
//     never sent to the browser, and the /ai/status endpoint reports only
//     whether a key is present.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Provider identifies the wire protocol to speak.
type Provider string

const (
	// ProviderAnthropic speaks the Anthropic Messages API.
	ProviderAnthropic Provider = "anthropic"
	// ProviderGemini speaks the Google Generative Language API.
	ProviderGemini Provider = "gemini"
	// ProviderOpenAI speaks the OpenAI chat-completions API, which is also
	// what most self-hosted gateways (vLLM, Ollama, LiteLLM, Azure OpenAI)
	// expose. Point BaseURL at yours.
	ProviderOpenAI Provider = "openai"
	// ProviderDisabled turns the feature off.
	ProviderDisabled Provider = "disabled"
)

// Config configures the client.
type Config struct {
	Provider Provider
	BaseURL  string
	Model    string
	APIKey   string
}

// DefaultModel per provider, used when IC_AI_MODEL is unset.
func DefaultModel(p Provider) string {
	switch p {
	case ProviderAnthropic:
		return "claude-sonnet-4-5"
	case ProviderGemini:
		return "gemini-2.0-flash"
	case ProviderOpenAI:
		return "gpt-4o-mini"
	}
	return ""
}

func defaultBase(p Provider) string {
	switch p {
	case ProviderAnthropic:
		return "https://api.anthropic.com"
	case ProviderGemini:
		return "https://generativelanguage.googleapis.com"
	case ProviderOpenAI:
		return "https://api.openai.com"
	}
	return ""
}

// Client talks to the configured provider.
type Client struct {
	cfg  Config
	http *http.Client
}

// New builds a client. It never fails: a misconfigured client reports
// Enabled() == false and the UI explains what is missing.
func New(cfg Config) *Client {
	if cfg.Model == "" {
		cfg.Model = DefaultModel(cfg.Provider)
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBase(cfg.Provider)
	}
	cfg.BaseURL = strings.TrimSuffix(cfg.BaseURL, "/")
	return &Client{cfg: cfg, http: &http.Client{Timeout: 90 * time.Second}}
}

// Status is the UI-facing configuration report. It deliberately contains no
// key material.
type Status struct {
	Enabled   bool     `json:"enabled"`
	Provider  string   `json:"provider"`
	Model     string   `json:"model,omitempty"`
	BaseURL   string   `json:"baseUrl,omitempty"`
	HasKey    bool     `json:"hasKey"`
	Reason    string   `json:"reason,omitempty"`
	Providers []string `json:"providers"`
}

// Status reports whether the assistant can run.
func (c *Client) Status() Status {
	s := Status{
		Provider:  string(c.cfg.Provider),
		Model:     c.cfg.Model,
		BaseURL:   c.cfg.BaseURL,
		HasKey:    c.cfg.APIKey != "",
		Providers: []string{"anthropic", "gemini", "openai", "disabled"},
	}
	switch {
	case c.cfg.Provider == ProviderDisabled || c.cfg.Provider == "":
		s.Reason = "set IC_AI_PROVIDER to anthropic, gemini or openai"
	case c.cfg.APIKey == "":
		s.Reason = "no API key: mount one via IC_AI_API_KEY_FILE (Kubernetes Secret) or set IC_AI_API_KEY"
	case c.cfg.Model == "":
		s.Reason = "set IC_AI_MODEL"
	default:
		s.Enabled = true
	}
	return s
}

// Enabled reports whether Suggest can be called.
func (c *Client) Enabled() bool { return c.Status().Enabled }

// Suggestion is one proposed change. Kind determines which console action the
// Approve button maps onto, so the model cannot invent an operation the
// console does not already support and audit.
type Suggestion struct {
	// Kind: enforce | monitor | exclude | disable | tune | investigate
	Kind string `json:"kind"`
	// Target policy.
	Policy    string `json:"policy"`
	Namespace string `json:"namespace,omitempty"`
	// Title is a one-line description shown in the list.
	Title string `json:"title"`
	// Rationale explains the evidence it used.
	Rationale string `json:"rationale"`
	// Confidence: high | medium | low.
	Confidence string `json:"confidence,omitempty"`
	// Risk explains what could break if this is applied.
	Risk string `json:"risk,omitempty"`
	// Exclusions carries the concrete values for kind=exclude.
	Exclusions struct {
		Binaries       []string `json:"binaries,omitempty"`
		ParentBinaries []string `json:"parentBinaries,omitempty"`
		Paths          []string `json:"paths,omitempty"`
		PodLabels      []string `json:"podLabels,omitempty"`
		Containers     []string `json:"containers,omitempty"`
	} `json:"exclusions,omitempty"`
}

// Response is the assistant's full reply.
type Response struct {
	Summary     string       `json:"summary"`
	Suggestions []Suggestion `json:"suggestions"`
	// Raw is the model's unparsed text, kept so a malformed reply is still
	// readable rather than silently dropped.
	Raw string `json:"raw,omitempty"`
	// Model and Provider identify what produced this.
	Model    string `json:"model"`
	Provider string `json:"provider"`
}

const systemPrompt = `You are a Kubernetes runtime- and network-security analyst reviewing a live Cilium + Tetragon deployment through the Isovalent Control console.

You will be given: the policies currently in the cluster, how many times each has fired, what fired them (process, path, user, pod label, container), and recent network drop statistics.

Propose concrete, conservative changes. Rules:
- Never propose enabling enforcement (Sigkill) on a policy whose hits still include processes you cannot confidently classify as malicious.
- Prefer narrowing a noisy policy with an exclusion over disabling it.
- A policy firing thousands of times per minute on one legitimate binary is an exclusion candidate, not a detection.
- Only propose "disable" when a policy is both noisy and provides no signal the others do not.
- Say plainly when the evidence is insufficient; an empty suggestion list is a valid answer.

Reply with JSON only, no prose outside it, in exactly this shape:
{"summary":"...","suggestions":[{"kind":"enforce|monitor|exclude|disable|tune|investigate","policy":"name","namespace":"","title":"...","rationale":"...","confidence":"high|medium|low","risk":"...","exclusions":{"binaries":[],"parentBinaries":[],"paths":[],"podLabels":[],"containers":[]}}]}`

// Suggest sends the posture context and parses the structured reply.
func (c *Client) Suggest(ctx context.Context, contextJSON []byte, question string) (*Response, error) {
	if !c.Enabled() {
		return nil, errors.New(c.Status().Reason)
	}
	user := "Current cluster posture:\n```json\n" + string(contextJSON) + "\n```\n"
	if strings.TrimSpace(question) != "" {
		user += "\nOperator question: " + question
	} else {
		user += "\nReview this posture and propose changes."
	}

	var text string
	var err error
	switch c.cfg.Provider {
	case ProviderAnthropic:
		text, err = c.anthropic(ctx, user)
	case ProviderGemini:
		text, err = c.gemini(ctx, user)
	default:
		text, err = c.openai(ctx, user)
	}
	if err != nil {
		return nil, err
	}
	out := &Response{Raw: text, Model: c.cfg.Model, Provider: string(c.cfg.Provider)}
	if err := json.Unmarshal([]byte(extractJSON(text)), out); err != nil {
		out.Summary = "The model did not return usable JSON. Its reply is shown below."
	}
	out.Raw = text
	out.Model = c.cfg.Model
	out.Provider = string(c.cfg.Provider)
	return out, nil
}

// extractJSON pulls the first balanced JSON object out of a reply that may be
// wrapped in a code fence or prose.
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return "{}"
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(s); i++ {
		ch := s[i]
		switch {
		case esc:
			esc = false
		case ch == '\\' && inStr:
			esc = true
		case ch == '"':
			inStr = !inStr
		case inStr:
		case ch == '{':
			depth++
		case ch == '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return "{}"
}

func (c *Client) post(ctx context.Context, url string, headers map[string]string, body any) ([]byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s returned %d: %s", c.cfg.Provider, resp.StatusCode, truncate(string(data), 400))
	}
	return data, nil
}

func (c *Client) anthropic(ctx context.Context, user string) (string, error) {
	data, err := c.post(ctx, c.cfg.BaseURL+"/v1/messages", map[string]string{
		"x-api-key":         c.cfg.APIKey,
		"anthropic-version": "2023-06-01",
	}, map[string]any{
		"model":      c.cfg.Model,
		"max_tokens": 4096,
		"system":     systemPrompt,
		"messages":   []any{map[string]any{"role": "user", "content": user}},
	})
	if err != nil {
		return "", err
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String(), nil
}

func (c *Client) gemini(ctx context.Context, user string) (string, error) {
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", c.cfg.BaseURL, c.cfg.Model, c.cfg.APIKey)
	data, err := c.post(ctx, url, nil, map[string]any{
		"systemInstruction": map[string]any{"parts": []any{map[string]string{"text": systemPrompt}}},
		"contents":          []any{map[string]any{"role": "user", "parts": []any{map[string]string{"text": user}}}},
		"generationConfig":  map[string]any{"responseMimeType": "application/json"},
	})
	if err != nil {
		return "", err
	}
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, c := range out.Candidates {
		for _, p := range c.Content.Parts {
			sb.WriteString(p.Text)
		}
	}
	return sb.String(), nil
}

func (c *Client) openai(ctx context.Context, user string) (string, error) {
	data, err := c.post(ctx, c.cfg.BaseURL+"/v1/chat/completions", map[string]string{
		"Authorization": "Bearer " + c.cfg.APIKey,
	}, map[string]any{
		"model": c.cfg.Model,
		"messages": []any{
			map[string]string{"role": "system", "content": systemPrompt},
			map[string]string{"role": "user", "content": user},
		},
		"response_format": map[string]string{"type": "json_object"},
	})
	if err != nil {
		return "", err
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", errors.New("model returned no choices")
	}
	return out.Choices[0].Message.Content, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
