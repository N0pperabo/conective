package database

import (
	"path/filepath"
	"testing"

	"freenode/pkg/models"
)

func TestFavoritesAndTags(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_fav.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	// Seed test configs
	configs := []*models.Config{
		{
			Identity:   "node-1",
			Protocol:   "vless",
			Name:       "US Fast Server",
			Server:     "1.1.1.1",
			Port:       443,
			Country:    "US",
			Status:     "working",
			Latency:    80,
			Score:      95,
			IsFavorite: false,
			Tags:       "fast, streaming",
			RawLink:    "vless://node-1",
		},
		{
			Identity:   "node-2",
			Protocol:   "vmess",
			Name:       "DE German Node",
			Server:     "2.2.2.2",
			Port:       8443,
			Country:    "DE",
			Status:     "working",
			Latency:    120,
			Score:      85,
			IsFavorite: true,
			Tags:       "europe, gaming",
			RawLink:    "vmess://node-2",
		},
		{
			Identity:   "node-3",
			Protocol:   "trojan",
			Name:       "SG Singapore Proxy",
			Server:     "3.3.3.3",
			Port:       443,
			Country:    "SG",
			Status:     "dead",
			Latency:    -1,
			Score:      10,
			IsFavorite: false,
			Tags:       "asia",
			RawLink:    "trojan://node-3",
		},
	}

	err = db.UpsertBatch(configs)
	if err != nil {
		t.Fatalf("failed to save configs: %v", err)
	}

	// Fetch all
	all, total, err := db.GetConfigs(ConfigFilter{})
	if err != nil {
		t.Fatalf("failed to get configs: %v", err)
	}
	if total != 3 || len(all) != 3 {
		t.Fatalf("expected 3 configs, got %d", total)
	}

	var node1, node2 models.Config
	for _, c := range all {
		if c.Identity == "node-1" {
			node1 = c
		} else if c.Identity == "node-2" {
			node2 = c
		}
	}

	if node1.IsFavorite != false {
		t.Errorf("expected node1 IsFavorite to be false, got true")
	}
	if node2.IsFavorite != true {
		t.Errorf("expected node2 IsFavorite to be true, got false")
	}

	// 1. ToggleFavorite on node1 (false -> true)
	newFav, err := db.ToggleFavorite(node1.ID)
	if err != nil {
		t.Fatalf("failed to toggle favorite: %v", err)
	}
	if !newFav {
		t.Errorf("expected newFav=true, got false")
	}

	updated1, err := db.GetConfigByID(node1.ID)
	if err != nil {
		t.Fatalf("failed to get config: %v", err)
	}
	if !updated1.IsFavorite {
		t.Errorf("expected updated node1 IsFavorite=true")
	}

	// ToggleFavorite again (true -> false)
	newFav, err = db.ToggleFavorite(node1.ID)
	if err != nil {
		t.Fatalf("failed to toggle favorite: %v", err)
	}
	if newFav {
		t.Errorf("expected newFav=false, got true")
	}

	// 2. SetNodeTags on node1
	err = db.SetNodeTags(node1.ID, "superfast, premium")
	if err != nil {
		t.Fatalf("failed to set node tags: %v", err)
	}
	updated1, _ = db.GetConfigByID(node1.ID)
	if updated1.Tags != "superfast, premium" {
		t.Errorf("expected tags 'superfast, premium', got '%s'", updated1.Tags)
	}

	// 3. Filter by is_favorite
	isFavTrue := true
	favItems, favTotal, err := db.GetConfigs(ConfigFilter{IsFavorite: &isFavTrue})
	if err != nil {
		t.Fatalf("failed to filter favorites: %v", err)
	}
	if favTotal != 1 || len(favItems) != 1 || favItems[0].Identity != "node-2" {
		t.Errorf("expected 1 favorite (node-2), got %d items", favTotal)
	}

	// 4. Filter by tag
	tagItems, tagTotal, err := db.GetConfigs(ConfigFilter{Tag: "premium"})
	if err != nil {
		t.Fatalf("failed to filter by tag: %v", err)
	}
	if tagTotal != 1 || len(tagItems) != 1 || tagItems[0].Identity != "node-1" {
		t.Errorf("expected 1 item with tag 'premium', got %d", tagTotal)
	}

	// Search matching tag
	searchItems, searchTotal, err := db.GetConfigs(ConfigFilter{Search: "gaming"})
	if err != nil {
		t.Fatalf("failed to search by tag: %v", err)
	}
	if searchTotal != 1 || searchItems[0].Identity != "node-2" {
		t.Errorf("expected search for 'gaming' to find node-2, got %d", searchTotal)
	}
}

func TestPsiphonAndCleanIPFiltering(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_psiphon.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	configs := []*models.Config{
		{
			Identity: "psi-proto",
			Protocol: "psiphon",
			Name:     "Psiphon Official Node",
			Server:   "10.0.0.1",
			Port:     443,
			Country:  "US",
			Status:   "working",
			Latency:  150,
			Score:    90,
			Tags:     "psiphon, clean-ip, vpn",
		},
		{
			Identity: "vless-clean-ip",
			Protocol: "vless",
			Name:     "Cloudflare Clean IP Node",
			Server:   "104.16.1.1",
			Port:     443,
			Country:  "CF",
			Status:   "working",
			Latency:  50,
			Score:    99,
			Tags:     "clean-ip, cdn",
		},
		{
			Identity: "vmess-normal",
			Protocol: "vmess",
			Name:     "Standard VMess Node",
			Server:   "192.168.1.1",
			Port:     8443,
			Country:  "DE",
			Status:   "working",
			Latency:  100,
			Score:    80,
			Tags:     "standard",
		},
	}

	if err := db.UpsertBatch(configs); err != nil {
		t.Fatalf("failed to insert configs: %v", err)
	}

	// 1. Filter with Protocol: "psiphon" (should match psi-proto and vless-clean-ip due to tags)
	resPsi, totalPsi, err := db.GetConfigs(ConfigFilter{Protocol: "psiphon"})
	if err != nil {
		t.Fatalf("failed to get configs for psiphon: %v", err)
	}
	if totalPsi != 2 || len(resPsi) != 2 {
		t.Errorf("expected 2 configs for protocol 'psiphon', got %d", totalPsi)
	}

	// 2. Filter with Protocol: "clean-ip"
	resClean, totalClean, err := db.GetConfigs(ConfigFilter{Protocol: "clean-ip"})
	if err != nil {
		t.Fatalf("failed to get configs for clean-ip: %v", err)
	}
	if totalClean != 2 || len(resClean) != 2 {
		t.Errorf("expected 2 configs for protocol 'clean-ip', got %d", totalClean)
	}

	// 3. Filter with Protocol: "vmess" should only return vmess-normal
	resVmess, totalVmess, err := db.GetConfigs(ConfigFilter{Protocol: "vmess"})
	if err != nil {
		t.Fatalf("failed to get configs for vmess: %v", err)
	}
	if totalVmess != 1 || len(resVmess) != 1 || resVmess[0].Identity != "vmess-normal" {
		t.Errorf("expected 1 vmess config, got %d", totalVmess)
	}
}

