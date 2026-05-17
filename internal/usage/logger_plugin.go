// Package usage provides usage tracking and logging functionality for the CLI Proxy API server.
// It includes plugins for monitoring API usage, token consumption, and other metrics
// to help with observability and billing purposes.
package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	internallogging "github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

// shardCount governs the number of shards for distributed locking.
// 16 is chosen as a power of two that balances concurrency against memory overhead.
const shardCount = 16

var statisticsEnabled atomic.Bool

func init() {
	statisticsEnabled.Store(true)
	coreusage.RegisterPlugin(NewLoggerPlugin())
}

// LoggerPlugin collects in-memory request statistics for usage analysis.
// It implements coreusage.Plugin to receive usage records emitted by the runtime.
type LoggerPlugin struct {
	stats *RequestStatistics
}

// NewLoggerPlugin constructs a new logger plugin instance.
//
// Returns:
//   - *LoggerPlugin: A new logger plugin instance wired to the shared statistics store.
func NewLoggerPlugin() *LoggerPlugin { return &LoggerPlugin{stats: defaultRequestStatistics} }

// HandleUsage implements coreusage.Plugin.
// It updates the in-memory statistics store whenever a usage record is received.
//
// Parameters:
//   - ctx: The context for the usage record
//   - record: The usage record to aggregate
func (p *LoggerPlugin) HandleUsage(ctx context.Context, record coreusage.Record) {
	if !statisticsEnabled.Load() {
		return
	}
	if p == nil || p.stats == nil {
		return
	}
	p.stats.Record(ctx, record)
}

// SetStatisticsEnabled toggles whether in-memory statistics are recorded.
func SetStatisticsEnabled(enabled bool) { statisticsEnabled.Store(enabled) }

// StatisticsEnabled reports the current recording state.
func StatisticsEnabled() bool { return statisticsEnabled.Load() }

// RequestStatistics maintains aggregated request metrics in memory.
// Global counters use atomic.Int64 to avoid lock contention.
// Per-API data is distributed across 16 shards, each with its own mutex,
// so that concurrent requests targeting different API keys can proceed in parallel.
type RequestStatistics struct {
	totalRequests atomic.Int64
	successCount  atomic.Int64
	failureCount  atomic.Int64
	totalTokens   atomic.Int64
	shards        [shardCount]*statsShard
}

// statsShard holds a partition of the per-API statistics.
type statsShard struct {
	mu             sync.Mutex
	apis           map[string]*apiStats
	requestsByDay  map[string]int64
	tokensByDay    map[string]int64
	requestsByHour [24]int64
	tokensByHour   [24]int64
}

// apiStats holds aggregated metrics for a single API key.
type apiStats struct {
	TotalRequests int64
	TotalTokens   int64
	Models        map[string]*modelStats
}

// modelStats holds aggregated metrics for a specific model within an API.
type modelStats struct {
	TotalRequests int64
	TotalTokens   int64
	Details       *RingBuffer[RequestDetail]
}

// MaxDetailsPerModel limits the number of stored request details per model
// to prevent unbounded memory growth. Once exceeded, the oldest entries are
// overwritten by the ring buffer. Set via SetMaxDetailsPerModel; defaults to 2000.
var MaxDetailsPerModel = 2000

// SetMaxDetailsPerModel updates the global detail retention limit. A value <= 0
// resets to the default of 2000.
func SetMaxDetailsPerModel(n int) {
	if n <= 0 {
		n = 2000
	}
	MaxDetailsPerModel = n
}

// RequestDetail stores the timestamp, latency, and token usage for a single request.
type RequestDetail struct {
	Timestamp time.Time  `json:"timestamp"`
	LatencyMs int64      `json:"latency_ms"`
	Source    string     `json:"source"`
	AuthIndex string     `json:"auth_index"`
	Model     string     `json:"model"`
	Alias     string     `json:"alias,omitempty"`
	Tokens    TokenStats `json:"tokens"`
	Failed    bool       `json:"failed"`
	Fail      FailDetail `json:"fail,omitempty"`
}

// FailDetail captures HTTP failure metadata for a failed upstream attempt.
type FailDetail struct {
	StatusCode int    `json:"status_code,omitempty"`
	Body       string `json:"body,omitempty"`
}

// TokenStats captures the token usage breakdown for a request.
type TokenStats struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	ReasoningTokens     int64 `json:"reasoning_tokens"`
	CachedTokens        int64 `json:"cached_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int64 `json:"cache_creation_tokens,omitempty"`
	TotalTokens         int64 `json:"total_tokens"`
}

// statsVersion is embedded in saved snapshots for forward compatibility.
const statsVersion = 1

// StatisticsSnapshot represents an immutable view of the aggregated metrics.
type StatisticsSnapshot struct {
	Version int `json:"version"`

	TotalRequests int64 `json:"total_requests"`
	SuccessCount  int64 `json:"success_count"`
	FailureCount  int64 `json:"failure_count"`
	TotalTokens   int64 `json:"total_tokens"`

	APIs map[string]APISnapshot `json:"apis"`

	RequestsByDay  map[string]int64 `json:"requests_by_day"`
	RequestsByHour map[string]int64 `json:"requests_by_hour"`
	TokensByDay    map[string]int64 `json:"tokens_by_day"`
	TokensByHour   map[string]int64 `json:"tokens_by_hour"`
}

// APISnapshot summarises metrics for a single API key.
type APISnapshot struct {
	TotalRequests int64                    `json:"total_requests"`
	TotalTokens   int64                    `json:"total_tokens"`
	Models        map[string]ModelSnapshot `json:"models"`
}

// ModelSnapshot summarises metrics for a specific model.
type ModelSnapshot struct {
	TotalRequests int64           `json:"total_requests"`
	TotalTokens   int64           `json:"total_tokens"`
	Details       []RequestDetail `json:"details"`
}

var defaultRequestStatistics = NewRequestStatistics()

// GetRequestStatistics returns the shared statistics store.
func GetRequestStatistics() *RequestStatistics { return defaultRequestStatistics }

// NewRequestStatistics constructs an empty statistics store.
func NewRequestStatistics() *RequestStatistics {
	s := &RequestStatistics{}
	for i := range s.shards {
		s.shards[i] = &statsShard{
			apis:          make(map[string]*apiStats),
			requestsByDay: make(map[string]int64),
			tokensByDay:   make(map[string]int64),
		}
	}
	return s
}

// shardIndex returns the shard for a given API key. Uses FNV-1a hashing.
func shardIndex(key string) int {
	var h uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return int(h % shardCount)
}

// Record ingests a new usage record and updates the aggregates.
func (s *RequestStatistics) Record(ctx context.Context, record coreusage.Record) {
	if s == nil {
		return
	}
	if !statisticsEnabled.Load() {
		return
	}
	timestamp := record.RequestedAt
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	detail := normaliseDetail(record.Detail)
	totalTokens := detail.TotalTokens
	statsKey := record.APIKey
	if statsKey == "" {
		statsKey = resolveAPIIdentifier(ctx, record)
	}
	failed := record.Failed
	if !failed {
		failed = !resolveSuccess(ctx)
	}
	modelName := record.Model
	if modelName == "" {
		modelName = "unknown"
	}
	alias := record.Alias
	if alias == "" {
		alias = record.Model
	}
	failInfo := FailDetail{}
	if record.Fail.StatusCode != 0 || record.Fail.Body != "" {
		failInfo = FailDetail{
			StatusCode: record.Fail.StatusCode,
			Body:       record.Fail.Body,
		}
	}
	dayKey := timestamp.Format("2006-01-02")
	hourKey := timestamp.Hour()

	s.totalRequests.Add(1)
	if !failed {
		s.successCount.Add(1)
	} else {
		s.failureCount.Add(1)
	}
	s.totalTokens.Add(totalTokens)

	sh := s.shards[shardIndex(statsKey)]
	sh.mu.Lock()
	sh.requestsByDay[dayKey]++
	sh.requestsByHour[hourKey]++
	sh.tokensByDay[dayKey] += totalTokens
	sh.tokensByHour[hourKey] += totalTokens

	stats, ok := sh.apis[statsKey]
	if !ok {
		stats = &apiStats{Models: make(map[string]*modelStats)}
		sh.apis[statsKey] = stats
	}
	s.updateAPIStats(stats, modelName, RequestDetail{
		Timestamp: timestamp,
		LatencyMs: normaliseLatency(record.Latency),
		Source:    record.Source,
		AuthIndex: record.AuthIndex,
		Model:     modelName,
		Alias:     alias,
		Tokens:    detail,
		Failed:    failed,
		Fail:      failInfo,
	})
	sh.mu.Unlock()
}

func (s *RequestStatistics) updateAPIStats(stats *apiStats, model string, detail RequestDetail) {
	stats.TotalRequests++
	stats.TotalTokens += detail.Tokens.TotalTokens
	modelStatsValue, ok := stats.Models[model]
	if !ok {
		modelStatsValue = &modelStats{Details: NewRingBuffer[RequestDetail](MaxDetailsPerModel)}
		stats.Models[model] = modelStatsValue
	}
	modelStatsValue.TotalRequests++
	modelStatsValue.TotalTokens += detail.Tokens.TotalTokens
	modelStatsValue.Details.Push(detail)
}

// Snapshot returns a copy of the aggregated metrics for external consumption.
//
// The snapshot iterates shards sequentially without a global lock.
// This means the returned data is a "fuzzy" point-in-time: each shard reflects
// a slightly different instant. For usage statistics this is an acceptable
// tradeoff — global totals (via atomic counters) remain internally consistent
// and the sub-millisecond discrepancy between shards has no practical impact.
func (s *RequestStatistics) Snapshot() StatisticsSnapshot {
	result := StatisticsSnapshot{Version: statsVersion}
	if s == nil {
		return result
	}

	result.TotalRequests = s.totalRequests.Load()
	result.SuccessCount = s.successCount.Load()
	result.FailureCount = s.failureCount.Load()
	result.TotalTokens = s.totalTokens.Load()

	result.APIs = make(map[string]APISnapshot)
	result.RequestsByDay = make(map[string]int64)
	result.RequestsByHour = make(map[string]int64)
	result.TokensByDay = make(map[string]int64)
	result.TokensByHour = make(map[string]int64)

	for _, sh := range s.shards {
		sh.mu.Lock()
		for apiName, stats := range sh.apis {
			apiSnapshot, exists := result.APIs[apiName]
			if !exists {
				apiSnapshot = APISnapshot{Models: make(map[string]ModelSnapshot)}
			}
			apiSnapshot.TotalRequests += stats.TotalRequests
			apiSnapshot.TotalTokens += stats.TotalTokens
			for modelName, ms := range stats.Models {
				existingModel, hasModel := apiSnapshot.Models[modelName]
				if !hasModel {
					existingModel = ModelSnapshot{}
				}
				snap := ms.Details.Snapshot()
				existingModel.TotalRequests += ms.TotalRequests
				existingModel.TotalTokens += ms.TotalTokens
				existingModel.Details = append(existingModel.Details, snap...)
				apiSnapshot.Models[modelName] = existingModel
			}
			result.APIs[apiName] = apiSnapshot
		}
		for k, v := range sh.requestsByDay {
			result.RequestsByDay[k] += v
		}
		for hour, v := range sh.requestsByHour {
			result.RequestsByHour[formatHour(hour)] += v
		}
		for k, v := range sh.tokensByDay {
			result.TokensByDay[k] += v
		}
		for hour, v := range sh.tokensByHour {
			result.TokensByHour[formatHour(hour)] += v
		}
		sh.mu.Unlock()
	}

	return result
}

type MergeResult struct {
	Added   int64 `json:"added"`
	Skipped int64 `json:"skipped"`
}

// MergeSnapshot merges an exported statistics snapshot into the current store.
// Existing data is preserved and duplicate request details are skipped.
// It uses a two-phase approach:
// Phase 1 — scan all shards to build a cross-shard dedup set.
// Phase 2 — for each entry in the snapshot, hash-route to the correct shard and merge.
func (s *RequestStatistics) MergeSnapshot(snapshot StatisticsSnapshot) MergeResult {
	result := MergeResult{}
	if s == nil {
		return result
	}

	// Phase 1: collect dedup keys from ALL shards.
	seen := make(map[string]struct{})
	for _, sh := range s.shards {
		sh.mu.Lock()
		for apiName, stats := range sh.apis {
			if stats == nil {
				continue
			}
			for modelName, ms := range stats.Models {
				if ms == nil {
					continue
				}
				for _, detail := range ms.Details.Snapshot() {
					seen[dedupKey(apiName, modelName, detail)] = struct{}{}
				}
			}
		}
		sh.mu.Unlock()
	}

	// Phase 2: merge each entry into its target shard.
	for apiName, apiSnapshot := range snapshot.APIs {
		apiName = strings.TrimSpace(apiName)
		if apiName == "" {
			continue
		}
		sh := s.shards[shardIndex(apiName)]
		sh.mu.Lock()
		stats, ok := sh.apis[apiName]
		if !ok || stats == nil {
			stats = &apiStats{Models: make(map[string]*modelStats)}
			sh.apis[apiName] = stats
		} else if stats.Models == nil {
			stats.Models = make(map[string]*modelStats)
		}
		for modelName, modelSnapshot := range apiSnapshot.Models {
			modelName = strings.TrimSpace(modelName)
			if modelName == "" {
				modelName = "unknown"
			}
			for _, detail := range modelSnapshot.Details {
				detail.Tokens = normaliseTokenStats(detail.Tokens)
				if detail.LatencyMs < 0 {
					detail.LatencyMs = 0
				}
				if detail.Timestamp.IsZero() {
					detail.Timestamp = time.Now()
				}
				key := dedupKey(apiName, modelName, detail)
				if _, exists := seen[key]; exists {
					result.Skipped++
					continue
				}
				seen[key] = struct{}{}
				s.recordImportedLocked(apiName, modelName, stats, detail, sh)
				result.Added++
			}
		}
		sh.mu.Unlock()
	}

	return result
}

// recordImportedLocked inserts an imported detail. Caller must hold sh.mu.
func (s *RequestStatistics) recordImportedLocked(apiName, modelName string, stats *apiStats, detail RequestDetail, sh *statsShard) {
	totalTokens := detail.Tokens.TotalTokens
	if totalTokens < 0 {
		totalTokens = 0
	}

	s.totalRequests.Add(1)
	if detail.Failed {
		s.failureCount.Add(1)
	} else {
		s.successCount.Add(1)
	}
	s.totalTokens.Add(totalTokens)

	s.updateAPIStats(stats, modelName, detail)

	dayKey := detail.Timestamp.Format("2006-01-02")
	hourKey := detail.Timestamp.Hour()

	sh.requestsByDay[dayKey]++
	sh.requestsByHour[hourKey]++
	sh.tokensByDay[dayKey] += totalTokens
	sh.tokensByHour[hourKey] += totalTokens
}

func dedupKey(apiName, modelName string, detail RequestDetail) string {
	timestamp := detail.Timestamp.UTC().Format(time.RFC3339Nano)
	tokens := normaliseTokenStats(detail.Tokens)

	// Pre-size: apiName + modelName + timestamp + source + authIndex + 1(failed) + 5*20(tokens) + 11 separators
	est := len(apiName) + len(modelName) + len(timestamp) + len(detail.Source) + len(detail.AuthIndex) + 120
	var b strings.Builder
	b.Grow(est)
	b.WriteString(apiName)
	b.WriteByte('|')
	b.WriteString(modelName)
	b.WriteByte('|')
	b.WriteString(timestamp)
	b.WriteByte('|')
	b.WriteString(detail.Source)
	b.WriteByte('|')
	b.WriteString(detail.AuthIndex)
	b.WriteByte('|')
	if detail.Failed {
		b.WriteString("true")
	} else {
		b.WriteString("false")
	}
	b.WriteByte('|')
	b.WriteString(strconv.FormatInt(tokens.InputTokens, 10))
	b.WriteByte('|')
	b.WriteString(strconv.FormatInt(tokens.OutputTokens, 10))
	b.WriteByte('|')
	b.WriteString(strconv.FormatInt(tokens.ReasoningTokens, 10))
	b.WriteByte('|')
	b.WriteString(strconv.FormatInt(tokens.CachedTokens, 10))
	b.WriteByte('|')
	b.WriteString(strconv.FormatInt(tokens.TotalTokens, 10))
	return b.String()
}

func resolveAPIIdentifier(ctx context.Context, record coreusage.Record) string {
	if ctx != nil {
		if endpoint := strings.TrimSpace(internallogging.GetEndpoint(ctx)); endpoint != "" {
			return endpoint
		}
	}
	if record.Provider != "" {
		return record.Provider
	}
	return "unknown"
}

func resolveSuccess(ctx context.Context) bool {
	status := internallogging.GetResponseStatus(ctx)
	if status == 0 {
		return true
	}
	return status < httpStatusBadRequest
}

const httpStatusBadRequest = 400

func normaliseDetail(detail coreusage.Detail) TokenStats {
	tokens := TokenStats{
		InputTokens:         detail.InputTokens,
		OutputTokens:        detail.OutputTokens,
		ReasoningTokens:     detail.ReasoningTokens,
		CachedTokens:        detail.CachedTokens,
		CacheReadTokens:     detail.CacheReadTokens,
		CacheCreationTokens: detail.CacheCreationTokens,
		TotalTokens:         detail.TotalTokens,
	}
	if tokens.TotalTokens == 0 {
		tokens.TotalTokens = detail.InputTokens + detail.OutputTokens + detail.ReasoningTokens + detail.CachedTokens
	}
	return tokens
}

func normaliseTokenStats(tokens TokenStats) TokenStats {
	if tokens.TotalTokens == 0 {
		tokens.TotalTokens = tokens.InputTokens + tokens.OutputTokens + tokens.ReasoningTokens + tokens.CachedTokens
	}
	if tokens.CacheReadTokens == 0 {
		tokens.CacheReadTokens = tokens.CachedTokens
	}
	return tokens
}

func normaliseLatency(latency time.Duration) int64 {
	if latency <= 0 {
		return 0
	}
	return latency.Milliseconds()
}

func formatHour(hour int) string {
	if hour < 0 {
		hour = 0
	}
	hour = hour % 24
	return fmt.Sprintf("%02d", hour)
}

// SaveToFile serialises the current statistics snapshot as JSON and writes it to path.
// An empty path is a no-op. The file is written atomically via a temp file + rename.
func (s *RequestStatistics) SaveToFile(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	snapshot := s.Snapshot()
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("usage: marshal snapshot: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("usage: create stats dir %s: %w", dir, err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("usage: write stats temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("usage: rename stats file: %w", err)
	}
	log.Debugf("usage: statistics saved to %s (%d bytes)", path, len(data))
	return nil
}

// LoadFromFile reads a previously saved statistics snapshot and merges it into
// the current store. Missing files are silently ignored.
func (s *RequestStatistics) LoadFromFile(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("usage: read stats file: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	var snapshot StatisticsSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("usage: unmarshal stats: %w", err)
	}
	result := s.MergeSnapshot(snapshot)
	log.Infof("usage: loaded statistics from %s (added %d, skipped %d)", path, result.Added, result.Skipped)
	return nil
}

// DefaultStatsSavePath returns the conventional path for the usage statistics file.
// The file is placed in a "data" directory alongside the auth directory to avoid
// being picked up by the auth file watcher, which treats every .json inside the
// auth directory as a credential file.
func DefaultStatsSavePath(authDir string) string {
	dir := strings.TrimSpace(authDir)
	if dir == "" || dir == "." {
		return "usage_stats.json"
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	return filepath.Join(filepath.Dir(abs), "data", "usage_stats.json")
}

// AutoSaveInterval is the default interval for periodic statistics persistence.
// Set via SetAutoSaveInterval; defaults to 5 minutes.
var AutoSaveInterval = 5 * time.Minute

// SetAutoSaveInterval updates the global auto-save interval. An interval <= 0
// resets to the default of 5 minutes.
func SetAutoSaveInterval(d time.Duration) {
	if d <= 0 {
		d = 5 * time.Minute
	}
	AutoSaveInterval = d
}

// StartAutoSave launches a background goroutine that periodically persists the
// default statistics store to path. The goroutine exits when ctx is cancelled.
func StartAutoSave(ctx context.Context, path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	stats := GetRequestStatistics()
	go func() {
		ticker := time.NewTicker(AutoSaveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				if err := stats.SaveToFile(path); err != nil {
					log.Errorf("usage: final auto-save failed: %v", err)
				}
				return
			case <-ticker.C:
				if err := stats.SaveToFile(path); err != nil {
					log.Errorf("usage: auto-save failed: %v", err)
				}
			}
		}
	}()
}
