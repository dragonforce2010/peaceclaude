package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"peaceclaude/internal/config"
)

const (
	// DefaultRetentionDays enforces 30-day rolling cleanup on persisted audit logs.
	DefaultRetentionDays = 30
	DefaultRetention     = DefaultRetentionDays * 24 * time.Hour
	// DefaultTextLogMaxBytes caps auxiliary daemon/launcher logs to avoid unbounded disk growth.
	DefaultTextLogMaxBytes int64 = 10 << 20
)

type LeakEvent struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Kind      string    `json:"kind"` // split_ip_warning | direct_corporate | blocked_failclosed | blocked_telemetry | host_direct_socket
	Host      string    `json:"host"`
	Port      string    `json:"port,omitempty"`
	Process   string    `json:"process,omitempty"`
	Action    string    `json:"action"`
	Reason    string    `json:"reason"`
}

type CaptureRecord struct {
	ID             string            `json:"id"`
	Timestamp      time.Time         `json:"timestamp"`
	Protocol       string            `json:"protocol"` // HTTP | CONNECT | SOCKS5
	Method         string            `json:"method,omitempty"`
	Host           string            `json:"host"`
	Port           string            `json:"port,omitempty"`
	Path           string            `json:"path,omitempty"`
	Category       string            `json:"category"`
	Action         string            `json:"action"`
	EgressIP       string            `json:"egress_ip,omitempty"`
	StatusCode     int               `json:"status_code,omitempty"`
	BytesIn        int64             `json:"bytes_in"`
	BytesOut       int64             `json:"bytes_out"`
	DurationMs     int64             `json:"duration_ms"`
	Error          string            `json:"error,omitempty"`
	RequestHeaders map[string]string `json:"request_headers,omitempty"`
}

type Stats struct {
	TotalRequests      int64 `json:"total_requests"`
	ProxiedRequests    int64 `json:"proxied_requests"`
	BlockedFailClosed  int64 `json:"blocked_failclosed"`
	BlockedTelemetry   int64 `json:"blocked_telemetry"`
	BlockedStrictSplit int64 `json:"blocked_strict_split"`
	SplitIPWarnings    int64 `json:"split_ip_warnings"`
	DirectCorporate    int64 `json:"direct_corporate"`
	DirectLocal        int64 `json:"direct_local"`
	CaptureEnabled     bool  `json:"capture_enabled"`
	CaptureCount       int   `json:"capture_count"`
	LeakCount          int   `json:"leak_count"`
}

type Store struct {
	mu             sync.RWMutex
	captureEnabled bool
	maxCapture     int
	maxLeaks       int
	captures       []CaptureRecord
	leaks          []LeakEvent
	seq            atomic.Uint64

	totalReq           atomic.Int64
	proxiedReq         atomic.Int64
	blockedFailClosed  atomic.Int64
	blockedTelemetry   atomic.Int64
	blockedStrictSplit atomic.Int64
	splitWarnings      atomic.Int64
	directCorp         atomic.Int64
	directLocal        atomic.Int64
}

func NewStore(captureEnabled bool, maxCapture int) *Store {
	if maxCapture <= 0 {
		maxCapture = 500
	}
	s := &Store{
		captureEnabled: captureEnabled,
		maxCapture:     maxCapture,
		maxLeaks:       1000,
	}
	_, _ = s.PruneOlderThan(DefaultRetention)
	s.loadTail()
	return s
}

// PruneOlderThan removes JSONL entries older than maxAge on disk and caps auxiliary log files.
func (s *Store) PruneOlderThan(maxAge time.Duration) (removedLeaks, removedCaptures int) {
	if maxAge <= 0 {
		maxAge = DefaultRetention
	}
	cutoff := time.Now().UTC().Add(-maxAge)

	s.mu.Lock()
	removedLeaks = pruneJSONLFile[LeakEvent](config.LeaksLogPath(), cutoff, func(ev LeakEvent) time.Time { return ev.Timestamp })
	removedCaptures = pruneJSONLFile[CaptureRecord](config.CaptureLogPath(), cutoff, func(r CaptureRecord) time.Time { return r.Timestamp })
	if removedLeaks > 0 {
		kept := s.leaks[:0]
		for _, ev := range s.leaks {
			if !ev.Timestamp.IsZero() && !ev.Timestamp.Before(cutoff) {
				kept = append(kept, ev)
			}
		}
		s.leaks = kept
	}
	if removedCaptures > 0 {
		kept := s.captures[:0]
		for _, rec := range s.captures {
			if !rec.Timestamp.IsZero() && !rec.Timestamp.Before(cutoff) {
				kept = append(kept, rec)
			}
		}
		s.captures = kept
	}
	s.mu.Unlock()

	RotateTextLogs(DefaultTextLogMaxBytes)
	return removedLeaks, removedCaptures
}

func pruneJSONLFile[T any](path string, cutoff time.Time, tsFn func(T) time.Time) int {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return 0
	}
	lines := strings.Split(string(raw), "\n")
	var keptLines []string
	removed := 0
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		var item T
		if err := json.Unmarshal([]byte(trim), &item); err != nil {
			continue
		}
		ts := tsFn(item)
		if !ts.IsZero() && ts.Before(cutoff) {
			removed++
			continue
		}
		keptLines = append(keptLines, trim)
	}
	if removed == 0 {
		return 0
	}
	var out []byte
	if len(keptLines) > 0 {
		out = []byte(strings.Join(keptLines, "\n") + "\n")
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, out, 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
	return removed
}

// RotateTextLogs keeps daemon and launcher text logs compact (tail-truncated above maxBytes).
func RotateTextLogs(maxBytes int64) {
	if maxBytes <= 0 {
		maxBytes = DefaultTextLogMaxBytes
	}
	keepTail := maxBytes / 5
	if keepTail < 512*1024 {
		keepTail = 512 * 1024
	}
	for _, p := range []string{
		filepath.Join(config.LogsDir(), "daemon.log"),
		filepath.Join(config.LogsDir(), "daemon.err.log"),
		config.AppLauncherLog(),
	} {
		fi, err := os.Stat(p)
		if err != nil || fi.Size() <= maxBytes {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		if _, err := f.Seek(-keepTail, io.SeekEnd); err != nil {
			_ = f.Close()
			continue
		}
		tail, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			continue
		}
		if idx := strings.IndexByte(string(tail), '\n'); idx >= 0 && idx+1 < len(tail) {
			tail = tail[idx+1:]
		}
		tmp := p + ".tmp"
		if os.WriteFile(tmp, tail, 0o600) == nil {
			_ = os.Rename(tmp, p)
		}
	}
}

func (s *Store) loadTail() {
	s.leaks = readJSONLTail[LeakEvent](config.LeaksLogPath(), s.maxLeaks)
	s.captures = readJSONLTail[CaptureRecord](config.CaptureLogPath(), s.maxCapture)
}

func readJSONLTail[T any](path string, limit int) []T {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var items []T
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var item T
		if err := json.Unmarshal([]byte(line), &item); err == nil {
			items = append(items, item)
		}
	}
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items
}

func (s *Store) nextID(prefix string) string {
	n := s.seq.Add(1)
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixMilli(), n)
}

func (s *Store) SetCaptureEnabled(enabled bool) {
	s.mu.Lock()
	s.captureEnabled = enabled
	s.mu.Unlock()
}

func (s *Store) CaptureEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.captureEnabled
}

func (s *Store) CountDecision(action string, splitWarn bool) {
	s.totalReq.Add(1)
	switch action {
	case "proxy":
		s.proxiedReq.Add(1)
	case "block_failclosed":
		s.blockedFailClosed.Add(1)
	case "block_telemetry":
		s.blockedTelemetry.Add(1)
	case "block_strict_split":
		s.blockedStrictSplit.Add(1)
	case "direct_corporate":
		s.directCorp.Add(1)
	case "direct_local":
		s.directLocal.Add(1)
	}
	if splitWarn {
		s.splitWarnings.Add(1)
	}
}

func (s *Store) RecordLeak(ev LeakEvent) {
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	if ev.ID == "" {
		ev.ID = s.nextID("lk")
	}
	s.mu.Lock()
	s.leaks = append(s.leaks, ev)
	if len(s.leaks) > s.maxLeaks {
		s.leaks = s.leaks[len(s.leaks)-s.maxLeaks:]
	}
	s.mu.Unlock()
	appendJSONL(config.LeaksLogPath(), ev)
}

func (s *Store) RecordCapture(rec CaptureRecord) {
	if !s.CaptureEnabled() {
		return
	}
	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now().UTC()
	}
	if rec.ID == "" {
		rec.ID = s.nextID("cap")
	}
	rec.Path = SanitizePath(rec.Path)
	s.mu.Lock()
	s.captures = append(s.captures, rec)
	if len(s.captures) > s.maxCapture {
		s.captures = s.captures[len(s.captures)-s.maxCapture:]
	}
	s.mu.Unlock()
	appendJSONL(config.CaptureLogPath(), rec)
}

func (s *Store) ListLeaks(since time.Duration, limit int) []LeakEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var cutoff time.Time
	if since > 0 {
		cutoff = time.Now().UTC().Add(-since)
	}
	out := make([]LeakEvent, 0, len(s.leaks))
	for i := len(s.leaks) - 1; i >= 0; i-- {
		ev := s.leaks[i]
		if !cutoff.IsZero() && ev.Timestamp.Before(cutoff) {
			continue
		}
		out = append(out, ev)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func (s *Store) ListCaptures(limit int) []CaptureRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > len(s.captures) {
		limit = len(s.captures)
	}
	out := make([]CaptureRecord, 0, limit)
	for i := len(s.captures) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.captures[i])
	}
	return out
}

func (s *Store) GetCapture(id string) (CaptureRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := len(s.captures) - 1; i >= 0; i-- {
		if s.captures[i].ID == id {
			return s.captures[i], true
		}
	}
	return CaptureRecord{}, false
}

func (s *Store) ClearCaptures() {
	s.mu.Lock()
	s.captures = nil
	s.mu.Unlock()
	_ = os.WriteFile(config.CaptureLogPath(), nil, 0o600)
}

func (s *Store) ClearLeaks() {
	s.mu.Lock()
	s.leaks = nil
	s.mu.Unlock()
	_ = os.WriteFile(config.LeaksLogPath(), nil, 0o600)
}

func (s *Store) Stats() Stats {
	s.mu.RLock()
	capEnabled := s.captureEnabled
	capCount := len(s.captures)
	leakCount := len(s.leaks)
	s.mu.RUnlock()
	return Stats{
		TotalRequests:      s.totalReq.Load(),
		ProxiedRequests:    s.proxiedReq.Load(),
		BlockedFailClosed:  s.blockedFailClosed.Load(),
		BlockedTelemetry:   s.blockedTelemetry.Load(),
		BlockedStrictSplit: s.blockedStrictSplit.Load(),
		SplitIPWarnings:    s.splitWarnings.Load(),
		DirectCorporate:    s.directCorp.Load(),
		DirectLocal:        s.directLocal.Load(),
		CaptureEnabled:     capEnabled,
		CaptureCount:       capCount,
		LeakCount:          leakCount,
	}
}

func appendJSONL(path string, v any) {
	_ = config.EnsureDirs()
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

var sensitiveHeaderNeedles = []string{
	"authorization",
	"cookie",
	"api-key",
	"apikey",
	"token",
	"secret",
	"password",
	"session",
	"credential",
}

const RedactedToken = "[REDACTED]"

func SanitizeHeaders(h http.Header) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, vals := range h {
		lk := strings.ToLower(k)
		redacted := false
		for _, needle := range sensitiveHeaderNeedles {
			if strings.Contains(lk, needle) {
				redacted = true
				break
			}
		}
		if redacted {
			out[k] = RedactedToken
		} else {
			v := strings.Join(vals, ", ")
			if len(v) > 256 {
				v = v[:256] + "..."
			}
			out[k] = v
		}
	}
	return out
}

var sensitiveQueryKeys = []string{
	"key", "token", "code", "secret", "password", "pass", "state", "auth", "session", "sig",
}

func SanitizePath(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.RawQuery == "" {
		return u.Path
	}
	q := u.Query()
	for k := range q {
		lk := strings.ToLower(k)
		for _, bad := range sensitiveQueryKeys {
			if strings.Contains(lk, bad) {
				q.Set(k, RedactedToken)
				break
			}
		}
	}
	encoded := strings.ReplaceAll(q.Encode(), "%5BREDACTED%5D", RedactedToken)
	return u.Path + "?" + encoded
}
