package subscription

import (
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"freenode/pkg/cleanip"
	"freenode/pkg/database"
	"freenode/pkg/models"
	"freenode/pkg/v2go"
)

type Manager struct {
	db *database.DB
}

func NewManager(db *database.DB) *Manager {
	return &Manager{db: db}
}

// GenerateSubscription creates a subscription content from database working nodes
func (m *Manager) GenerateSubscription(filter database.ConfigFilter, format string) (string, error) {
	filter.Status = "working"
	if filter.Limit <= 0 {
		filter.Limit = 500
	}

	configs, _, err := m.db.GetConfigs(filter)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for _, c := range configs {
		if c.RawLink != "" {
			sb.WriteString(c.RawLink)
			sb.WriteString("\n")
		}
	}

	plain := sb.String()
	if strings.ToLower(format) == "base64" {
		return base64.StdEncoding.EncodeToString([]byte(plain)), nil
	}
	return plain, nil
}

// parseCleanIPLine tests if a line represents an IPv4 address, optional port, and optional name fragment
func parseCleanIPLine(l string) (cleanIP string, port int, name string, ok bool) {
	l = strings.TrimSpace(l)
	l = strings.Trim(l, "\"'`")
	if l == "" || strings.HasPrefix(l, "//") {
		return "", 0, "", false
	}

	// If it has a URL scheme other than psiphon://, it's not a Clean IP
	if strings.Contains(l, "://") && !strings.HasPrefix(strings.ToLower(l), "psiphon://") {
		return "", 0, "", false
	}

	// Handle psiphon:// scheme
	if strings.HasPrefix(strings.ToLower(l), "psiphon://") {
		raw := l[len("psiphon://"):]
		frag := ""
		if idx := strings.Index(raw, "#"); idx != -1 {
			frag = strings.TrimSpace(raw[idx+1:])
			raw = strings.TrimSpace(raw[:idx])
		}
		if unescaped, err := url.QueryUnescape(frag); err == nil && unescaped != "" {
			frag = unescaped
		}
		if idx := strings.Index(raw, "?"); idx != -1 {
			raw = strings.TrimSpace(raw[:idx])
		}
		if idx := strings.Index(raw, "/"); idx != -1 {
			raw = strings.TrimSpace(raw[:idx])
		}
		if idx := strings.LastIndex(raw, "@"); idx != -1 {
			raw = strings.TrimSpace(raw[idx+1:])
		}

		p := 443
		host := raw
		if strings.Contains(raw, ":") {
			h, pStr, err := net.SplitHostPort(raw)
			if err != nil {
				return "", 0, "", false
			}
			host = h
			parsedPort, err := strconv.Atoi(pStr)
			if err != nil || parsedPort <= 0 || parsedPort > 65535 {
				return "", 0, "", false
			}
			p = parsedPort
		}

		netIP := net.ParseIP(host)
		if netIP != nil && netIP.To4() != nil {
			return host, p, frag, true
		}
		return "", 0, "", false
	}

	// Extract # name tag
	customName := ""
	if idx := strings.Index(l, "#"); idx != -1 {
		customName = strings.TrimSpace(l[idx+1:])
		if unescaped, err := url.QueryUnescape(customName); err == nil && unescaped != "" {
			customName = unescaped
		}
		l = strings.TrimSpace(l[:idx])
	}

	// Extract port if formatted as IP:PORT
	port = 443
	host := l
	if strings.Contains(l, ":") {
		h, pStr, err := net.SplitHostPort(l)
		if err != nil {
			return "", 0, "", false
		}
		host = h
		p, err := strconv.Atoi(pStr)
		if err != nil || p <= 0 || p > 65535 {
			return "", 0, "", false
		}
		port = p
	}

	// Verify valid IPv4
	netIP := net.ParseIP(host)
	if netIP != nil && netIP.To4() != nil {
		return host, port, customName, true
	}
	return "", 0, "", false
}

// ImportContent parses content (plain lines, base64, or CDN Clean IPs) and saves new nodes into database
func (m *Manager) ImportContent(content string, sourceName string) (int, int, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return 0, 0, fmt.Errorf("content is empty")
	}

	// Try base64 decoding whole content
	if !strings.Contains(content, "://") && len(content) > 10 {
		cleaned := strings.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, content)
		cleaned = strings.ReplaceAll(cleaned, "-", "+")
		cleaned = strings.ReplaceAll(cleaned, "_", "/")
		if rem := len(cleaned) % 4; rem > 0 {
			cleaned += strings.Repeat("=", 4-rem)
		}
		if decoded, err := base64.StdEncoding.DecodeString(cleaned); err == nil && len(decoded) > 0 {
			content = string(decoded)
		}
	}

	rawLines := strings.Split(content, "\n")
	var candidateLinks []string
	var cleanIPNodes []*models.Config
	schemes := []string{"vless://", "vmess://", "trojan://", "ss://", "hysteria2://", "tuic://", "ssr://"}

	for _, l := range rawLines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}

		// First, check if line is a Clean IP or psiphon format
		if ip, port, name, ok := parseCleanIPLine(l); ok {
			cleanNode, err := cleanip.CreateOrUpdateCleanIPNodeWithName(m.db, ip, 120, name)
			if err == nil && cleanNode != nil {
				if port > 0 && port != cleanNode.Port {
					cleanNode.Port = port
					cleanNode.RawLink = fmt.Sprintf("psiphon://%s:%d?fronting=cdn#Clean-IP-%s", ip, port, ip)
					if m.db != nil {
						_, _ = m.db.UpsertConfig(cleanNode)
					}
				}
				cleanIPNodes = append(cleanIPNodes, cleanNode)
			}
			continue
		}

		// Check if the line itself is a base64 chunk
		if !strings.Contains(l, "://") && len(l) > 20 {
			if dec, err := base64.StdEncoding.DecodeString(l); err == nil && (strings.Contains(string(dec), "://") || strings.Contains(string(dec), ".")) {
				for _, subL := range strings.Split(string(dec), "\n") {
					subL = strings.TrimSpace(subL)
					if subL != "" {
						if ip, port, name, ok := parseCleanIPLine(subL); ok {
							cleanNode, err := cleanip.CreateOrUpdateCleanIPNodeWithName(m.db, ip, 120, name)
							if err == nil && cleanNode != nil {
								if port > 0 && port != cleanNode.Port {
									cleanNode.Port = port
									cleanNode.RawLink = fmt.Sprintf("psiphon://%s:%d?fronting=cdn#Clean-IP-%s", ip, port, ip)
									if m.db != nil {
										_, _ = m.db.UpsertConfig(cleanNode)
									}
								}
								cleanIPNodes = append(cleanIPNodes, cleanNode)
							}
						} else {
							candidateLinks = append(candidateLinks, subL)
						}
					}
				}
				continue
			}
		}

		// Find any valid scheme on the line
		found := false
		for _, s := range schemes {
			if idx := strings.Index(strings.ToLower(l), s); idx != -1 {
				link := strings.TrimSpace(l[idx:])
				candidateLinks = append(candidateLinks, link)
				found = true
				break
			}
		}
		if !found && strings.Contains(l, "://") {
			candidateLinks = append(candidateLinks, l)
		}
	}

	if len(candidateLinks) == 0 && len(cleanIPNodes) == 0 {
		return 0, 0, fmt.Errorf("no valid proxy links or Clean IPs found (supported: vless, vmess, trojan, ss, hysteria2, or CDN IP addresses)")
	}

	dedup := v2go.NewDeduplicator()
	var configsToInsert []*models.Config
	duplicates := 0

	for _, node := range cleanIPNodes {
		if dedup.IsDuplicate(node) {
			duplicates++
			continue
		}
		configsToInsert = append(configsToInsert, node)
	}

	for _, rawLink := range candidateLinks {
		cfg, err := v2go.ParseLink(rawLink, sourceName)
		if err != nil {
			continue
		}

		if dedup.IsDuplicate(cfg) {
			duplicates++
			continue
		}

		configsToInsert = append(configsToInsert, cfg)
	}

	if len(configsToInsert) == 0 && duplicates == 0 {
		return 0, 0, fmt.Errorf("failed to parse configurations")
	}

	if len(configsToInsert) > 0 && m.db != nil {
		err := m.db.UpsertBatch(configsToInsert)
		if err != nil {
			return 0, duplicates, err
		}
	}

	return len(configsToInsert), duplicates, nil
}

// ImportURL fetches from a remote subscription URL and imports all found nodes
func (m *Manager) ImportURL(urlStr, sourceName string) (int, int, error) {
	urlStr = v2go.NormalizeURL(urlStr)
	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("User-Agent", "v2rayN/6.39 (Windows NT 10.0; Win64; x64) v2go/1.3")

	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("HTTP error: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, err
	}

	return m.ImportContent(string(body), sourceName)
}
