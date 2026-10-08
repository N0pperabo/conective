package database

import (
	"path/filepath"
	"testing"

	"freenode/pkg/models"
)

func TestSeedDefaultSourcesPersistence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_sources_persist.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	defaults := []models.Source{
		{Name: "freedom", URL: "https://example.com/freedom", Enabled: true, Format: "auto"},
		{Name: "v2go", URL: "https://example.com/v2go", Enabled: true, Format: "auto"},
		{Name: "custom_default", URL: "https://example.com/custom", Enabled: false, Format: "auto"},
	}

	// 1. Initial Seed
	if err := db.SeedDefaultSources(defaults); err != nil {
		t.Fatalf("SeedDefaultSources failed: %v", err)
	}

	sources, err := db.GetSources()
	if err != nil {
		t.Fatalf("GetSources failed: %v", err)
	}

	var freedomSource, customSource models.Source
	for _, s := range sources {
		if s.Name == "freedom" {
			freedomSource = s
			if !s.Enabled {
				t.Errorf("expected freedom to be enabled initially")
			}
		}
		if s.Name == "custom_default" {
			customSource = s
			if s.Enabled {
				t.Errorf("expected custom_default to be disabled initially")
			}
		}
	}

	// 2. User modifies sources: disables freedom, enables custom_default
	freedomSource.Enabled = false
	if err := db.UpdateSource(freedomSource); err != nil {
		t.Fatalf("UpdateSource freedom failed: %v", err)
	}
	customSource.Enabled = true
	if err := db.UpdateSource(customSource); err != nil {
		t.Fatalf("UpdateSource custom failed: %v", err)
	}

	// 3. Simulate App Restart: SeedDefaultSources runs again
	if err := db.SeedDefaultSources(defaults); err != nil {
		t.Fatalf("SeedDefaultSources restart failed: %v", err)
	}

	sourcesAfter, err := db.GetSources()
	if err != nil {
		t.Fatalf("GetSources failed: %v", err)
	}

	for _, s := range sourcesAfter {
		if s.Name == "freedom" && s.Enabled {
			t.Errorf("expected freedom to remain disabled after restart, but got enabled=true")
		}
		if s.Name == "custom_default" && !s.Enabled {
			t.Errorf("expected custom_default to remain enabled after restart, but got enabled=false")
		}
	}
}

func TestUpdateRepoMigration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_repo_migration.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	// Initial Open() runs migrate, seeded update_repo should be N0pperabo/conective
	repo := db.GetSetting("update_repo", "")
	if repo != "N0pperabo/conective" {
		t.Errorf("expected seeded update_repo to be N0pperabo/conective, got %s", repo)
	}

	// Set legacy repo connective-app/connective
	if err := db.SetSetting("update_repo", "connective-app/connective"); err != nil {
		t.Fatalf("SetSetting failed: %v", err)
	}

	// Re-run migration via Init()
	if err := db.Init(); err != nil {
		t.Fatalf("Init() migration failed: %v", err)
	}

	repoAfter := db.GetSetting("update_repo", "")
	if repoAfter != "N0pperabo/conective" {
		t.Errorf("expected migrated update_repo to be N0pperabo/conective, got %s", repoAfter)
	}

	// Ensure custom user fork is preserved
	if err := db.SetSetting("update_repo", "custom-user/conective"); err != nil {
		t.Fatalf("SetSetting custom failed: %v", err)
	}
	if err := db.Init(); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}
	repoCustom := db.GetSetting("update_repo", "")
	if repoCustom != "custom-user/conective" {
		t.Errorf("expected custom repo to be preserved, got %s", repoCustom)
	}
}

func TestUserSettingsPersistenceAcrossRestarts(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_user_settings_persist.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	// Initial defaults
	if c := db.GetSetting("test_concurrency", ""); c != "100" {
		t.Errorf("expected default test_concurrency 100, got %s", c)
	}

	// User customizes settings
	if err := db.SetSetting("test_concurrency", "500"); err != nil {
		t.Fatalf("SetSetting test_concurrency failed: %v", err)
	}
	if err := db.SetSetting("test_timeout_sec", "10"); err != nil {
		t.Fatalf("SetSetting test_timeout_sec failed: %v", err)
	}
	if err := db.SetSetting("auto_scan", "false"); err != nil {
		t.Fatalf("SetSetting auto_scan failed: %v", err)
	}
	if err := db.SetSetting("auto_scan_interval", "60"); err != nil {
		t.Fatalf("SetSetting auto_scan_interval failed: %v", err)
	}
	if err := db.SetSetting("clean_ip_workers", "200"); err != nil {
		t.Fatalf("SetSetting clean_ip_workers failed: %v", err)
	}

	// Simulate app restart: Init() is called again
	if err := db.Init(); err != nil {
		t.Fatalf("Init() restart failed: %v", err)
	}

	// Assert custom settings were NOT overwritten
	if c := db.GetSetting("test_concurrency", ""); c != "500" {
		t.Errorf("expected test_concurrency to remain 500, got %s", c)
	}
	if to := db.GetSetting("test_timeout_sec", ""); to != "10" {
		t.Errorf("expected test_timeout_sec to remain 10, got %s", to)
	}
	if as := db.GetSetting("auto_scan", ""); as != "false" {
		t.Errorf("expected auto_scan to remain false, got %s", as)
	}
	if asi := db.GetSetting("auto_scan_interval", ""); asi != "60" {
		t.Errorf("expected auto_scan_interval to remain 60, got %s", asi)
	}
	if cip := db.GetSetting("clean_ip_workers", ""); cip != "200" {
		t.Errorf("expected clean_ip_workers to remain 200, got %s", cip)
	}
}

