// Package apikeys issues bearer tokens for machine clients — CI pipelines,
// scripts, an external agent — so automation does not have to hold a human's
// OIDC credentials.
//
// Only a SHA-256 hash of each token is retained. The plaintext is returned
// exactly once, at creation; there is no endpoint that can read it back. That
// is inconvenient by design.
package apikeys

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
)

// Key is an issued token's metadata (never its plaintext).
type Key struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Role      string    `json:"role"` // viewer | editor | admin
	Prefix    string    `json:"prefix"`
	Created   time.Time `json:"created"`
	LastUsed  time.Time `json:"lastUsed,omitempty"`
	Uses      int64     `json:"uses"`
	CreatedBy string    `json:"createdBy,omitempty"`
	Static    bool      `json:"static,omitempty"`
	// Expires is zero for a non-expiring key.
	Expires time.Time `json:"expires,omitempty"`
}

// Store holds issued keys.
type Store struct {
	mu   sync.RWMutex
	keys map[string]*entry // hash -> entry
}

type entry struct {
	meta Key
	hash string
}

// New returns an empty store.
func New() *Store { return &Store{keys: map[string]*entry{}} }

func hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// LoadStatic registers tokens from configuration, formatted
// "name:role:token" (role optional, default viewer), comma-separated.
func (s *Store) LoadStatic(spec string) int {
	n := 0
	for _, item := range strings.Split(spec, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.Split(item, ":")
		var name, role, token string
		switch len(parts) {
		case 2:
			name, role, token = parts[0], "viewer", parts[1]
		case 3:
			name, role, token = parts[0], parts[1], parts[2]
		default:
			continue
		}
		h := hash(token)
		s.mu.Lock()
		s.keys[h] = &entry{hash: h, meta: Key{
			ID: h[:12], Name: name, Role: role, Prefix: prefixOf(token),
			Created: time.Now().UTC(), Static: true,
		}}
		s.mu.Unlock()
		n++
	}
	return n
}

func prefixOf(token string) string {
	if len(token) <= 8 {
		return token
	}
	return token[:8]
}

// Create issues a new token and returns the metadata plus the plaintext.
func (s *Store) Create(name, role, createdBy string, ttl time.Duration) (Key, string, error) {
	if strings.TrimSpace(name) == "" {
		return Key{}, "", errors.New("name is required")
	}
	switch role {
	case "viewer", "editor", "admin":
	default:
		return Key{}, "", errors.New("role must be viewer, editor or admin")
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return Key{}, "", err
	}
	token := "ic_" + hex.EncodeToString(buf)
	h := hash(token)
	k := Key{
		ID: h[:12], Name: name, Role: role, Prefix: prefixOf(token),
		Created: time.Now().UTC(), CreatedBy: createdBy,
	}
	if ttl > 0 {
		k.Expires = k.Created.Add(ttl)
	}
	s.mu.Lock()
	s.keys[h] = &entry{hash: h, meta: k}
	s.mu.Unlock()
	return k, token, nil
}

// List returns issued keys, newest first.
func (s *Store) List() []Key {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Key, 0, len(s.keys))
	for _, e := range s.keys {
		out = append(out, e.meta)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Created.After(out[i].Created) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// Revoke deletes a key by ID. Returns the revoked key's name.
func (s *Store) Revoke(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for h, e := range s.keys {
		if e.meta.ID == id {
			if e.meta.Static {
				return "", errors.New("static tokens are set in configuration and cannot be revoked here")
			}
			delete(s.keys, h)
			return e.meta.Name, nil
		}
	}
	return "", errors.New("no such key")
}

// Lookup resolves a plaintext token to its metadata, recording the use.
func (s *Store) Lookup(token string) (Key, bool) {
	if !strings.HasPrefix(token, "ic_") && len(token) < 8 {
		return Key{}, false
	}
	h := hash(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.keys {
		// Constant-time compare even though the map lookup already leaked
		// nothing useful: the habit is cheaper than the audit conversation.
		if subtle.ConstantTimeCompare([]byte(e.hash), []byte(h)) == 1 {
			if !e.meta.Expires.IsZero() && time.Now().After(e.meta.Expires) {
				return Key{}, false
			}
			e.meta.Uses++
			e.meta.LastUsed = time.Now().UTC()
			return e.meta, true
		}
	}
	return Key{}, false
}
