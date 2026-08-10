// Package auth implements OIDC JWT verification and namespace-scoped RBAC.
//
// Role assignments arrive in a configurable claim (default "groups") as
// strings of the form:
//
//	ic:viewer                 read-only, all namespaces
//	ic:editor:shop,web        policy edit rights in namespaces shop and web
//	ic:editor:team-*          globs are supported
//	ic:admin                  full control incl. cluster-wide policies
//
// With no OIDC issuer configured the server runs in dev mode and injects a
// synthetic admin identity.
package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// RoleName is an ordered permission level.
type RoleName string

const (
	RoleViewer RoleName = "viewer"
	RoleEditor RoleName = "editor"
	RoleAdmin  RoleName = "admin"
)

// Role is a permission level bound to a set of namespace globs.
type Role struct {
	Name       RoleName `json:"name"`
	Namespaces []string `json:"namespaces,omitempty"` // empty = all
}

// Identity is the authenticated caller.
type Identity struct {
	Subject string `json:"subject"`
	Email   string `json:"email,omitempty"`
	Name    string `json:"name,omitempty"`
	Roles   []Role `json:"roles"`
	Dev     bool   `json:"dev,omitempty"`
}

// DevAdmin is the identity used when authentication is disabled.
func DevAdmin() *Identity {
	return &Identity{Subject: "dev-admin", Name: "Development Admin", Roles: []Role{{Name: RoleAdmin}}, Dev: true}
}

func matches(globs []string, namespace string) bool {
	if len(globs) == 0 {
		return true
	}
	for _, g := range globs {
		if ok, _ := path.Match(g, namespace); ok {
			return true
		}
	}
	return false
}

// CanRead reports read access to a namespace ("" = cluster scope).
func (id *Identity) CanRead(namespace string) bool {
	for _, r := range id.Roles {
		if matches(r.Namespaces, namespace) {
			return true
		}
	}
	return false
}

// CanEdit reports policy-write access to a namespace.
func (id *Identity) CanEdit(namespace string) bool {
	for _, r := range id.Roles {
		if (r.Name == RoleEditor || r.Name == RoleAdmin) && matches(r.Namespaces, namespace) {
			return true
		}
	}
	return false
}

// IsAdmin reports cluster-wide administrative access.
func (id *Identity) IsAdmin() bool {
	for _, r := range id.Roles {
		if r.Name == RoleAdmin && len(r.Namespaces) == 0 {
			return true
		}
	}
	return false
}

// ParseRoleClaim converts one claim string into a Role, if it is ours.
func ParseRoleClaim(s string) (Role, bool) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) < 2 || parts[0] != "ic" {
		return Role{}, false
	}
	name := RoleName(parts[1])
	if name != RoleViewer && name != RoleEditor && name != RoleAdmin {
		return Role{}, false
	}
	role := Role{Name: name}
	if len(parts) == 3 && parts[2] != "" {
		role.Namespaces = strings.Split(parts[2], ",")
	}
	return role, true
}

type ctxKey struct{}

// FromContext returns the request identity (always set behind Middleware).
func FromContext(ctx context.Context) *Identity {
	if id, ok := ctx.Value(ctxKey{}).(*Identity); ok {
		return id
	}
	return &Identity{Subject: "anonymous"}
}

// Verifier validates OIDC JWTs against the issuer's JWKS.
type Verifier struct {
	Issuer     string
	ClientID   string
	RolesClaim string

	mu      sync.Mutex
	keys    map[string]any // kid -> public key
	fetched time.Time
}

// Middleware returns an authentication middleware. A nil receiver (auth
// disabled) injects the dev admin identity.
func (v *Verifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v == nil {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, DevAdmin())))
			return
		}
		raw := bearerToken(r)
		if raw == "" {
			httpError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		id, err := v.Verify(raw)
		if err != nil {
			httpError(w, http.StatusUnauthorized, "invalid token: "+err.Error())
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, id)))
	})
}

func bearerToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	// WebSocket clients cannot set headers from the browser.
	return r.URL.Query().Get("access_token")
}

// Verify parses and validates a JWT, returning the mapped identity.
func (v *Verifier) Verify(raw string) (*Identity, error) {
	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, v.keyFunc,
		jwt.WithValidMethods([]string{"RS256", "RS384", "RS512", "ES256", "ES384"}),
		jwt.WithIssuer(v.Issuer),
		jwt.WithAudience(v.ClientID),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}
	id := &Identity{}
	id.Subject, _ = claims["sub"].(string)
	id.Email, _ = claims["email"].(string)
	id.Name, _ = claims["name"].(string)
	if vals, ok := claims[v.RolesClaim].([]any); ok {
		for _, g := range vals {
			if s, ok := g.(string); ok {
				if role, ok := ParseRoleClaim(s); ok {
					id.Roles = append(id.Roles, role)
				}
			}
		}
	}
	if len(id.Roles) == 0 {
		return nil, errors.New("token carries no ic:* role claims")
	}
	return id, nil
}

func (v *Verifier) keyFunc(t *jwt.Token) (any, error) {
	kid, _ := t.Header["kid"].(string)
	v.mu.Lock()
	defer v.mu.Unlock()
	if key, ok := v.keys[kid]; ok && time.Since(v.fetched) < 10*time.Minute {
		return key, nil
	}
	if err := v.refreshLocked(); err != nil {
		return nil, err
	}
	if key, ok := v.keys[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("unknown key id %q", kid)
}

func (v *Verifier) refreshLocked() error {
	var disc struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := getJSON(strings.TrimSuffix(v.Issuer, "/")+"/.well-known/openid-configuration", &disc); err != nil {
		return fmt.Errorf("oidc discovery: %w", err)
	}
	var jwks struct {
		Keys []struct {
			Kty, Kid, N, E, Crv, X, Y string
		} `json:"keys"`
	}
	if err := getJSON(disc.JWKSURI, &jwks); err != nil {
		return fmt.Errorf("jwks fetch: %w", err)
	}
	keys := map[string]any{}
	for _, k := range jwks.Keys {
		switch k.Kty {
		case "RSA":
			n, err1 := base64.RawURLEncoding.DecodeString(k.N)
			e, err2 := base64.RawURLEncoding.DecodeString(k.E)
			if err1 != nil || err2 != nil {
				continue
			}
			keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
		case "EC":
			x, err1 := base64.RawURLEncoding.DecodeString(k.X)
			y, err2 := base64.RawURLEncoding.DecodeString(k.Y)
			if err1 != nil || err2 != nil {
				continue
			}
			var curve elliptic.Curve
			switch k.Crv {
			case "P-256":
				curve = elliptic.P256()
			case "P-384":
				curve = elliptic.P384()
			default:
				continue
			}
			keys[k.Kid] = &ecdsa.PublicKey{Curve: curve, X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		}
	}
	v.keys = keys
	v.fetched = time.Now()
	return nil
}

func getJSON(url string, out any) error {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func httpError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
