// Package gitops implements the "open a PR instead of applying" path: it
// renders a policy manifest to a file on a new branch and opens a pull request
// via the GitHub API (ArgoCD/Flux then reconcile it into the cluster).
package gitops

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Client talks to the GitHub REST API.
type Client struct {
	Repo   string // owner/repo
	Token  string
	Base   string
	Path   string
	APIURL string
	http   *http.Client
}

// New returns a client (Enabled() is false unless Repo and Token are set).
func New(repo, token, base, path, apiURL string) *Client {
	if base == "" {
		base = "main"
	}
	if apiURL == "" {
		apiURL = "https://api.github.com"
	}
	return &Client{Repo: repo, Token: token, Base: base, Path: strings.Trim(path, "/"), APIURL: strings.TrimRight(apiURL, "/"),
		http: &http.Client{Timeout: 20 * time.Second}}
}

// Enabled reports whether GitOps PR mode is configured.
func (c *Client) Enabled() bool { return c.Repo != "" && c.Token != "" }

var slug = regexp.MustCompile(`[^a-z0-9-]+`)

// OpenPR creates a branch, commits filename with content, and opens a PR.
// Returns the PR HTML URL.
func (c *Client) OpenPR(ctx context.Context, filename, content, title, body string) (string, error) {
	if !c.Enabled() {
		return "", fmt.Errorf("gitops not configured (set IC_GITHUB_REPO and IC_GITHUB_TOKEN)")
	}
	branch := fmt.Sprintf("isovalent-control/%s-%d", slug.ReplaceAllString(strings.ToLower(title), "-"), time.Now().Unix())
	path := filename
	if c.Path != "" {
		path = c.Path + "/" + filename
	}

	baseSHA, err := c.refSHA(ctx, "heads/"+c.Base)
	if err != nil {
		return "", fmt.Errorf("read base branch %q: %w", c.Base, err)
	}
	if err := c.createRef(ctx, "refs/heads/"+branch, baseSHA); err != nil {
		return "", fmt.Errorf("create branch: %w", err)
	}
	existingSHA, _ := c.fileSHA(ctx, path, branch) // empty if new file
	if err := c.putFile(ctx, path, branch, content, "isovalent-control: "+title, existingSHA); err != nil {
		return "", fmt.Errorf("commit file: %w", err)
	}
	prURL, err := c.createPR(ctx, title, branch, body)
	if err != nil {
		return "", fmt.Errorf("open PR: %w", err)
	}
	return prURL, nil
}

func (c *Client) do(ctx context.Context, method, path string, in any, out any) (int, error) {
	var rdr io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.APIURL+path, rdr)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("github %s %s: %d %s", method, path, resp.StatusCode, string(data))
	}
	if out != nil && len(data) > 0 {
		return resp.StatusCode, json.Unmarshal(data, out)
	}
	return resp.StatusCode, nil
}

func (c *Client) refSHA(ctx context.Context, ref string) (string, error) {
	var out struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if _, err := c.do(ctx, http.MethodGet, "/repos/"+c.Repo+"/git/ref/"+ref, nil, &out); err != nil {
		return "", err
	}
	return out.Object.SHA, nil
}

func (c *Client) createRef(ctx context.Context, ref, sha string) error {
	_, err := c.do(ctx, http.MethodPost, "/repos/"+c.Repo+"/git/refs",
		map[string]string{"ref": ref, "sha": sha}, nil)
	return err
}

func (c *Client) fileSHA(ctx context.Context, path, branch string) (string, error) {
	var out struct {
		SHA string `json:"sha"`
	}
	if _, err := c.do(ctx, http.MethodGet, "/repos/"+c.Repo+"/contents/"+path+"?ref="+branch, nil, &out); err != nil {
		return "", err
	}
	return out.SHA, nil
}

func (c *Client) putFile(ctx context.Context, path, branch, content, message, sha string) error {
	body := map[string]string{
		"message": message,
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
		"branch":  branch,
	}
	if sha != "" {
		body["sha"] = sha
	}
	_, err := c.do(ctx, http.MethodPut, "/repos/"+c.Repo+"/contents/"+path, body, nil)
	return err
}

func (c *Client) createPR(ctx context.Context, title, head, body string) (string, error) {
	var out struct {
		HTMLURL string `json:"html_url"`
	}
	_, err := c.do(ctx, http.MethodPost, "/repos/"+c.Repo+"/pulls",
		map[string]string{"title": "isovalent-control: " + title, "head": head, "base": c.Base, "body": body}, &out)
	return out.HTMLURL, err
}
