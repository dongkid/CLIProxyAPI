package helps

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
)

func TestRecordAPIResponseMetadataStoresHeadersWhenRequestLogDisabled(t *testing.T) {
	ctx := logging.WithResponseHeadersHolder(context.Background())
	headers := http.Header{}
	headers.Add("X-Upstream-Request-Id", "upstream-req-1")

	RecordAPIResponseMetadata(ctx, &config.Config{}, http.StatusOK, headers)
	headers.Set("X-Upstream-Request-Id", "mutated")

	got := logging.GetResponseHeaders(ctx)
	if got.Get("X-Upstream-Request-Id") != "upstream-req-1" {
		t.Fatalf("response header = %q, want %q", got.Get("X-Upstream-Request-Id"), "upstream-req-1")
	}
}

func TestFormatCPADelta(t *testing.T) {
	before := map[string]int{"system": 12, "user": 195, "assistant": 18, "tool": 5}
	after := map[string]int{"system": 14, "user": 193, "assistant": 18, "tool": 5}

	result := FormatCPADelta("cpa-norm", before, after)
	if !strings.Contains(result, "[cpa-norm]") {
		t.Fatal("should contain step name")
	}
	if !strings.Contains(result, "system:12→14") {
		t.Fatal("should contain system delta")
	}
	if !strings.Contains(result, "user:195→193") {
		t.Fatal("should contain user delta")
	}
	if strings.Contains(result, "assistant") {
		t.Fatal("should NOT contain unchanged roles")
	}
}

func TestFormatCPADelta_NoChange(t *testing.T) {
	counts := map[string]int{"system": 5, "user": 10}
	result := FormatCPADelta("cpa-norm", counts, counts)
	if result != "" {
		t.Fatalf("expected empty for no change, got %q", result)
	}
}

func TestFormatCPADelta_NewRole(t *testing.T) {
	before := map[string]int{"user": 5}
	after := map[string]int{"user": 5, "tool": 1}
	result := FormatCPADelta("cpa-step", before, after)
	if !strings.Contains(result, "tool:0→1") {
		t.Fatalf("should show new role, got %q", result)
	}
}

func TestFormatCPADelta_RemovedRole(t *testing.T) {
	before := map[string]int{"system": 3, "user": 5}
	after := map[string]int{"user": 5}
	result := FormatCPADelta("cpa-step", before, after)
	if !strings.Contains(result, "system:3→0") {
		t.Fatalf("should show removed role, got %q", result)
	}
}
