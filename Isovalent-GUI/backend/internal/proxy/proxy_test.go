package proxy

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// This exercises the exact failure the HAR export showed for the Service
// Map: Hubble UI's built HTML references its own bundle with a root-
// relative path, which 404s once the app is mounted under a sub-path.
func TestRewriteRootRelativeRefs(t *testing.T) {
	const html = `<!doctype html><html><head>` +
		`<link href="/bundle.main.eae50800ddcd18c25e9e.css" rel="stylesheet">` +
		`</head><body>` +
		`<script src="/bundle.main.1d051ccbd0f5cd57832e.js"></script>` +
		`<img src="//other-host.example.com/logo.png">` + // protocol-relative: leave alone
		`<a href="/hubble-ui/already-prefixed">already scoped</a>` + // already under prefix: leave alone
		`<a href="/">root</a>` + // bare root: leave alone
		`</body></html>`

	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:   io.NopCloser(strings.NewReader(html)),
	}

	if err := rewriteRootRelativeRefs(resp, "/hubble-ui"); err != nil {
		t.Fatalf("rewriteRootRelativeRefs: %v", err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read rewritten body: %v", err)
	}
	body := string(got)

	for _, want := range []string{
		`href="/hubble-ui/bundle.main.eae50800ddcd18c25e9e.css"`,
		`src="/hubble-ui/bundle.main.1d051ccbd0f5cd57832e.js"`,
		`src="//other-host.example.com/logo.png"`,
		`href="/hubble-ui/already-prefixed"`,
		`href="/"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rewritten body missing %q\ngot: %s", want, body)
		}
	}
	if strings.Contains(body, `/hubble-ui/hubble-ui/`) {
		t.Errorf("prefix was applied twice: %s", body)
	}

	if cl := resp.Header.Get("Content-Length"); cl != "" {
		if want := strconv.Itoa(len(got)); cl != want {
			// Content-Length must match the rewritten body length, not the
			// original — a stale value here would truncate the response the
			// browser actually receives.
			t.Errorf("Content-Length = %s, want %s", cl, want)
		}
	}
}

func TestRewriteRootRelativeRefsGzip(t *testing.T) {
	const html = `<script src="/bundle.main.abc123.js"></script>`

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte(html)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	resp := &http.Response{
		Header: http.Header{
			"Content-Type":     []string{"text/html"},
			"Content-Encoding": []string{"gzip"},
		},
		Body: io.NopCloser(bytes.NewReader(buf.Bytes())),
	}

	if err := rewriteRootRelativeRefs(resp, "/hubble-ui"); err != nil {
		t.Fatalf("rewriteRootRelativeRefs: %v", err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read rewritten body: %v", err)
	}
	if !strings.Contains(string(got), `src="/hubble-ui/bundle.main.abc123.js"`) {
		t.Errorf("gzip body not rewritten: %s", got)
	}
	if enc := resp.Header.Get("Content-Encoding"); enc != "" {
		t.Errorf("Content-Encoding should be stripped once the body is plain, got %q", enc)
	}
}

// End-to-end sanity check through ModifyResponse itself, so this fails if a
// future edit stops wiring rewriteRootRelativeRefs into New() for hubble-ui.
func TestNewRewritesHubbleUIHTMLThroughProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<script src="/bundle.main.xyz.js"></script>`))
	}))
	defer upstream.Close()

	target, err := New("hubble-ui", "/hubble-ui", upstream.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/hubble-ui/", nil)
	rec := httptest.NewRecorder()
	target.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `src="/hubble-ui/bundle.main.xyz.js"`) {
		t.Errorf("proxied response not rewritten: %s", body)
	}
}
