package v2go

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"freenode/pkg/models"
)

type M = map[string]any

// ParseLink takes a raw proxy link and converts it to a structured models.Config
func ParseLink(raw string, sourceName string) (*models.Config, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") {
		return nil, fmt.Errorf("empty or commented line")
	}

	proto := DetectProtocol(raw)
	if proto == "unknown" {
		return nil, fmt.Errorf("unknown protocol: %s", raw)
	}

	cfg := &models.Config{
		Protocol: proto,
		RawLink:  raw,
		Source:   sourceName,
		Status:   "untested",
		Latency:  -1,
	}

	switch proto {
	case "vless":
		if err := parseVLESSDetails(raw, cfg); err != nil {
			return nil, err
		}
	case "vmess":
		if err := parseVMessDetails(raw, cfg); err != nil {
			return nil, err
		}
	case "trojan":
		if err := parseTrojanDetails(raw, cfg); err != nil {
			return nil, err
		}
	case "ss":
		if err := parseSSDetails(raw, cfg); err != nil {
			return nil, err
		}
	case "hysteria2":
		if err := parseHysteria2Details(raw, cfg); err != nil {
			return nil, err
		}
	case "tuic":
		if err := parseTUICDetails(raw, cfg); err != nil {
			return nil, err
		}
	case "ssr":
		if err := parseSSRDetails(raw, cfg); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported protocol %s", proto)
	}

	// Generate identity for smart deduplication
	cfg.Identity = GenerateIdentity(cfg)

	return cfg, nil
}

func DetectProtocol(raw string) string {
	raw = strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(raw, "vless://"):
		return "vless"
	case strings.HasPrefix(raw, "vmess://"):
		return "vmess"
	case strings.HasPrefix(raw, "trojan://"):
		return "trojan"
	case strings.HasPrefix(raw, "ss://"):
		return "ss"
	case strings.HasPrefix(raw, "hysteria2://"), strings.HasPrefix(raw, "hy2://"):
		return "hysteria2"
	case strings.HasPrefix(raw, "tuic://"):
		return "tuic"
	case strings.HasPrefix(raw, "ssr://"):
		return "ssr"
	default:
		return "unknown"
	}
}

func parseVLESSDetails(link string, c *models.Config) error {
	u, err := url.Parse(link)
	if err != nil {
		return err
	}
	q := u.Query()

	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 || port > 65535 {
		return fmt.Errorf("invalid port")
	}

	c.Server = u.Hostname()
	c.Port = port
	if u.User != nil {
		c.UUID = u.User.Username()
	}
	c.Name = u.Fragment
	if c.Name == "" {
		c.Name = fmt.Sprintf("vless-%s:%d", c.Server, c.Port)
	}

	c.Transport = q.Get("type")
	if c.Transport == "" {
		c.Transport = "tcp"
	}
	c.TLS = q.Get("security")
	if c.TLS == "" {
		c.TLS = "none"
	}
	c.SNI = q.Get("sni")
	c.Path = q.Get("path")
	c.Host = q.Get("host")
	if pbk := q.Get("pbk"); pbk != "" {
		c.Reality = pbk
		c.TLS = "reality"
	}

	return nil
}

func parseVMessDetails(link string, c *models.Config) error {
	raw := strings.TrimPrefix(link, "vmess://")
	decoded, err := decodeBase64(raw)
	if err != nil {
		return fmt.Errorf("decode vmess: %w", err)
	}

	var d map[string]any
	if err := json.Unmarshal([]byte(decoded), &d); err != nil {
		return fmt.Errorf("unmarshal vmess: %w", err)
	}

	c.Server, _ = d["add"].(string)
	c.UUID, _ = d["id"].(string)

	switch p := d["port"].(type) {
	case float64:
		c.Port = int(p)
	case string:
		c.Port, _ = strconv.Atoi(p)
	}
	if c.Port <= 0 || c.Port > 65535 || c.Server == "" {
		return fmt.Errorf("invalid server or port in vmess")
	}

	if ps, ok := d["ps"].(string); ok && ps != "" {
		c.Name = ps
	} else {
		c.Name = fmt.Sprintf("vmess-%s:%d", c.Server, c.Port)
	}

	c.Transport, _ = d["net"].(string)
	if c.Transport == "" {
		c.Transport = "tcp"
	}
	c.TLS, _ = d["tls"].(string)
	if c.TLS == "" {
		c.TLS = "none"
	}
	c.SNI, _ = d["sni"].(string)
	c.Path, _ = d["path"].(string)
	c.Host, _ = d["host"].(string)

	return nil
}

func parseTrojanDetails(link string, c *models.Config) error {
	u, err := url.Parse(link)
	if err != nil {
		return err
	}
	q := u.Query()

	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 || port > 65535 {
		return fmt.Errorf("invalid trojan port")
	}

	c.Server = u.Hostname()
	c.Port = port
	if u.User != nil {
		c.Password = u.User.Username()
	}
	c.Name = u.Fragment
	if c.Name == "" {
		c.Name = fmt.Sprintf("trojan-%s:%d", c.Server, c.Port)
	}

	c.Transport = q.Get("type")
	if c.Transport == "" {
		c.Transport = "tcp"
	}
	c.TLS = q.Get("security")
	if c.TLS == "" {
		c.TLS = "tls"
	}
	c.SNI = q.Get("sni")
	c.Path = q.Get("path")
	c.Host = q.Get("host")

	return nil
}

func parseSSDetails(link string, c *models.Config) error {
	u, err := url.Parse(link)
	if err != nil {
		return err
	}

	c.Name = u.Fragment
	if u.User != nil {
		// Standard ss://BASE64@host:port or ss://method:password@host:port
		userStr := u.User.String()
		if !strings.Contains(userStr, ":") {
			decoded, err := decodeBase64(userStr)
			if err == nil {
				userStr = decoded
			}
		}
		c.Password = userStr
		c.Server = u.Hostname()
		p, _ := strconv.Atoi(u.Port())
		c.Port = p
	} else if u.Host != "" {
		// ss://BASE64
		decoded, err := decodeBase64(u.Host)
		if err == nil {
			// method:password@host:port
			parts := strings.Split(decoded, "@")
			if len(parts) == 2 {
				c.Password = parts[0]
				hp := strings.Split(parts[1], ":")
				if len(hp) == 2 {
					c.Server = hp[0]
					c.Port, _ = strconv.Atoi(hp[1])
				}
			}
		}
	}

	if c.Port <= 0 || c.Server == "" {
		return fmt.Errorf("invalid shadowsocks address")
	}
	if c.Name == "" {
		c.Name = fmt.Sprintf("ss-%s:%d", c.Server, c.Port)
	}
	c.Transport = "tcp"
	c.TLS = "none"

	return nil
}

func parseHysteria2Details(link string, c *models.Config) error {
	u, err := url.Parse(link)
	if err != nil {
		return err
	}
	q := u.Query()

	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 {
		return fmt.Errorf("invalid hy2 port")
	}

	c.Server = u.Hostname()
	c.Port = port
	if u.User != nil {
		c.Password = u.User.Username()
	}
	c.Name = u.Fragment
	if c.Name == "" {
		c.Name = fmt.Sprintf("hy2-%s:%d", c.Server, c.Port)
	}
	c.Transport = "udp"
	c.TLS = "tls"
	c.SNI = q.Get("sni")

	return nil
}

func parseTUICDetails(link string, c *models.Config) error {
	u, err := url.Parse(link)
	if err != nil {
		return err
	}
	q := u.Query()

	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 {
		return fmt.Errorf("invalid tuic port")
	}

	c.Server = u.Hostname()
	c.Port = port
	if u.User != nil {
		c.UUID = u.User.Username()
		c.Password, _ = u.User.Password()
	}
	c.Name = u.Fragment
	if c.Name == "" {
		c.Name = fmt.Sprintf("tuic-%s:%d", c.Server, c.Port)
	}
	c.Transport = "quic"
	c.TLS = "tls"
	c.SNI = q.Get("sni")

	return nil
}

func parseSSRDetails(link string, c *models.Config) error {
	raw := strings.TrimPrefix(link, "ssr://")
	decoded, err := decodeBase64(raw)
	if err != nil {
		return err
	}
	parts := strings.Split(decoded, "/?")
	if len(parts) < 1 {
		return fmt.Errorf("invalid ssr")
	}
	mainParts := strings.Split(parts[0], ":")
	if len(mainParts) < 6 {
		return fmt.Errorf("invalid ssr structure")
	}

	c.Server = mainParts[0]
	c.Port, _ = strconv.Atoi(mainParts[1])
	c.Password = mainParts[5]
	c.Transport = "tcp"
	c.TLS = "none"
	c.Name = fmt.Sprintf("ssr-%s:%d", c.Server, c.Port)

	return nil
}

func decodeBase64(s string) (string, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	if rem := len(s) % 4; rem > 0 {
		s += strings.Repeat("=", 4-rem)
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ConvertToXrayOutbound builds the outbound JSON structure for Xray-core
func ConvertToXrayOutbound(c *models.Config) (M, error) {
	switch c.Protocol {
	case "vless":
		user := M{
			"id":         c.UUID,
			"encryption": "none",
		}
		stream := buildStreamSettings(c)
		return M{
			"protocol": "vless",
			"tag":      "proxy",
			"settings": M{
				"vnext": []M{{
					"address": c.Server,
					"port":    c.Port,
					"users":   []M{user},
				}},
			},
			"streamSettings": stream,
		}, nil

	case "vmess":
		user := M{
			"id":       c.UUID,
			"security": "auto",
		}
		stream := buildStreamSettings(c)
		return M{
			"protocol": "vmess",
			"tag":      "proxy",
			"settings": M{
				"vnext": []M{{
					"address": c.Server,
					"port":    c.Port,
					"users":   []M{user},
				}},
			},
			"streamSettings": stream,
		}, nil

	case "trojan":
		stream := buildStreamSettings(c)
		return M{
			"protocol": "trojan",
			"tag":      "proxy",
			"settings": M{
				"servers": []M{{
					"address":  c.Server,
					"port":     c.Port,
					"password": c.Password,
				}},
			},
			"streamSettings": stream,
		}, nil

	case "ss":
		method := "aes-256-gcm"
		pass := c.Password
		if strings.Contains(c.Password, ":") {
			parts := strings.SplitN(c.Password, ":", 2)
			method = parts[0]
			pass = parts[1]
		}
		return M{
			"protocol": "shadowsocks",
			"tag":      "proxy",
			"settings": M{
				"servers": []M{{
					"address":  c.Server,
					"port":     c.Port,
					"method":   method,
					"password": pass,
				}},
			},
		}, nil

	case "hysteria2":
		return M{
			"protocol": "hysteria2",
			"tag":      "proxy",
			"settings": M{
				"servers": []M{{
					"address":  c.Server,
					"port":     c.Port,
					"password": c.Password,
				}},
			},
			"streamSettings": M{
				"security": "tls",
				"tlsSettings": M{
					"serverName": c.SNI,
				},
			},
		}, nil

	case "wireguard":
		port := c.Port
		if port <= 0 {
			port = 2408
		}
		addr := c.UUID
		if addr == "" {
			addr = "172.16.0.2/32"
		}
		pubKey := c.Reality
		if pubKey == "" {
			pubKey = "bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo="
		}
		endpoint := net.JoinHostPort(c.Server, strconv.Itoa(port))
		peer := M{
			"publicKey": pubKey,
			"endpoint":  endpoint,
		}

		settings := M{
			"secretKey": c.Password,
			"address": []string{
				addr,
			},
			"peers": []M{
				peer,
			},
			"mtu": 1280,
		}

		// Parse reserved bytes (e.g. "209,89,21" in c.Path or in c.Tags)
		reservedStr := c.Path
		if reservedStr == "" && strings.Contains(c.Tags, "reserved=") {
			for _, tag := range strings.Split(c.Tags, ",") {
				if strings.HasPrefix(strings.TrimSpace(tag), "reserved=") {
					reservedStr = strings.TrimPrefix(strings.TrimSpace(tag), "reserved=")
					break
				}
			}
		}
		if reservedStr != "" && strings.Contains(reservedStr, ",") {
			parts := strings.Split(reservedStr, ",")
			if len(parts) == 3 {
				var res []int
				for _, p := range parts {
					if v, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
						res = append(res, v)
					}
				}
				if len(res) == 3 {
					settings["reserved"] = res
					peer["reserved"] = res
				}
			}
		}

		return M{
			"protocol": "wireguard",
			"tag":      "proxy",
			"settings": settings,
		}, nil

	default:
		return nil, fmt.Errorf("protocol %s not supported by Xray outbound converter", c.Protocol)
	}
}

func buildStreamSettings(c *models.Config) M {
	stream := M{
		"network": c.Transport,
	}

	switch c.Transport {
	case "ws":
		ws := M{}
		if c.Path != "" {
			ws["path"] = c.Path
		}
		if c.Host != "" {
			ws["headers"] = M{"Host": c.Host}
		}
		stream["wsSettings"] = ws
	case "grpc":
		grpc := M{}
		if c.Path != "" {
			grpc["serviceName"] = c.Path
		}
		stream["grpcSettings"] = grpc
	case "xhttp", "splithttp":
		xhttp := M{}
		if c.Path != "" {
			xhttp["path"] = c.Path
		}
		if c.Host != "" {
			xhttp["host"] = c.Host
		}
		stream["xhttpSettings"] = xhttp
	}

	if c.TLS == "reality" {
		stream["security"] = "reality"
		reality := M{
			"publicKey": c.Reality,
			"fingerprint": "chrome",
		}
		if c.SNI != "" {
			reality["serverName"] = c.SNI
		}
		stream["realitySettings"] = reality
	} else if c.TLS == "tls" {
		stream["security"] = "tls"
		tls := M{
			"fingerprint": "chrome",
		}
		if c.SNI != "" {
			tls["serverName"] = c.SNI
		}
		stream["tlsSettings"] = tls
	} else {
		stream["security"] = "none"
	}

	return stream
}
