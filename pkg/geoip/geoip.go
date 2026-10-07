package geoip

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/oschwald/geoip2-golang"
)

type Resolver struct {
	db         *geoip2.Reader
	cache      sync.Map
	cacheCount atomic.Int64
}

var (
	countryNames = map[string]string{
		"US": "United States", "DE": "Germany", "GB": "United Kingdom", "FR": "France",
		"NL": "Netherlands", "CA": "Canada", "SG": "Singapore", "JP": "Japan",
		"KR": "South Korea", "TR": "Turkey", "RU": "Russia", "IR": "Iran",
		"AE": "United Arab Emirates", "FI": "Finland", "SE": "Sweden", "PL": "Poland",
		"IT": "Italy", "ES": "Spain", "CH": "Switzerland", "AT": "Austria",
		"AU": "Australia", "IN": "India", "BR": "Brazil", "HK": "Hong Kong",
		"TW": "Taiwan", "UA": "Ukraine", "RO": "Romania", "NO": "Norway",
		"DK": "Denmark", "CZ": "Czechia", "BE": "Belgium", "IE": "Ireland",
		"IL": "Israel", "KZ": "Kazakhstan", "AM": "Armenia", "GE": "Georgia",
		"AZ": "Azerbaijan", "UZ": "Uzbekistan", "MD": "Moldova", "BG": "Bulgaria",
		"HU": "Hungary", "GR": "Greece", "PT": "Portugal", "ZA": "South Africa",
	}
)

func New(dbPath string) (*Resolver, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return &Resolver{}, fmt.Errorf("geoip mmdb not found at %s: %w", dbPath, err)
	}

	reader, err := geoip2.Open(dbPath)
	if err != nil {
		return &Resolver{}, fmt.Errorf("opening geoip mmdb: %w", err)
	}

	return &Resolver{db: reader}, nil
}

func (r *Resolver) Close() error {
	if r.db != nil {
		return r.db.Close()
	}
	return nil
}

type GeoResult struct {
	Code string
	Name string
	Flag string
}

func (r *Resolver) Lookup(hostOrIP string) GeoResult {
	hostOrIP = strings.TrimSpace(hostOrIP)
	if hostOrIP == "" {
		return GeoResult{Code: "UN", Name: "Unknown", Flag: "🌐"}
	}

	// Check cache
	if val, ok := r.cache.Load(hostOrIP); ok {
		return val.(GeoResult)
	}

	ip := net.ParseIP(hostOrIP)
	res := GeoResult{Code: "UN", Name: "Unknown", Flag: "🌐"}

	if ip != nil && r.db != nil {
		record, err := r.db.Country(ip)
		if err == nil && record.Country.IsoCode != "" {
			code := strings.ToUpper(record.Country.IsoCode)
			name := record.Country.Names["en"]
			if name == "" {
				if n, exists := countryNames[code]; exists {
					name = n
				} else {
					name = code
				}
			}
			res = GeoResult{
				Code: code,
				Name: name,
				Flag: Flag(code),
			}
		}
	}

	if r.cacheCount.Load() < 4000 {
		r.cache.Store(hostOrIP, res)
		r.cacheCount.Add(1)
	}
	return res
}

func Flag(isoCode string) string {
	code := strings.ToUpper(strings.TrimSpace(isoCode))
	if code == "CF" || code == "CLOUDFLARE" || code == "WW" || code == "GLOBAL" {
		return "☁️"
	}
	if len(code) != 2 {
		return "🌐"
	}
	// Unicode regional indicator symbols
	return string(rune(code[0])+127397) + string(rune(code[1])+127397)
}
