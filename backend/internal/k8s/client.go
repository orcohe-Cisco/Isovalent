// Package k8s talks to the Kubernetes API server with a deliberately minimal
// REST client (see docs/architecture.md): the CRDs we manage are round-tripped
// as opaque JSON manifests, so the dynamic-client machinery of client-go adds
// no value here while multiplying the module graph ~40x.
//
// Supported credential sources, in order:
//  1. Explicit IC_K8S_API_SERVER (+ optional token / CA).
//  2. In-cluster service account (KUBERNETES_SERVICE_HOST + mounted token).
//  3. kubectl proxy on http://127.0.0.1:8001 (local development).
package k8s

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	saTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token" // #nosec G101 -- well-known mount path
	saCAPath    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

// Client is a minimal Kubernetes REST client.
type Client struct {
	base      string
	tokenFile string
	token     string
	http      *http.Client
}

// Options configures NewClient.
type Options struct {
	APIServer string // e.g. https://10.96.0.1:443 or http://127.0.0.1:8001
	Token     string
	TokenFile string
	CAFile    string
	Insecure  bool
}

// NewClient builds a client from options + ambient environment.
func NewClient(opts Options) (*Client, error) {
	base := opts.APIServer
	tokenFile := opts.TokenFile
	caFile := opts.CAFile
	if base == "" {
		if host := os.Getenv("KUBERNETES_SERVICE_HOST"); host != "" {
			port := os.Getenv("KUBERNETES_SERVICE_PORT")
			if port == "" {
				port = "443"
			}
			base = "https://" + host + ":" + port
			if tokenFile == "" {
				tokenFile = saTokenPath
			}
			if caFile == "" {
				caFile = saCAPath
			}
		} else {
			base = "http://127.0.0.1:8001" // kubectl proxy
		}
	}

	transport := &http.Transport{}
	if strings.HasPrefix(base, "https://") {
		tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
		if opts.Insecure {
			tlsCfg.InsecureSkipVerify = true // #nosec G402 -- explicit opt-in for dev
		} else if caFile != "" {
			pem, err := os.ReadFile(caFile)
			if err != nil {
				return nil, fmt.Errorf("read CA file: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, fmt.Errorf("no certificates in %s", caFile)
			}
			tlsCfg.RootCAs = pool
		}
		transport.TLSClientConfig = tlsCfg
	}

	return &Client{
		base:      strings.TrimSuffix(base, "/"),
		token:     opts.Token,
		tokenFile: tokenFile,
		http:      &http.Client{Timeout: 30 * time.Second, Transport: transport},
	}, nil
}

// APIError is a non-2xx response from the API server.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("kubernetes API: %d %s", e.Status, e.Message) }

// Do performs a request and returns the raw response body.
func (c *Client) Do(ctx context.Context, method, path, contentType string, body []byte) (json.RawMessage, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	if tok := c.bearer(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := string(data)
		var status struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &status) == nil && status.Message != "" {
			msg = status.Message
		}
		return nil, &APIError{Status: resp.StatusCode, Message: msg}
	}
	return data, nil
}

func (c *Client) bearer() string {
	if c.token != "" {
		return c.token
	}
	if c.tokenFile != "" {
		if data, err := os.ReadFile(c.tokenFile); err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return ""
}
