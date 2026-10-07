package cleanip

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"freenode/pkg/database"
	"freenode/pkg/models"
)

// fallbackOrigins are last-resort Cloudflare-hosted VLESS/WS origins.
var fallbackOrigins = []struct{ uuid, host string }{
	{"8546bcef-8456-41a2-82bc-d2231b42b769", "small-tree-9ec0.84-971.workers.dev"},
}

func originRank(c *models.Config) int {
	score := 0
	h := strings.ToLower(c.SNI + " " + c.Host)
	if strings.Contains(h, "workers.dev") || strings.Contains(h, "pages.dev") {
		score += 40
	}
	switch c.Status {
	case "working":
		score += 100
	case "untested", "":
		score += 20
	}
	if c.Latency > 0 && c.Latency < 3000 {
		score += 10
	}
	return score
}

// BuildCleanIPCandidates returns up to max distinct origin nodes (one per SNI/Host)
// whose dial address is replaced by cleanIP (CDN fronting on a TLS port), best first.
// A clean IP is only useful in front of a live origin, so the caller should try
// candidates in order and keep the first one that passes a real traffic check.
func BuildCleanIPCandidates(db *database.DB, cleanIP string, latency, max int) []*models.Config {
	p := net.ParseIP(strings.TrimSpace(cleanIP))
	if p == nil || p.To4() == nil {
		return nil
	}
	cleanIP = p.String()
	if max <= 0 {
		max = 20
	}
	if latency <= 0 {
		latency = 120
	}

	var pool []models.Config
	if db != nil {
		if cfgs, _, err := db.GetConfigs(database.ConfigFilter{Limit: 3000, SortBy: "score", SortOrder: "DESC"}); err == nil {
			pool = cfgs
		}
	}

	var origins []*models.Config
	for i := range pool {
		c := &pool[i]
		if strings.HasPrefix(c.Identity, "cleanip-") {
			continue
		}
		switch c.Protocol {
		case "vless", "vmess", "trojan":
		default:
			continue
		}
		t := strings.ToLower(c.Transport)
		if t != "ws" && t != "grpc" && t != "xhttp" && t != "httpupgrade" {
			continue
		}
		if c.TLS != "tls" || (c.SNI == "" && c.Host == "") {
			continue
		}
		origins = append(origins, c)
	}
	sort.SliceStable(origins, func(i, j int) bool { return originRank(origins[i]) > originRank(origins[j]) })

	seen := map[string]bool{}
	var out []*models.Config
	add := func(n *models.Config) {
		out = append(out, n)
	}
	for _, o := range origins {
		key := strings.ToLower(o.SNI + "|" + o.Host + "|" + o.UUID + o.Password)
		if seen[key] {
			continue
		}
		seen[key] = true
		n := *o
		n.ID = 0
		n.Port = 443
		n.Server = cleanIP
		n.Identity = fmt.Sprintf("cleanip-%s-%s", cleanIP, o.Identity)
		n.Name = fmt.Sprintf("⚡ Clean IP | %s (%s)", cleanIP, o.Name)
		n.Source = "Clean IP Fronting"
		n.Latency = latency
		n.Status = "working"
		n.Score = 1000
		n.IsFavorite = true
		n.Tags = "clean-ip,cdn-fronting,cloudflare"
		n.LastTested = time.Now()
		if err := ApplyCleanIP(&n, cleanIP); err != nil {
			continue
		}
		if geo := GetDefaultGeoResolver(); geo != nil {
			g := geo.Lookup(cleanIP)
			if g.Code != "" && g.Code != "UN" {
				n.Country = g.Code
				n.CountryName = g.Name
			}
		}
		n.Port = 443
		add(&n)
		if len(out) >= max {
			return out
		}
	}

	countryCode := "CF"
	countryName := "Cloudflare Edge"
	if geo := GetDefaultGeoResolver(); geo != nil {
		g := geo.Lookup(cleanIP)
		if g.Code != "" && g.Code != "UN" {
			countryCode = g.Code
			countryName = g.Name
		}
	}

	for _, f := range fallbackOrigins {
		n := &models.Config{
			Identity:    fmt.Sprintf("cleanip-%s-edge-%s", cleanIP, f.host),
			Name:        fmt.Sprintf("⚡ Clean IP Tunnel | %s", cleanIP),
			Protocol:    "vless",
			Server:      cleanIP,
			Port:        443,
			UUID:        f.uuid,
			Transport:   "ws",
			TLS:         "tls",
			SNI:         f.host,
			Host:        f.host,
			Path:        "/",
			Country:     countryCode,
			CountryName: countryName,
			Source:      "Clean IP Fronting",
			Latency:     latency,
			Status:      "working",
			Score:       1000,
			IsFavorite:  true,
			Tags:        "clean-ip,cdn-fronting,cloudflare,worker",
			FirstSeen:   time.Now(),
			LastSeen:    time.Now(),
			LastTested:  time.Now(),
		}
		add(n)
	}
	return out
}
