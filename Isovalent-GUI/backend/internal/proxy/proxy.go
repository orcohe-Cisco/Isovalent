// Package proxy reverse-proxies the consoles we embed rather than reimplement.
//
// The service map here is the real Hubble UI, and the dashboards are the real
// Grafana. Rebuilding either would produce a worse version of a tool the user
// already knows, and would drift the moment upstream changed. Proxying them
// through the backend (rather than iframing them directly) means one origin,
// no CORS, and one place to strip the frame-blocking headers both ship by
// default.
package proxy

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// hubbleUIAssetRef matches a root-relative src="/..." or href="/..." attribute
// value. The leading "[^\"/]" after the slash is what excludes protocol-
// relative references ("//host/...") and the bare root ("/") itself — both
// legitimate and neither in need of rewriting.
var hubbleUIAssetRef = regexp.MustCompile(`(src|href)="(/[^"/][^"]*)"`)

// Target is one proxied upstream.
type Target struct {
	// Name is the id used in the API ("hubble-ui", "grafana").
	Name string
	// Prefix is the path the console mounts it under, e.g. "/hubble-ui".
	Prefix string
	// URL is the upstream base URL.
	URL string

	parsed *url.URL
	rp     *httputil.ReverseProxy
}

// New builds a proxy handler for an upstream. A malformed URL is reported at
// construction so the server can log it once rather than on every request.
func New(name, prefix, rawURL string) (*Target, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("%s: %q is not a usable URL", name, rawURL)
	}
	t := &Target{Name: name, Prefix: strings.TrimSuffix(prefix, "/"), URL: rawURL, parsed: u}
	t.rp = &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(u)
			r.Out.Host = u.Host
			// Upstreams that build absolute links (Grafana does) need to know
			// they are being served from a sub-path.
			r.Out.Header.Set("X-Forwarded-Prefix", t.Prefix)
			r.SetXForwarded()
			// Grafana's anonymous-auth path still honours these when the
			// deployment enables auth.proxy, which is how the run script sets
			// it up for a read-only embed.
			if t.Name == "grafana" {
				r.Out.Header.Set("X-WEBAUTH-USER", "isovalent-control")
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			// Both upstreams ship frame-blocking headers by default. We are
			// deliberately framing them from the same origin, so drop them.
			resp.Header.Del("X-Frame-Options")
			if csp := resp.Header.Get("Content-Security-Policy"); csp != "" {
				resp.Header.Set("Content-Security-Policy", stripFrameAncestors(csp))
			}
			resp.Header.Del("Content-Security-Policy-Report-Only")

			// Hubble UI's built HTML references its own JS/CSS bundle with a
			// root-relative path (e.g. src="/bundle.main.<hash>.js") baked in
			// at build time. It ships no config knob to make that sub-path
			// aware (unlike Grafana's root_url), so once mounted under
			// t.Prefix those requests land on the console's own bare root
			// and 404 — the HTML shell loads, its JS never does, and the
			// page renders blank. Rewrite those references here, the one
			// place both the real mount prefix and the upstream's actual
			// response are both in hand.
			if t.Name == "hubble-ui" && strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
				if err := rewriteRootRelativeRefs(resp, t.Prefix); err != nil {
					return err
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(w, `<!doctype html><html><body style="font:14px system-ui;background:#111;color:#ddd;padding:2rem">
<h2 style="margin:0 0 .5rem">%s is not reachable</h2>
<p style="color:#999">The console tried <code>%s</code> and got: %s</p>
<p style="color:#999">Set <code>IC_%s_URL</code> to the right address, or port-forward it.
The <code>Diagnostics</code> page checks this for you.</p></body></html>`,
				t.Name, rawURL, err, strings.ToUpper(strings.ReplaceAll(t.Name, "-", "_")))
		},
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			ResponseHeaderTimeout: 30 * time.Second,
			// The Hubble UI backend streams; do not buffer it into oblivion.
			ForceAttemptHTTP2: true,
		},
		FlushInterval: 200 * time.Millisecond,
	}
	return t, nil
}

// ServeHTTP proxies one request, stripping the mount prefix.
func (t *Target) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.URL.Path = strings.TrimPrefix(r.URL.Path, t.Prefix)
	if r.URL.Path == "" {
		r.URL.Path = "/"
	}
	t.rp.ServeHTTP(w, r)
}

// Probe reports whether the upstream answers, and how fast.
func (t *Target) Probe(ctx context.Context) (ok bool, latency time.Duration, detail string) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		return false, 0, err.Error()
	}
	// Do not follow redirects: the question this asks is "does the upstream
	// answer at all", and the Location it redirects to is frequently a URL
	// that only makes sense from wherever the upstream's own config thinks
	// it is being accessed from — e.g. Grafana's root_url — which is not
	// necessarily reachable from *this* pod even when the upstream is
	// perfectly healthy. Judging on the redirect response itself, rather
	// than chasing it, is what makes the comment below actually true.
	client := &http.Client{
		Timeout:       4 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, time.Since(start), err.Error()
	}
	defer resp.Body.Close()
	// Anything that answers HTTP is reachable; 302 to a login page still means
	// the service is up, which is the question being asked.
	return resp.StatusCode < 500, time.Since(start), fmt.Sprintf("HTTP %d", resp.StatusCode)
}

func stripFrameAncestors(csp string) string {
	parts := strings.Split(csp, ";")
	kept := parts[:0]
	for _, p := range parts {
		if strings.HasPrefix(strings.TrimSpace(strings.ToLower(p)), "frame-ancestors") {
			continue
		}
		kept = append(kept, p)
	}
	return strings.Join(kept, ";")
}

// rewriteRootRelativeRefs prefixes root-relative src="/..." / href="/..."
// attribute values in an HTML response body with prefix, so the browser
// requests them from where the console actually serves them instead of its
// bare root. It transparently handles a gzip-encoded body, since that's how
// most upstreams (including Hubble UI's) serve HTML by default.
func rewriteRootRelativeRefs(resp *http.Response, prefix string) error {
	defer resp.Body.Close()

	var reader io.Reader = resp.Body
	gzipped := strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip")
	if gzipped {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			// A response that claims gzip but isn't isn't something we can
			// safely patch; leave it exactly as it arrived.
			return nil
		}
		defer gz.Close()
		reader = gz
	}

	body, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	rewritten := hubbleUIAssetRef.ReplaceAllFunc(body, func(m []byte) []byte {
		sub := hubbleUIAssetRef.FindSubmatch(m)
		attr, ref := string(sub[1]), string(sub[2])
		if ref == prefix || strings.HasPrefix(ref, prefix+"/") {
			return m // already scoped under our prefix; don't double it up
		}
		return []byte(attr + `="` + prefix + ref + `"`)
	})

	resp.Body = io.NopCloser(bytes.NewReader(rewritten))
	resp.Header.Del("Content-Encoding")
	resp.Header.Set("Content-Length", strconv.Itoa(len(rewritten)))
	resp.ContentLength = int64(len(rewritten))
	return nil
}
