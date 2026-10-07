package v2go

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"freenode/pkg/models"
)

type FetchResult struct {
	Source models.Source
	Lines  []string
	Error  error
}

type Fetcher struct {
	client *http.Client
}

func NewFetcher(timeoutSec int) *Fetcher {
	if timeoutSec <= 0 {
		timeoutSec = 15
	}
	return &Fetcher{
		client: &http.Client{
			Timeout: time.Duration(timeoutSec) * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     30 * time.Second,
			},
		},
	}
}

// FetchAll fetches all enabled sources in parallel using a worker pool and cancellation context
func (f *Fetcher) FetchAll(
	ctx context.Context,
	sources []models.Source,
	maxWorkers int,
	onSourceStart func(s models.Source),
	onSourceDone func(res FetchResult),
) []FetchResult {
	if maxWorkers <= 0 {
		maxWorkers = 8
	}

	results := make([]FetchResult, len(sources))
	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for i, s := range sources {
		select {
		case <-ctx.Done():
			results[i] = FetchResult{Source: s, Error: ctx.Err()}
			continue
		default:
		}

		wg.Add(1)
		sem <- struct{}{}

		go func(idx int, src models.Source) {
			defer wg.Done()
			defer func() { <-sem }()

			if onSourceStart != nil {
				onSourceStart(src)
			}

			lines, err := f.fetchOne(ctx, src)
			res := FetchResult{
				Source: src,
				Lines:  lines,
				Error:  err,
			}
			results[idx] = res

			if onSourceDone != nil {
				onSourceDone(res)
			}
		}(i, s)
	}

	wg.Wait()
	return results
}

// NormalizeURL converts GitHub blob/web URLs to direct raw content URLs
func NormalizeURL(u string) string {
	u = strings.TrimSpace(u)
	// GitHub blob/raw to raw.githubusercontent.com
	// e.g. https://github.com/Danialsamadi/v2go/blob/main/AllConfigsSub.txt -> https://raw.githubusercontent.com/Danialsamadi/v2go/main/AllConfigsSub.txt
	if strings.Contains(u, "github.com/") && strings.Contains(u, "/blob/") {
		u = strings.Replace(u, "github.com/", "raw.githubusercontent.com/", 1)
		u = strings.Replace(u, "/blob/", "/", 1)
	} else if strings.Contains(u, "github.com/") && strings.Contains(u, "/raw/") {
		u = strings.Replace(u, "github.com/", "raw.githubusercontent.com/", 1)
		u = strings.Replace(u, "/raw/", "/", 1)
	}
	// Pastebin: https://pastebin.com/xxxx -> https://pastebin.com/raw/xxxx
	if strings.Contains(u, "pastebin.com/") && !strings.Contains(u, "pastebin.com/raw/") {
		u = strings.Replace(u, "pastebin.com/", "pastebin.com/raw/", 1)
	}
	return u
}

func (f *Fetcher) fetchOne(ctx context.Context, src models.Source) ([]string, error) {
	fetchURL := NormalizeURL(src.URL)
	req, err := http.NewRequestWithContext(ctx, "GET", fetchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "v2rayN/6.39 (Windows NT 10.0; Win64; x64) v2go/1.3")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	content := string(body)

	// If format is base64 or doesn't contain standard scheme separators, try decoding
	if src.Format == "base64" || (!strings.Contains(content, "://") && len(content) > 10) {
		decoded, err := decodeBase64(content)
		if err == nil && strings.Contains(decoded, "://") {
			content = decoded
		}
	}

	var lines []string
	for _, l := range strings.Split(content, "\n") {
		l = strings.TrimSpace(l)
		// Fix HTML entities e.g. &amp;
		l = strings.ReplaceAll(l, "&amp;", "&")
		if l != "" && !strings.HasPrefix(l, "#") && strings.Contains(l, "://") {
			lines = append(lines, l)
		}
	}

	return lines, nil
}
