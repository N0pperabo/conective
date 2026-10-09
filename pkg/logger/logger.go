package logger

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Entry represents a single structured log line.
type Entry struct {
	ID        int64  `json:"id"`
	Timestamp string `json:"timestamp"` // HH:MM:SS format
	Source    string `json:"source"`    // "psiphon", "xray", "tun", "system", "api"
	Level     string `json:"level"`     // "info", "warn", "error", "debug"
	Message   string `json:"message"`
}

const (
	// DefaultCapacity is the maximum number of log entries stored in memory.
	DefaultCapacity = 1000
)

// Hub manages thread-safe log storage in a ring buffer, Pub/Sub broadcasting, and streaming.
type Hub struct {
	mu          sync.RWMutex
	capacity    int
	entries     []Entry
	start       int
	count       int
	idSeq       int64
	subscribers map[chan Entry]struct{}
}

// New creates a new Hub instance with the specified ring buffer capacity.
func New(capacity int) *Hub {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Hub{
		capacity:    capacity,
		entries:     make([]Entry, capacity),
		subscribers: make(map[chan Entry]struct{}),
	}
}

var (
	defaultHub = New(DefaultCapacity)
	stdLogOnce sync.Once
)

// Default returns the package-level default log hub.
func Default() *Hub {
	return defaultHub
}

// Log records a log entry with the given source, level, and formatted message.
func (h *Hub) Log(source, level, format string, args ...any) Entry {
	var msg string
	if len(args) == 0 {
		msg = format
	} else {
		msg = fmt.Sprintf(format, args...)
	}

	normLevel := strings.ToLower(strings.TrimSpace(level))
	if normLevel == "" {
		normLevel = "info"
	}
	normSource := strings.ToLower(strings.TrimSpace(source))
	if normSource == "" {
		normSource = "system"
	}

	id := atomic.AddInt64(&h.idSeq, 1)
	entry := Entry{
		ID:        id,
		Timestamp: time.Now().Format("15:04:05"),
		Source:    normSource,
		Level:     normLevel,
		Message:   msg,
	}

	h.mu.Lock()
	if h.count < h.capacity {
		h.entries[h.count] = entry
		h.count++
	} else {
		h.entries[h.start] = entry
		h.start = (h.start + 1) % h.capacity
	}

	// Non-blocking broadcast to all active subscribers
	for ch := range h.subscribers {
		select {
		case ch <- entry:
		default:
			// Non-blocking drop if subscriber buffer is full
		}
	}
	h.mu.Unlock()

	return entry
}

// Info logs an informational message.
func (h *Hub) Info(source, format string, args ...any) {
	h.Log(source, "info", format, args...)
}

// Warn logs a warning message.
func (h *Hub) Warn(source, format string, args ...any) {
	h.Log(source, "warn", format, args...)
}

// Error logs an error message.
func (h *Hub) Error(source, format string, args ...any) {
	h.Log(source, "error", format, args...)
}

// Debug logs a debug message.
func (h *Hub) Debug(source, format string, args ...any) {
	h.Log(source, "debug", format, args...)
}

// GetHistory returns a snapshot of all buffered entries in chronological order.
func (h *Hub) GetHistory() []Entry {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.count == 0 {
		return []Entry{}
	}

	res := make([]Entry, h.count)
	for i := 0; i < h.count; i++ {
		idx := (h.start + i) % h.capacity
		res[i] = h.entries[idx]
	}
	return res
}

// Clear empties all buffered log entries.
func (h *Hub) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.start = 0
	h.count = 0
	h.entries = make([]Entry, h.capacity)
}

// Subscribe returns a channel of live log entries and an unsubscribe function.
func (h *Hub) Subscribe() (ch chan Entry, unsubscribe func()) {
	ch = make(chan Entry, 256)

	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	unsubscribe = func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subscribers, ch)
			h.mu.Unlock()
		})
	}

	return ch, unsubscribe
}

// LineWriter implements io.Writer to buffer bytes, split by newline, clean \r,
// classify level, and dispatch non-empty lines to the hub.
type LineWriter struct {
	hub          *Hub
	source       string
	defaultLevel string
	mu           sync.Mutex
	buf          bytes.Buffer
}

// Writer creates an io.Writer that dispatches lines to this Hub.
func (h *Hub) Writer(source, defaultLevel string) io.Writer {
	if defaultLevel == "" {
		defaultLevel = "info"
	}
	return &LineWriter{
		hub:          h,
		source:       source,
		defaultLevel: defaultLevel,
	}
}

func (w *LineWriter) classifyLevel(line string) string {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "[error]") || strings.Contains(lower, "error") || strings.Contains(lower, "fatal") {
		return "error"
	}
	if strings.Contains(lower, "[warn]") || strings.Contains(lower, "warning") || strings.Contains(lower, "warn") {
		return "warn"
	}
	return w.defaultLevel
}

func (w *LineWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf.Write(p)
	b := w.buf.Bytes()

	for {
		idx := bytes.IndexByte(b, '\n')
		if idx == -1 {
			break
		}
		lineBytes := b[:idx]
		b = b[idx+1:]

		line := strings.ReplaceAll(string(lineBytes), "\r", "")
		line = strings.TrimSpace(line)
		if line != "" {
			level := w.classifyLevel(line)
			w.hub.Log(w.source, level, "%s", line)
		}
	}

	w.buf.Reset()
	if len(b) > 0 {
		w.buf.Write(b)
	}

	return len(p), nil
}

// Flush dispatches any remaining un-terminated line in the buffer.
func (w *LineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.buf.Len() > 0 {
		line := strings.ReplaceAll(w.buf.String(), "\r", "")
		line = strings.TrimSpace(line)
		if line != "" {
			level := w.classifyLevel(line)
			w.hub.Log(w.source, level, "%s", line)
		}
		w.buf.Reset()
	}
}

// Package-level convenience functions targeting defaultHub

// Log records a log entry on the default log hub.
func Log(source, level, format string, args ...any) Entry {
	return defaultHub.Log(source, level, format, args...)
}

// Info logs an informational message on the default log hub.
func Info(source, format string, args ...any) {
	defaultHub.Info(source, format, args...)
}

// Warn logs a warning message on the default log hub.
func Warn(source, format string, args ...any) {
	defaultHub.Warn(source, format, args...)
}

// Error logs an error message on the default log hub.
func Error(source, format string, args ...any) {
	defaultHub.Error(source, format, args...)
}

// Debug logs a debug message on the default log hub.
func Debug(source, format string, args ...any) {
	defaultHub.Debug(source, format, args...)
}

// GetHistory returns a snapshot of all buffered entries from the default hub.
func GetHistory() []Entry {
	return defaultHub.GetHistory()
}

// Clear empties all buffered log entries on the default hub.
func Clear() {
	defaultHub.Clear()
}

// Subscribe returns a subscription channel and unsubscribe func for the default hub.
func Subscribe() (ch chan Entry, unsubscribe func()) {
	return defaultHub.Subscribe()
}

// Writer returns an io.Writer connected to the default hub.
func Writer(source, defaultLevel string) io.Writer {
	return defaultHub.Writer(source, defaultLevel)
}

// InitStdLogCapture wraps os.Stderr and a logger.Writer("system", "info") via io.MultiWriter,
// ensuring all standard log.Printf calls are automatically captured and broadcast to the logs hub.
func InitStdLogCapture() {
	stdLogOnce.Do(func() {
		w := Writer("system", "info")
		mw := io.MultiWriter(os.Stderr, w)
		log.SetOutput(mw)
	})
}
