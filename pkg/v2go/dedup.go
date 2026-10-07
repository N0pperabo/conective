package v2go

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"freenode/pkg/models"
)

// GenerateIdentity builds a normalized, deterministic identity hash for a config
func GenerateIdentity(c *models.Config) string {
	server := strings.ToLower(strings.TrimSpace(c.Server))
	proto := strings.ToLower(strings.TrimSpace(c.Protocol))
	transport := strings.ToLower(strings.TrimSpace(c.Transport))
	tls := strings.ToLower(strings.TrimSpace(c.TLS))
	sni := strings.ToLower(strings.TrimSpace(c.SNI))
	host := strings.ToLower(strings.TrimSpace(c.Host))
	path := strings.TrimSpace(c.Path)
	auth := strings.TrimSpace(c.UUID)
	if auth == "" {
		auth = strings.TrimSpace(c.Password)
	}
	reality := strings.TrimSpace(c.Reality)

	// Combine all routing and authentication identity fields
	key := fmt.Sprintf("%s|%s|%d|%s|%s|%s|%s|%s|%s|%s",
		proto, server, c.Port, auth, transport, tls, sni, host, path, reality)

	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:16]) // 32 hex chars
}

// Deduplicator manages seen identities safely
type Deduplicator struct {
	seen map[string]bool
}

func NewDeduplicator() *Deduplicator {
	return &Deduplicator{
		seen: make(map[string]bool),
	}
}

func (d *Deduplicator) IsDuplicate(c *models.Config) bool {
	if c.Identity == "" {
		c.Identity = GenerateIdentity(c)
	}
	if d.seen[c.Identity] {
		return true
	}
	d.seen[c.Identity] = true
	return false
}

func (d *Deduplicator) SeenCount() int {
	return len(d.seen)
}
