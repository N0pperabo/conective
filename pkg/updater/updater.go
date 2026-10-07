package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"freenode/pkg/database"
)

const (
	CurrentVersion = "1.0.0"
	DefaultRepo    = "N0pperabo/conective"
)

// ReleaseInfo holds the update check result
type ReleaseInfo struct {
	UpdateAvailable bool   `json:"update_available"`
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	ReleaseNotes    string `json:"release_notes"`
	DownloadURL     string `json:"download_url"`
	PublishedAt     string `json:"published_at,omitempty"`
	TagName         string `json:"tag_name,omitempty"`
	AssetName       string `json:"asset_name,omitempty"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type githubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	Body        string        `json:"body"`
	HTMLURL     string        `json:"html_url"`
	PublishedAt string        `json:"published_at"`
	Assets      []githubAsset `json:"assets"`
}

// Updater manages checking and applying software updates
type Updater struct {
	db             *database.DB
	currentVersion string
	httpClient     *http.Client
}

// NewUpdater creates a new Updater instance
func NewUpdater(db *database.DB, currentVersion string) *Updater {
	if currentVersion == "" {
		currentVersion = CurrentVersion
	}
	return &Updater{
		db:             db,
		currentVersion: currentVersion,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// SetHTTPClient sets custom http client (useful for unit testing)
func (u *Updater) SetHTTPClient(client *http.Client) {
	u.httpClient = client
}

// NormalizeRepo extracts owner/repo format from various URL formats
func NormalizeRepo(repo string) string {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return ""
	}
	// Strip protocols
	repo = strings.TrimPrefix(repo, "https://")
	repo = strings.TrimPrefix(repo, "http://")
	// Strip github.com/
	repo = strings.TrimPrefix(repo, "github.com/")
	repo = strings.TrimPrefix(repo, "www.github.com/")
	// Strip trailing .git and trailing slashes
	repo = strings.TrimSuffix(repo, ".git")
	repo = strings.Trim(repo, "/")

	parts := strings.Split(repo, "/")
	if len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return repo
}

// CompareSemver compares two semver strings (e.g. "v1.2.3" and "1.2.4").
// Returns 1 if v1 > v2, -1 if v1 < v2, and 0 if v1 == v2.
func CompareSemver(v1, v2 string) int {
	clean1 := strings.TrimLeft(strings.TrimSpace(v1), "vV")
	clean2 := strings.TrimLeft(strings.TrimSpace(v2), "vV")

	// Separate core semver from prerelease tag (e.g. 1.0.0-beta.1)
	core1, pre1 := splitCoreAndPre(clean1)
	core2, pre2 := splitCoreAndPre(clean2)

	parts1 := parseVersionNumbers(core1)
	parts2 := parseVersionNumbers(core2)

	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}

	for i := 0; i < maxLen; i++ {
		num1 := 0
		num2 := 0
		if i < len(parts1) {
			num1 = parts1[i]
		}
		if i < len(parts2) {
			num2 = parts2[i]
		}

		if num1 > num2 {
			return 1
		}
		if num1 < num2 {
			return -1
		}
	}

	// Core versions are identical. Check prerelease tags.
	// In semver: a version without a prerelease tag has higher precedence than one with a prerelease tag.
	if pre1 == "" && pre2 != "" {
		return 1
	}
	if pre1 != "" && pre2 == "" {
		return -1
	}
	if pre1 != "" && pre2 != "" {
		if pre1 > pre2 {
			return 1
		}
		if pre1 < pre2 {
			return -1
		}
	}

	return 0
}

func splitCoreAndPre(v string) (string, string) {
	idx := strings.Index(v, "-")
	if idx == -1 {
		return v, ""
	}
	return v[:idx], v[idx+1:]
}

func parseVersionNumbers(core string) []int {
	parts := strings.Split(core, ".")
	nums := make([]int, 0, len(parts))
	re := regexp.MustCompile(`^\d+`)
	for _, p := range parts {
		match := re.FindString(p)
		if match != "" {
			n, _ := strconv.Atoi(match)
			nums = append(nums, n)
		} else {
			nums = append(nums, 0)
		}
	}
	return nums
}

// CheckUpdate checks GitHub releases for updates
func (u *Updater) CheckUpdate(ctx context.Context) (*ReleaseInfo, error) {
	var repo string
	if u.db != nil {
		repo = u.db.GetSetting("update_repo", "")
	}
	if repo == "" {
		repo = DefaultRepo
	}

	normalized := NormalizeRepo(repo)
	if normalized == "" || !strings.Contains(normalized, "/") {
		return &ReleaseInfo{
			UpdateAvailable: false,
			CurrentVersion:  u.currentVersion,
			LatestVersion:   u.currentVersion,
			ReleaseNotes:    "Update repository not configured. Set your GitHub repository in Settings.",
		}, nil
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", normalized)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating update request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Conective-Updater/"+u.currentVersion)

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching latest release from GitHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return &ReleaseInfo{
			UpdateAvailable: false,
			CurrentVersion:  u.currentVersion,
			LatestVersion:   u.currentVersion,
			ReleaseNotes:    fmt.Sprintf("No releases found for repository %s", normalized),
		}, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("GitHub API returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("parsing GitHub release JSON: %w", err)
	}

	latestVersionClean := strings.TrimLeft(rel.TagName, "vV")
	currentVersionClean := strings.TrimLeft(u.currentVersion, "vV")

	updateAvailable := CompareSemver(latestVersionClean, currentVersionClean) > 0

	// Select best asset (prefer .exe installer for Windows, or any .exe)
	downloadURL := rel.HTMLURL
	assetName := ""
	for _, a := range rel.Assets {
		lower := strings.ToLower(a.Name)
		if strings.HasSuffix(lower, ".exe") {
			if strings.Contains(lower, "setup") || strings.Contains(lower, "conective") {
				downloadURL = a.BrowserDownloadURL
				assetName = a.Name
				break
			} else if downloadURL == rel.HTMLURL {
				downloadURL = a.BrowserDownloadURL
				assetName = a.Name
			}
		}
	}

	// Fallback to first asset if available
	if downloadURL == rel.HTMLURL && len(rel.Assets) > 0 {
		downloadURL = rel.Assets[0].BrowserDownloadURL
		assetName = rel.Assets[0].Name
	}

	return &ReleaseInfo{
		UpdateAvailable: updateAvailable,
		CurrentVersion:  u.currentVersion,
		LatestVersion:   rel.TagName,
		ReleaseNotes:    rel.Body,
		DownloadURL:     downloadURL,
		PublishedAt:     rel.PublishedAt,
		TagName:         rel.TagName,
		AssetName:       assetName,
	}, nil
}

// ApplyUpdate downloads and executes the update installer, or opens the download URL
func (u *Updater) ApplyUpdate(ctx context.Context, downloadURL string) (string, error) {
	if downloadURL == "" {
		info, err := u.CheckUpdate(ctx)
		if err != nil {
			return "", fmt.Errorf("checking update for URL: %w", err)
		}
		downloadURL = info.DownloadURL
	}

	if downloadURL == "" {
		return "", fmt.Errorf("no download URL found for update")
	}

	// If the download URL is a direct binary / installer executable
	if strings.HasSuffix(strings.ToLower(downloadURL), ".exe") {
		tempDir := os.TempDir()
		filename := filepath.Base(downloadURL)
		if filename == "" || filename == "." || filename == "/" {
			filename = "Conective-Setup.exe"
		}
		targetPath := filepath.Join(tempDir, filename)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
		if err != nil {
			return "", fmt.Errorf("creating download request: %w", err)
		}
		req.Header.Set("User-Agent", "Conective-Updater/"+u.currentVersion)

		resp, err := u.httpClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("downloading update file: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("download failed with HTTP %d", resp.StatusCode)
		}

		out, err := os.Create(targetPath)
		if err != nil {
			return "", fmt.Errorf("creating temp update file: %w", err)
		}
		_, err = io.Copy(out, resp.Body)
		out.Close()
		if err != nil {
			return "", fmt.Errorf("saving update file: %w", err)
		}

		// Launch the installer on Windows
		if runtime.GOOS == "windows" {
			cmd := exec.Command(targetPath)
			if err := cmd.Start(); err != nil {
				return fmt.Sprintf("Downloaded installer to %s, but failed to start automatically: %v", targetPath, err), nil
			}
			return fmt.Sprintf("Update downloaded to %s and installer launched successfully.", targetPath), nil
		}

		return fmt.Sprintf("Update downloaded successfully to %s", targetPath), nil
	}

	// For HTML releases or other URLs, launch default browser
	if runtime.GOOS == "windows" {
		_ = exec.Command("cmd", "/c", "start", downloadURL).Start()
		return fmt.Sprintf("Opened download page in browser: %s", downloadURL), nil
	}

	return fmt.Sprintf("Download URL: %s", downloadURL), nil
}
