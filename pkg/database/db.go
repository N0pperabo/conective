package database

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"freenode/pkg/models"
)

type DB struct {
	db *sql.DB
	mu sync.RWMutex
}

func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite at %s: %w", path, err)
	}

	// Limit connections to prevent modernc.org/libc memory duplication across threads
	sqlDB.SetMaxOpenConns(2)
	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	// Optimized SQLite pragmas for low memory usage and high concurrency
	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA cache_size = -2000;", // 2MB cache limit instead of 40MB+
		"PRAGMA mmap_size = 0;",       // Disable mmap to prevent unbounded virtual memory allocation in Go heap
		"PRAGMA temp_store = FILE;",   // Use disk for temp tables/sorts instead of Go heap modernc memory
		"PRAGMA wal_autocheckpoint = 500;",
		"PRAGMA foreign_keys = ON;",
	}
	for _, p := range pragmas {
		if _, err := sqlDB.Exec(p); err != nil {
			return nil, fmt.Errorf("executing pragma %s: %w", p, err)
		}
	}

	d := &DB{db: sqlDB}
	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	// Startup Crash Recovery: if any scan session was left in "running" state, mark it cancelled
	_, _ = sqlDB.Exec("UPDATE scan_sessions SET status = 'interrupted' WHERE status = 'running'")

	return d, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS sources (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		url TEXT NOT NULL UNIQUE,
		enabled INTEGER NOT NULL DEFAULT 1,
		format TEXT NOT NULL DEFAULT 'auto',
		last_update DATETIME,
		configs_found INTEGER NOT NULL DEFAULT 0,
		working_nodes INTEGER NOT NULL DEFAULT 0,
		error TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS configs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		identity TEXT NOT NULL UNIQUE,
		protocol TEXT NOT NULL,
		name TEXT NOT NULL,
		server TEXT NOT NULL,
		port INTEGER NOT NULL,
		uuid TEXT NOT NULL DEFAULT '',
		password TEXT NOT NULL DEFAULT '',
		transport TEXT NOT NULL DEFAULT 'tcp',
		tls TEXT NOT NULL DEFAULT 'none',
		sni TEXT NOT NULL DEFAULT '',
		path TEXT NOT NULL DEFAULT '',
		host TEXT NOT NULL DEFAULT '',
		reality TEXT NOT NULL DEFAULT '',
		country TEXT NOT NULL DEFAULT 'UN',
		country_name TEXT NOT NULL DEFAULT 'Unknown',
		source TEXT NOT NULL DEFAULT '',
		first_seen DATETIME NOT NULL,
		last_seen DATETIME NOT NULL,
		last_tested DATETIME,
		latency INTEGER NOT NULL DEFAULT -1,
		status TEXT NOT NULL DEFAULT 'untested',
		score INTEGER NOT NULL DEFAULT 0,
		trust_score INTEGER NOT NULL DEFAULT -1,
		risk_level TEXT NOT NULL DEFAULT '',
		threats_count INTEGER NOT NULL DEFAULT 0,
		organisation TEXT NOT NULL DEFAULT '',
		exit_ip TEXT NOT NULL DEFAULT '',
		raw_link TEXT NOT NULL,
		is_favorite INTEGER NOT NULL DEFAULT 0,
		tags TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_configs_status ON configs(status);
	CREATE INDEX IF NOT EXISTS idx_configs_country ON configs(country);
	CREATE INDEX IF NOT EXISTS idx_configs_protocol ON configs(protocol);
	CREATE INDEX IF NOT EXISTS idx_configs_latency ON configs(latency);
	CREATE INDEX IF NOT EXISTS idx_configs_score ON configs(score);

	CREATE TABLE IF NOT EXISTS test_results (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		config_id INTEGER NOT NULL,
		tested_at DATETIME NOT NULL,
		latency INTEGER NOT NULL,
		status TEXT NOT NULL,
		error TEXT NOT NULL DEFAULT '',
		exit_ip TEXT NOT NULL DEFAULT '',
		trust_score INTEGER NOT NULL DEFAULT -1,
		risk_level TEXT NOT NULL DEFAULT '',
		threats_count INTEGER NOT NULL DEFAULT 0,
		organisation TEXT NOT NULL DEFAULT '',
		FOREIGN KEY(config_id) REFERENCES configs(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_test_results_config_id ON test_results(config_id);

	CREATE TABLE IF NOT EXISTS scan_sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		started_at DATETIME NOT NULL,
		finished_at DATETIME,
		status TEXT NOT NULL,
		fetched_count INTEGER NOT NULL DEFAULT 0,
		parsed_count INTEGER NOT NULL DEFAULT 0,
		dedup_count INTEGER NOT NULL DEFAULT 0,
		tested_count INTEGER NOT NULL DEFAULT 0,
		working_count INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS connection_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		config_id INTEGER NOT NULL,
		server TEXT NOT NULL,
		protocol TEXT NOT NULL,
		connected_at DATETIME NOT NULL,
		disconnected_at DATETIME,
		status TEXT NOT NULL
	);
	`
	if _, err := d.db.Exec(schema); err != nil {
		return err
	}

	// Safe column additions for existing databases
	alters := []string{
		"ALTER TABLE configs ADD COLUMN trust_score INTEGER NOT NULL DEFAULT -1",
		"ALTER TABLE configs ADD COLUMN risk_level TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE configs ADD COLUMN threats_count INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE configs ADD COLUMN organisation TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE configs ADD COLUMN exit_ip TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE configs ADD COLUMN is_favorite INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE configs ADD COLUMN tags TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE test_results ADD COLUMN trust_score INTEGER NOT NULL DEFAULT -1",
		"ALTER TABLE test_results ADD COLUMN risk_level TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE test_results ADD COLUMN threats_count INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE test_results ADD COLUMN organisation TEXT NOT NULL DEFAULT ''",
	}
	for _, alter := range alters {
		_, _ = d.db.Exec(alter)
	}

	_, _ = d.db.Exec("CREATE INDEX IF NOT EXISTS idx_configs_trust ON configs(trust_score)")
	_, _ = d.db.Exec("CREATE INDEX IF NOT EXISTS idx_configs_favorite ON configs(is_favorite)")
	_, _ = d.db.Exec("UPDATE settings SET value = 'http://cp.cloudflare.com/generate_204' WHERE key = 'test_endpoint' AND value LIKE '%gstatic%'")
	_, _ = d.db.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('test_concurrency', '100')")
	_, _ = d.db.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('test_timeout_sec', '5')")
	_, _ = d.db.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('auto_scan', 'true')")
	_, _ = d.db.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('auto_scan_interval', '180')")
	_, _ = d.db.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('test_endpoint', 'http://cp.cloudflare.com/generate_204')")
	_, _ = d.db.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('auto_failover', 'true')")
	_, _ = d.db.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('clean_ip_workers', '100')")
	_, _ = d.db.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('clean_ip_timeout', '1500')")
	_, _ = d.db.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('clean_ip_sample_size', '500')")
	_, _ = d.db.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('clean_ip_port', '443')")

	// Seed default routing settings
	_, _ = d.db.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('routing_mode', 'blacklist')")
	_, _ = d.db.Exec("UPDATE settings SET value = 'blacklist' WHERE key = 'routing_mode' AND (value = 'bypass_iran' OR value = '')")
	_, _ = d.db.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('direct_domains', 'regexp:.*\\.ir$\nshaparak.ir\ndigikala.com\ndivar.ir\nsnapp.ir\ntorob.com\nvarzesh3.com\ntelewebion.com\nbale.ai\neitaa.com\nrubika.ir\naparat.com\nfilimo.com')")
	_, _ = d.db.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('proxy_domains', 'google.com\nyoutube.com\ntwitter.com\nx.com\nt.me\ntelegram.org\ninstagram.com\nfacebook.com\ndiscord.com')")
	_, _ = d.db.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('direct_apps', 'cs2.exe\nvalorant.exe\ndota2.exe\nleagueclient.exe\nidman.exe')")
	_, _ = d.db.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('proxy_apps', 'telegram.exe\ndiscord.exe\nchrome.exe\nmsedge.exe\nfirefox.exe\nspotify.exe')")
	_, _ = d.db.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('block_domains', '')")
	_, _ = d.db.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('share_lan', 'false')")
	_, _ = d.db.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('gaming_mode', 'false')")
	_, _ = d.db.Exec("UPDATE settings SET value = 'N0pperabo/conective' WHERE key = 'update_repo' AND (value = 'connective-app/connective' OR value = '' OR value IS NULL)")
	_, _ = d.db.Exec("INSERT OR IGNORE INTO settings (key, value) VALUES ('update_repo', 'N0pperabo/conective')")

	return nil
}

// Init runs database migrations and seeds default values
func (d *DB) Init() error {
	return d.migrate()
}

// SeedDefaultSources ensures all official sources exist in DB and sets defaults
func (d *DB) SeedDefaultSources(defaults []models.Source) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	stmt, err := d.db.Prepare(`
		INSERT OR IGNORE INTO sources (name, url, enabled, format, last_update, configs_found, working_nodes, error)
		VALUES (?, ?, ?, ?, ?, 0, 0, '')
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, s := range defaults {
		enabledInt := 0
		if s.Enabled {
			enabledInt = 1
		}
		_, err := stmt.Exec(s.Name, s.URL, enabledInt, s.Format, time.Now())
		if err != nil {
			return err
		}
	}

	return nil
}

// Sources operations
func (d *DB) GetSources() ([]models.Source, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, name, url, enabled, format, IFNULL(last_update, datetime('now')), configs_found, working_nodes, error
		FROM sources ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.Source
	for rows.Next() {
		var s models.Source
		var enabledInt int
		var lastUpdateStr string
		if err := rows.Scan(&s.ID, &s.Name, &s.URL, &enabledInt, &s.Format, &lastUpdateStr, &s.ConfigsFound, &s.WorkingNodes, &s.Error); err != nil {
			return nil, err
		}
		s.Enabled = enabledInt == 1
		s.LastUpdate, _ = time.Parse("2006-01-02 15:04:05", lastUpdateStr)
		list = append(list, s)
	}
	return list, nil
}

func (d *DB) AddSource(s models.Source) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	res, err := d.db.Exec(`
		INSERT INTO sources (name, url, enabled, format, last_update, configs_found, working_nodes, error)
		VALUES (?, ?, ?, ?, ?, 0, 0, '')
	`, s.Name, s.URL, s.Enabled, s.Format, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) UpdateSource(s models.Source) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE sources SET name = ?, url = ?, enabled = ?, format = ? WHERE id = ?
	`, s.Name, s.URL, s.Enabled, s.Format, s.ID)
	return err
}

func (d *DB) DeleteSource(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec("DELETE FROM sources WHERE id = ?", id)
	return err
}

func (d *DB) UpdateSourceStats(id int64, found, working int, errStr string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE sources SET last_update = ?, configs_found = ?, working_nodes = ?, error = ?
		WHERE id = ?
	`, time.Now(), found, working, errStr, id)
	return err
}

func (d *DB) UpsertConfig(c *models.Config) (int64, error) {
	return 0, d.UpsertBatch([]*models.Config{c})
}

// Configs operations
func (d *DB) UpsertBatch(configs []*models.Config) error {
	if len(configs) == 0 {
		return nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Chunk large slice into 500-item transactions to prevent massive uncommitted heap buffers in modernc SQLite
	chunkSize := 500
	for i := 0; i < len(configs); i += chunkSize {
		end := i + chunkSize
		if end > len(configs) {
			end = len(configs)
		}
		if err := d.upsertChunk(configs[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) upsertChunk(configs []*models.Config) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO configs (
			identity, protocol, name, server, port, uuid, password, transport,
			tls, sni, path, host, reality, country, country_name, source,
			first_seen, last_seen, last_tested, latency, status, score, raw_link,
			is_favorite, tags
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(identity) DO UPDATE SET
			last_seen = excluded.last_seen,
			status = CASE WHEN configs.status = 'dead' THEN 'untested' ELSE configs.status END,
			name = CASE WHEN configs.name LIKE 'v2go |%' THEN configs.name ELSE excluded.name END,
			port = excluded.port,
			raw_link = excluded.raw_link
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now()
	for _, c := range configs {
		favInt := 0
		if c.IsFavorite {
			favInt = 1
		}
		_, _ = stmt.Exec(
			c.Identity, c.Protocol, c.Name, c.Server, c.Port, c.UUID, c.Password, c.Transport,
			c.TLS, c.SNI, c.Path, c.Host, c.Reality, c.Country, c.CountryName, c.Source,
			now, now, c.LastTested, c.Latency, c.Status, c.Score, c.RawLink,
			favInt, c.Tags,
		)
	}

	return tx.Commit()
}

type TestResultUpdate struct {
	Identity     string
	Latency      int
	Status       string
	ExitIP       string
	Country      string
	CountryName  string
	StdName      string
	Score        int
	TrustScore   int
	RiskLevel    string
	ThreatsCount int
	Organisation string
}

func (d *DB) UpdateTestResultBatch(items []TestResultUpdate) error {
	if len(items) == 0 {
		return nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	updateStmt, err := tx.Prepare(`
		UPDATE configs SET
			latency = ?,
			status = ?,
			last_tested = ?,
			country = CASE WHEN ? != '' THEN ? ELSE country END,
			country_name = CASE WHEN ? != '' THEN ? ELSE country_name END,
			name = CASE WHEN ? != '' THEN ? ELSE name END,
			score = ?,
			exit_ip = CASE WHEN ? != '' THEN ? ELSE exit_ip END,
			trust_score = CASE WHEN ? >= 0 THEN ? ELSE trust_score END,
			risk_level = CASE WHEN ? != '' THEN ? ELSE risk_level END,
			threats_count = CASE WHEN ? > 0 THEN ? ELSE threats_count END,
			organisation = CASE WHEN ? != '' THEN ? ELSE organisation END
		WHERE identity = ?
	`)
	if err != nil {
		return err
	}
	defer updateStmt.Close()

	histStmt, err := tx.Prepare(`
		INSERT INTO test_results (config_id, tested_at, latency, status, exit_ip, trust_score, risk_level, threats_count, organisation)
		SELECT id, ?, ?, ?, ?, ?, ?, ?, ? FROM configs WHERE identity = ?
	`)
	if err != nil {
		return err
	}
	defer histStmt.Close()

	now := time.Now()
	for _, item := range items {
		_, _ = updateStmt.Exec(
			item.Latency, item.Status, now,
			item.Country, item.Country,
			item.CountryName, item.CountryName,
			item.StdName, item.StdName,
			item.Score,
			item.ExitIP, item.ExitIP,
			item.TrustScore, item.TrustScore,
			item.RiskLevel, item.RiskLevel,
			item.ThreatsCount, item.ThreatsCount,
			item.Organisation, item.Organisation,
			item.Identity,
		)

		if item.Status == "working" && item.Latency > 0 {
			_, _ = histStmt.Exec(
				now, item.Latency, item.Status, item.ExitIP,
				item.TrustScore, item.RiskLevel, item.ThreatsCount, item.Organisation,
				item.Identity,
			)
		}
	}

	return tx.Commit()
}

func (d *DB) UpdateTestResult(identity string, latency int, status, exitIP, country, countryName, standardizedName string, score int, trustScore int, riskLevel string, threatsCount int, organisation string) error {
	return d.UpdateTestResultBatch([]TestResultUpdate{{
		Identity:     identity,
		Latency:      latency,
		Status:       status,
		ExitIP:       exitIP,
		Country:      country,
		CountryName:  countryName,
		StdName:      standardizedName,
		Score:        score,
		TrustScore:   trustScore,
		RiskLevel:    riskLevel,
		ThreatsCount: threatsCount,
		Organisation: organisation,
	}})
}

func (d *DB) CheckpointWAL() {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, _ = d.db.Exec("PRAGMA wal_checkpoint(TRUNCATE);")
	_, _ = d.db.Exec("PRAGMA shrink_memory;")
}

func (d *DB) UpdateIPData(id int64, exitIP string, trustScore int, riskLevel string, threatsCount int, organisation string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE configs SET
			exit_ip = CASE WHEN ? != '' THEN ? ELSE exit_ip END,
			trust_score = ?,
			risk_level = ?,
			threats_count = ?,
			organisation = ?
		WHERE id = ?
	`, exitIP, exitIP, trustScore, riskLevel, threatsCount, organisation, id)
	return err
}

// UpdateLastTested updates the last_tested timestamp for a config without altering its status, score, or identity.
func (d *DB) UpdateLastTested(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`UPDATE configs SET last_tested = ? WHERE id = ?`, time.Now().Format("2006-01-02 15:04:05"), id)
	return err
}

func (d *DB) ToggleFavorite(id int64) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var current int
	err := d.db.QueryRow("SELECT is_favorite FROM configs WHERE id = ?", id).Scan(&current)
	if err != nil {
		return false, fmt.Errorf("config not found: %w", err)
	}

	newVal := current == 0
	valInt := 0
	if newVal {
		valInt = 1
	}

	_, err = d.db.Exec("UPDATE configs SET is_favorite = ? WHERE id = ?", valInt, id)
	if err != nil {
		return false, err
	}
	return newVal, nil
}

func (d *DB) SetNodeTags(id int64, tags string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec("UPDATE configs SET tags = ? WHERE id = ?", strings.TrimSpace(tags), id)
	return err
}

type ConfigFilter struct {
	Status      string
	Protocol    string
	Country     string
	Search      string
	MaxLatency  int
	MinScore    int
	SortBy      string // "latency", "score", "country", "last_tested"
	SortOrder   string // "ASC", "DESC"
	Limit       int
	Offset      int
	IsFavorite  *bool
	Tag         string
}

func (d *DB) GetConfigs(f ConfigFilter) ([]models.Config, int, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var where []string
	var args []any

	if f.Status != "" && f.Status != "all" {
		where = append(where, "status = ?")
		args = append(args, f.Status)
	}
	if f.Protocol != "" && f.Protocol != "all" {
		if models.IsCDNIPProtocol(f.Protocol) {
			where = append(where, "(protocol = 'psiphon' OR protocol = 'CDN IP' OR tags LIKE '%psiphon%' OR tags LIKE '%clean-ip%' OR tags LIKE '%CDN IP%' OR tags LIKE '%cdn-fronting%')")
		} else {
			where = append(where, "protocol = ?")
			args = append(args, strings.ToLower(f.Protocol))
		}
	}
	if f.Country != "" && f.Country != "all" {
		where = append(where, "country = ?")
		args = append(args, strings.ToUpper(f.Country))
	}
	if f.MaxLatency > 0 {
		where = append(where, "latency > 0 AND latency <= ?")
		args = append(args, f.MaxLatency)
	}
	if f.MinScore > 0 {
		where = append(where, "score >= ?")
		args = append(args, f.MinScore)
	}
	if f.IsFavorite != nil && *f.IsFavorite {
		where = append(where, "is_favorite = 1")
	}
	if f.Tag != "" {
		if models.IsCDNIPTag(f.Tag) {
			where = append(where, "(tags LIKE '%CDN IP%' OR tags LIKE '%cdn-ip%' OR tags LIKE '%clean-ip%' OR tags LIKE '%psiphon%' OR tags LIKE '%cdn-fronting%' OR protocol = 'psiphon' OR protocol = 'CDN IP')")
		} else {
			where = append(where, "tags LIKE ?")
			args = append(args, "%"+f.Tag+"%")
		}
	}
	if f.Search != "" {
		where = append(where, "(name LIKE ? OR server LIKE ? OR country_name LIKE ? OR protocol LIKE ? OR tags LIKE ?)")
		pat := "%" + f.Search + "%"
		args = append(args, pat, pat, pat, pat, pat)
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	// Count total
	countQuery := "SELECT COUNT(*) FROM configs " + whereClause
	var total int
	err := d.db.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Ordering (Default: Lowest Ping)
	orderClause := "ORDER BY status = 'working' DESC, CASE WHEN latency <= 0 THEN 99999 ELSE latency END ASC"
	switch f.SortBy {
	case "latency":
		if strings.ToUpper(f.SortOrder) == "DESC" {
			orderClause = "ORDER BY latency DESC"
		} else {
			orderClause = "ORDER BY CASE WHEN latency <= 0 THEN 99999 ELSE latency END ASC"
		}
	case "score":
		if strings.ToUpper(f.SortOrder) == "ASC" {
			orderClause = "ORDER BY score ASC"
		} else {
			orderClause = "ORDER BY score DESC"
		}
	case "trust":
		if strings.ToUpper(f.SortOrder) == "ASC" {
			orderClause = "ORDER BY CASE WHEN trust_score < 0 THEN 999 ELSE trust_score END ASC"
		} else {
			orderClause = "ORDER BY trust_score DESC"
		}
	case "country":
		orderClause = "ORDER BY country " + f.SortOrder
	case "last_tested":
		orderClause = "ORDER BY last_tested " + f.SortOrder
	}

	limitClause := ""
	if f.Limit > 0 {
		limitClause = fmt.Sprintf("LIMIT %d OFFSET %d", f.Limit, f.Offset)
	}

	query := fmt.Sprintf(`
		SELECT id, identity, protocol, name, server, port, uuid, password,
		       transport, tls, sni, path, host, reality, country, country_name,
		       source, first_seen, last_seen, IFNULL(last_tested, ''), latency, status, score,
		       trust_score, risk_level, threats_count, organisation, exit_ip, raw_link,
		       is_favorite, tags
		FROM configs %s %s %s
	`, whereClause, orderClause, limitClause)

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var configs []models.Config
	for rows.Next() {
		var c models.Config
		var firstSeenStr, lastSeenStr, lastTestedStr string
		var isFavInt int
		err := rows.Scan(
			&c.ID, &c.Identity, &c.Protocol, &c.Name, &c.Server, &c.Port, &c.UUID, &c.Password,
			&c.Transport, &c.TLS, &c.SNI, &c.Path, &c.Host, &c.Reality, &c.Country, &c.CountryName,
			&c.Source, &firstSeenStr, &lastSeenStr, &lastTestedStr, &c.Latency, &c.Status, &c.Score,
			&c.TrustScore, &c.RiskLevel, &c.ThreatsCount, &c.Organisation, &c.ExitIP, &c.RawLink,
			&isFavInt, &c.Tags,
		)
		if err != nil {
			return nil, 0, err
		}
		c.IsFavorite = isFavInt == 1
		c.FirstSeen, _ = time.Parse("2006-01-02 15:04:05", firstSeenStr)
		c.LastSeen, _ = time.Parse("2006-01-02 15:04:05", lastSeenStr)
		if lastTestedStr != "" {
			c.LastTested, _ = time.Parse("2006-01-02 15:04:05", lastTestedStr)
		}
		configs = append(configs, c)
	}

	return configs, total, nil
}

// GetConfigIDs returns only IDs matching a filter, consuming minimal RAM (e.g. 240KB for 30,000 configs)
func (d *DB) GetConfigIDs(f ConfigFilter) ([]int64, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var where []string
	var args []any

	if f.Status != "" && f.Status != "all" {
		where = append(where, "status = ?")
		args = append(args, f.Status)
	}
	if f.Protocol != "" && f.Protocol != "all" {
		if models.IsCDNIPProtocol(f.Protocol) {
			where = append(where, "(protocol = 'psiphon' OR protocol = 'CDN IP' OR tags LIKE '%psiphon%' OR tags LIKE '%clean-ip%' OR tags LIKE '%CDN IP%' OR tags LIKE '%cdn-fronting%')")
		} else {
			where = append(where, "protocol = ?")
			args = append(args, strings.ToLower(f.Protocol))
		}
	}
	if f.Country != "" && f.Country != "all" {
		where = append(where, "country = ?")
		args = append(args, strings.ToUpper(f.Country))
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	query := "SELECT id FROM configs " + whereClause + " ORDER BY id ASC"
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// GetConfigsByIDs fetches a bounded chunk of configs by their IDs for streaming
func (d *DB) GetConfigsByIDs(ids []int64) ([]*models.Config, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT id, identity, protocol, name, server, port, uuid, password,
		       transport, tls, sni, path, host, reality, country, country_name,
		       source, first_seen, last_seen, IFNULL(last_tested, ''), latency, status, score,
		       trust_score, risk_level, threats_count, organisation, exit_ip, raw_link,
		       is_favorite, tags
		FROM configs WHERE id IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []*models.Config
	for rows.Next() {
		c := &models.Config{}
		var firstSeenStr, lastSeenStr, lastTestedStr string
		var isFavInt int
		err := rows.Scan(
			&c.ID, &c.Identity, &c.Protocol, &c.Name, &c.Server, &c.Port, &c.UUID, &c.Password,
			&c.Transport, &c.TLS, &c.SNI, &c.Path, &c.Host, &c.Reality, &c.Country, &c.CountryName,
			&c.Source, &firstSeenStr, &lastSeenStr, &lastTestedStr, &c.Latency, &c.Status, &c.Score,
			&c.TrustScore, &c.RiskLevel, &c.ThreatsCount, &c.Organisation, &c.ExitIP, &c.RawLink,
			&isFavInt, &c.Tags,
		)
		if err != nil {
			return nil, err
		}
		c.IsFavorite = isFavInt == 1
		c.FirstSeen, _ = time.Parse("2006-01-02 15:04:05", firstSeenStr)
		c.LastSeen, _ = time.Parse("2006-01-02 15:04:05", lastSeenStr)
		if lastTestedStr != "" {
			c.LastTested, _ = time.Parse("2006-01-02 15:04:05", lastTestedStr)
		}
		configs = append(configs, c)
	}

	return configs, nil
}

func (d *DB) GetConfigByID(id int64) (*models.Config, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var c models.Config
	var firstSeenStr, lastSeenStr, lastTestedStr string
	var isFavInt int
	err := d.db.QueryRow(`
		SELECT id, identity, protocol, name, server, port, uuid, password,
		       transport, tls, sni, path, host, reality, country, country_name,
		       source, first_seen, last_seen, IFNULL(last_tested, ''), latency, status, score,
		       trust_score, risk_level, threats_count, organisation, exit_ip, raw_link,
		       is_favorite, tags
		FROM configs WHERE id = ?
	`, id).Scan(
		&c.ID, &c.Identity, &c.Protocol, &c.Name, &c.Server, &c.Port, &c.UUID, &c.Password,
		&c.Transport, &c.TLS, &c.SNI, &c.Path, &c.Host, &c.Reality, &c.Country, &c.CountryName,
		&c.Source, &firstSeenStr, &lastSeenStr, &lastTestedStr, &c.Latency, &c.Status, &c.Score,
		&c.TrustScore, &c.RiskLevel, &c.ThreatsCount, &c.Organisation, &c.ExitIP, &c.RawLink,
		&isFavInt, &c.Tags,
	)
	if err != nil {
		return nil, err
	}
	c.IsFavorite = isFavInt == 1
	c.FirstSeen, _ = time.Parse("2006-01-02 15:04:05", firstSeenStr)
	c.LastSeen, _ = time.Parse("2006-01-02 15:04:05", lastSeenStr)
	if lastTestedStr != "" {
		c.LastTested, _ = time.Parse("2006-01-02 15:04:05", lastTestedStr)
	}
	return &c, nil
}

// GetConfigByIdentity retrieves a config by its unique identity hash
func (d *DB) GetConfigByIdentity(identity string) (*models.Config, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var c models.Config
	var firstSeenStr, lastSeenStr, lastTestedStr string
	var isFavInt int
	err := d.db.QueryRow(`
		SELECT id, identity, protocol, name, server, port, uuid, password,
		       transport, tls, sni, path, host, reality, country, country_name,
		       source, first_seen, last_seen, IFNULL(last_tested, ''), latency, status, score,
		       trust_score, risk_level, threats_count, organisation, exit_ip, raw_link,
		       is_favorite, tags
		FROM configs WHERE identity = ?
	`, identity).Scan(
		&c.ID, &c.Identity, &c.Protocol, &c.Name, &c.Server, &c.Port, &c.UUID, &c.Password,
		&c.Transport, &c.TLS, &c.SNI, &c.Path, &c.Host, &c.Reality, &c.Country, &c.CountryName,
		&c.Source, &firstSeenStr, &lastSeenStr, &lastTestedStr, &c.Latency, &c.Status, &c.Score,
		&c.TrustScore, &c.RiskLevel, &c.ThreatsCount, &c.Organisation, &c.ExitIP, &c.RawLink,
		&isFavInt, &c.Tags,
	)
	if err != nil {
		return nil, err
	}
	c.IsFavorite = isFavInt == 1
	c.FirstSeen, _ = time.Parse("2006-01-02 15:04:05", firstSeenStr)
	c.LastSeen, _ = time.Parse("2006-01-02 15:04:05", lastSeenStr)
	if lastTestedStr != "" {
		c.LastTested, _ = time.Parse("2006-01-02 15:04:05", lastTestedStr)
	}
	return &c, nil
}

// UpdateConfig updates server, port, tls, sni, host, path, name, raw_link and identity of an existing config
func (d *DB) UpdateConfig(c *models.Config) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		UPDATE configs SET
			identity = ?,
			server = ?,
			port = ?,
			tls = ?,
			sni = ?,
			host = ?,
			path = ?,
			name = ?,
			raw_link = ?
		WHERE id = ?
	`, c.Identity, c.Server, c.Port, c.TLS, c.SNI, c.Host, c.Path, c.Name, c.RawLink, c.ID)
	return err
}

func (d *DB) DeleteConfig(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec("DELETE FROM configs WHERE id = ?", id)
	return err
}

func (d *DB) ClearAllConfigs() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec("DELETE FROM configs")
	return err
}

// Scan Session management
func (d *DB) CreateScanSession() (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	res, err := d.db.Exec(`
		INSERT INTO scan_sessions (started_at, status)
		VALUES (?, 'running')
	`, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) UpdateScanSession(id int64, status string, fetched, parsed, dedup, tested, working int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	_, err := d.db.Exec(`
		UPDATE scan_sessions SET
			finished_at = ?,
			status = ?,
			fetched_count = ?,
			parsed_count = ?,
			dedup_count = ?,
			tested_count = ?,
			working_count = ?
		WHERE id = ?
	`, now, status, fetched, parsed, dedup, tested, working, id)
	return err
}

func (d *DB) GetScanSessions() ([]models.ScanSession, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, started_at, IFNULL(finished_at, ''), status,
		       fetched_count, parsed_count, dedup_count, tested_count, working_count
		FROM scan_sessions ORDER BY id DESC LIMIT 50
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.ScanSession
	for rows.Next() {
		var s models.ScanSession
		var startStr, finStr string
		err := rows.Scan(&s.ID, &startStr, &finStr, &s.Status,
			&s.FetchedCount, &s.ParsedCount, &s.DedupCount, &s.TestedCount, &s.WorkingCount)
		if err != nil {
			return nil, err
		}
		s.StartedAt, _ = time.Parse("2006-01-02 15:04:05", startStr)
		if finStr != "" {
			s.FinishedAt, _ = time.Parse("2006-01-02 15:04:05", finStr)
		}
		list = append(list, s)
	}
	return list, nil
}

func (d *DB) ClearScanHistory() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec("DELETE FROM scan_sessions")
	return err
}

// Connection History
func (d *DB) RecordConnectionStart(configID int64, server, protocol string) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	res, err := d.db.Exec(`
		INSERT INTO connection_history (config_id, server, protocol, connected_at, status)
		VALUES (?, ?, ?, ?, 'connected')
	`, configID, server, protocol, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) RecordConnectionEnd(id int64, status string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	_, err := d.db.Exec(`
		UPDATE connection_history SET disconnected_at = ?, status = ? WHERE id = ?
	`, now, status, id)
	return err
}

// Statistics
type SystemStats struct {
	TotalConfigs    int            `json:"total_configs"`
	WorkingConfigs  int            `json:"working_configs"`
	DeadConfigs     int            `json:"dead_configs"`
	FavoriteConfigs int            `json:"favorite_configs"`
	AvgLatency      int            `json:"avg_latency"`
	ByCountry       map[string]int `json:"by_country"`
	ByProtocol      map[string]int `json:"by_protocol"`
	TotalSources    int            `json:"total_sources"`
	ActiveSources   int            `json:"active_sources"`
}

func (d *DB) GetStats() (SystemStats, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var stats SystemStats
	stats.ByCountry = make(map[string]int)
	stats.ByProtocol = make(map[string]int)

	_ = d.db.QueryRow("SELECT COUNT(*) FROM configs").Scan(&stats.TotalConfigs)
	_ = d.db.QueryRow("SELECT COUNT(*) FROM configs WHERE status = 'working'").Scan(&stats.WorkingConfigs)
	_ = d.db.QueryRow("SELECT COUNT(*) FROM configs WHERE status = 'dead'").Scan(&stats.DeadConfigs)
	_ = d.db.QueryRow("SELECT COUNT(*) FROM configs WHERE is_favorite = 1").Scan(&stats.FavoriteConfigs)
	_ = d.db.QueryRow("SELECT IFNULL(AVG(latency), 0) FROM configs WHERE status = 'working' AND latency > 0").Scan(&stats.AvgLatency)
	_ = d.db.QueryRow("SELECT COUNT(*) FROM sources").Scan(&stats.TotalSources)
	_ = d.db.QueryRow("SELECT COUNT(*) FROM sources WHERE enabled = 1").Scan(&stats.ActiveSources)

	// By Country
	cRows, err := d.db.Query("SELECT country, COUNT(*) FROM configs WHERE status = 'working' GROUP BY country")
	if err == nil {
		defer cRows.Close()
		for cRows.Next() {
			var code string
			var count int
			if err := cRows.Scan(&code, &count); err == nil && code != "" {
				stats.ByCountry[code] = count
			}
		}
	}

	// By Protocol
	pRows, err := d.db.Query("SELECT protocol, COUNT(*) FROM configs WHERE status = 'working' GROUP BY protocol")
	if err == nil {
		defer pRows.Close()
		for pRows.Next() {
			var proto string
			var count int
			if err := pRows.Scan(&proto, &count); err == nil && proto != "" {
				stats.ByProtocol[proto] = count
			}
		}
	}

	return stats, nil
}

// Settings
func (d *DB) GetSetting(key, defaultVal string) string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var val string
	err := d.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	if err != nil || val == "" {
		return defaultVal
	}
	if key == "test_endpoint" && strings.Contains(val, "gstatic") {
		return "http://cp.cloudflare.com/generate_204"
	}
	return val
}

func (d *DB) SetSetting(key, val string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec("INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)", key, val)
	return err
}
