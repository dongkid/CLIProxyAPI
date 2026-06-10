package usage

import (
	"context"
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func BenchmarkRingBufferPush(b *testing.B) {
	rb := NewRingBuffer[RequestDetail](2000)
	detail := RequestDetail{
		Timestamp: time.Now(),
		LatencyMs: 150,
		Source:    "test-source",
		AuthIndex: "idx-1",
		Model:     "test-model",
		Tokens:    TokenStats{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rb.Push(detail)
	}
}

func BenchmarkRingBufferSnapshot(b *testing.B) {
	rb := NewRingBuffer[RequestDetail](2000)
	detail := RequestDetail{Timestamp: time.Now(), Model: "m"}
	for i := 0; i < 2000; i++ {
		rb.Push(detail)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rb.Snapshot()
	}
}

func BenchmarkRecord(b *testing.B) {
	s := NewRequestStatistics()
	ctx := context.Background()
	record := coreusage.Record{
		APIKey:      "test-api-key-1",
		Model:       "test-model",
		RequestedAt: time.Now(),
		Detail: coreusage.Detail{
			InputTokens:  100,
			OutputTokens: 50,
			TotalTokens:  150,
		},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Record(ctx, record)
	}
}

func BenchmarkRecordParallel(b *testing.B) {
	s := NewRequestStatistics()
	now := time.Now()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		i := 0
		for pb.Next() {
			key := "api-key-" + string(rune('a'+i%16))
			s.Record(ctx, coreusage.Record{
				APIKey:      key,
				Model:       "test-model",
				RequestedAt: now,
				Detail: coreusage.Detail{
					InputTokens:  100,
					OutputTokens: 50,
					TotalTokens:  150,
				},
			})
			i++
		}
	})
}

func BenchmarkSnapshot(b *testing.B) {
	s := NewRequestStatistics()
	ctx := context.Background()
	record := coreusage.Record{
		APIKey:      "api-key",
		Model:       "test-model",
		RequestedAt: time.Now(),
		Detail:      coreusage.Detail{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
	}
	for i := 0; i < 1000; i++ {
		s.Record(ctx, record)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.Snapshot()
	}
}

func BenchmarkMergeSnapshot(b *testing.B) {
	s := NewRequestStatistics()
	ctx := context.Background()
	now := time.Now()
	for i := 0; i < 100; i++ {
		s.Record(ctx, coreusage.Record{
			APIKey:      "api-key",
			Model:       "test-model",
			RequestedAt: now,
			Detail:      coreusage.Detail{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
		})
	}
	snap := s.Snapshot()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.MergeSnapshot(snap)
	}
}

func BenchmarkDedupKey(b *testing.B) {
	detail := RequestDetail{
		Timestamp: time.Now(),
		Source:    "test-source",
		AuthIndex: "idx-1",
		Tokens:    TokenStats{InputTokens: 100, OutputTokens: 50, ReasoningTokens: 0, CachedTokens: 0, TotalTokens: 150},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = dedupKey("api-key", "test-model", detail)
	}
}
