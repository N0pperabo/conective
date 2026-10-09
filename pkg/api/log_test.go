package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"freenode/pkg/logger"
)

func TestLogsEndpoint(t *testing.T) {
	logger.Clear()

	logger.Info("api", "test info entry")
	logger.Warn("xray", "test warn entry")
	logger.Error("tun", "test error entry")

	s := &Server{
		mux: http.NewServeMux(),
	}
	s.registerRoutes()

	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var entries []logger.Entry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}

	if len(entries) < 3 {
		t.Fatalf("expected at least 3 entries, got %d", len(entries))
	}

	lastThree := entries[len(entries)-3:]
	if lastThree[0].Source != "api" || lastThree[0].Message != "test info entry" {
		t.Errorf("entry 0 mismatch: %+v", lastThree[0])
	}
	if lastThree[1].Source != "xray" || lastThree[1].Level != "warn" {
		t.Errorf("entry 1 mismatch: %+v", lastThree[1])
	}
	if lastThree[2].Source != "tun" || lastThree[2].Level != "error" {
		t.Errorf("entry 2 mismatch: %+v", lastThree[2])
	}
}

func TestLogsClearEndpoint(t *testing.T) {
	logger.Clear()
	logger.Info("system", "pre-clear entry")

	s := &Server{
		mux: http.NewServeMux(),
	}
	s.registerRoutes()

	// Clear logs
	clearReq := httptest.NewRequest(http.MethodPost, "/api/logs/clear", nil)
	clearRec := httptest.NewRecorder()
	s.mux.ServeHTTP(clearRec, clearReq)

	if clearRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on clear, got %d", clearRec.Code)
	}

	var clearResp map[string]any
	if err := json.Unmarshal(clearRec.Body.Bytes(), &clearResp); err != nil {
		t.Fatalf("failed to decode clear JSON: %v", err)
	}
	if clearResp["success"] != true {
		t.Fatalf("expected success: true, got %+v", clearResp)
	}

	// Verify history is empty
	getReq := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	getRec := httptest.NewRecorder()
	s.mux.ServeHTTP(getRec, getReq)

	var entries []logger.Entry
	if err := json.Unmarshal(getRec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("failed to decode logs JSON: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries after clear, got %d", len(entries))
	}
}

func TestLogsStreamSSE(t *testing.T) {
	logger.Clear()
	logger.Info("system", "backlog message")

	s := &Server{
		mux: http.NewServeMux(),
	}
	s.registerRoutes()

	ts := httptest.NewServer(s.mux)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/logs/stream", nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("performing request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("expected text/event-stream Content-Type, got %s", contentType)
	}

	reader := bufio.NewReader(resp.Body)

	// Read backlog event
	var backlogData string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("reading SSE stream backlog: %v", err)
		}
		if strings.HasPrefix(line, "data: ") {
			backlogData = strings.TrimPrefix(strings.TrimSpace(line), "data: ")
			break
		}
	}

	var backlogEntry logger.Entry
	if err := json.Unmarshal([]byte(backlogData), &backlogEntry); err != nil {
		t.Fatalf("parsing backlog entry: %v", err)
	}
	if backlogEntry.Message != "backlog message" {
		t.Fatalf("expected 'backlog message', got '%s'", backlogEntry.Message)
	}

	// Trigger a live log event
	go func() {
		time.Sleep(50 * time.Millisecond)
		logger.Info("psiphon", "live streamed event")
	}()

	// Read streamed event
	var liveData string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("reading SSE stream live: %v", err)
		}
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(strings.TrimSpace(line), "data: ")
			var e logger.Entry
			if err := json.Unmarshal([]byte(data), &e); err == nil && e.Message == "live streamed event" {
				liveData = data
				break
			}
		}
	}

	var liveEntry logger.Entry
	if err := json.Unmarshal([]byte(liveData), &liveEntry); err != nil {
		t.Fatalf("parsing live entry: %v", err)
	}
	if liveEntry.Source != "psiphon" || liveEntry.Message != "live streamed event" {
		t.Fatalf("unexpected live entry: %+v", liveEntry)
	}
}
