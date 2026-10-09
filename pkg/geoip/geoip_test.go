package geoip

import (
	"path/filepath"
	"testing"
)

func TestGeoAkamai(t *testing.T) {
	mmdbPath := filepath.Join("..", "..", "assets", "GeoLite2-Country.mmdb")
	res, err := New(mmdbPath)
	if err != nil {
		t.Fatalf("failed to open mmdb: %v", err)
	}
	defer res.Close()

	testIPs := []string{
		"184.24.77.42",
		"23.209.210.213",
		"23.213.161.22",
		"23.48.23.151",
		"92.123.102.43",
		"2.22.250.149",
		"2.21.0.1",
		"2.17.0.1",
		"2.18.0.1",
		"92.122.0.1",
		"95.100.0.1",
		"195.219.0.1",
		"185.200.232.49",
	}

	for _, ip := range testIPs {
		g := res.Lookup(ip)
		if g.Code == "" || g.Code == "UN" {
			t.Errorf("expected valid country code for %s, got %q", ip, g.Code)
		}
		if g.Name == "" || g.Name == "Unknown" {
			t.Errorf("expected valid country name for %s, got %q", ip, g.Name)
		}
		t.Logf("IP %s -> Code: %s, Name: %s", ip, g.Code, g.Name)
	}

	if CountryName("DE") != "Germany" {
		t.Errorf("expected Germany, got %s", CountryName("DE"))
	}
	if CountryName("GB") != "United Kingdom" {
		t.Errorf("expected United Kingdom, got %s", CountryName("GB"))
	}
}
