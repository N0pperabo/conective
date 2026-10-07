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
