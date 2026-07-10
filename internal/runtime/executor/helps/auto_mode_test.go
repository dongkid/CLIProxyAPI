package helps

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/tidwall/gjson"
)

func ptrInt(v int) *int { return &v }

// classifierBody returns a minimal auto mode classifier body with the
// system prompt in the top-level system array (Claude Messages API format).
func classifierBody(overrides ...string) string {
	body := `{"model":"deepseek-v4-flash","max_tokens":2112,"system":[{"type":"text","text":"You are a security monitor for autonomous AI coding agents."}],"messages":[{"role":"user","content":"hello"}]}`
	if len(overrides) > 0 {
		body = overrides[0]
	}
	return body
}

// --- IsCCAutoModeEnabled ---

func TestIsCCAutoModeEnabled_NilConfig(t *testing.T) {
	if IsCCAutoModeEnabled(nil, "deepseek-v4-flash") {
		t.Error("expected false for nil config")
	}
}

func TestIsCCAutoModeEnabled_EmptyModel(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{{
			Models: []config.PayloadModelRule{{Name: "deepseek-v4-*", CCAutoMode: ptrBool(true)}},
		}},
	}}
	if IsCCAutoModeEnabled(cfg, "") {
		t.Error("expected false for empty model")
	}
}

func TestIsCCAutoModeEnabled_Matches(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{{
			Models: []config.PayloadModelRule{{Name: "deepseek-v4-*", CCAutoMode: ptrBool(true)}},
		}},
	}}
	if !IsCCAutoModeEnabled(cfg, "deepseek-v4-flash") {
		t.Error("expected true for matching model")
	}
}

func TestIsCCAutoModeEnabled_NotMatches(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{{
			Models: []config.PayloadModelRule{{Name: "gemini-*", CCAutoMode: ptrBool(true)}},
		}},
	}}
	if IsCCAutoModeEnabled(cfg, "deepseek-v4-flash") {
		t.Error("expected false for non-matching model")
	}
}

func TestIsCCAutoModeEnabled_Disabled(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{{
			Models: []config.PayloadModelRule{{Name: "deepseek-v4-*", CCAutoMode: ptrBool(false)}},
		}},
	}}
	if IsCCAutoModeEnabled(cfg, "deepseek-v4-flash") {
		t.Error("expected false when CCAutoMode is false")
	}
}

func TestIsCCAutoModeEnabled_ScansAllRuleLists(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Default:     []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "a", CCAutoMode: ptrBool(true)}}}},
		DefaultRaw:  []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "b", CCAutoMode: ptrBool(true)}}}},
		Override:    []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "c", CCAutoMode: ptrBool(true)}}}},
		OverrideRaw: []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "d", CCAutoMode: ptrBool(true)}}}},
	}}
	if !IsCCAutoModeEnabled(cfg, "d") {
		t.Error("expected true, override-raw rule should be scanned")
	}
}

// --- GetCCAutoModeMaxTokens ---

func TestGetCCAutoModeMaxTokens_Default(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{{
			Models: []config.PayloadModelRule{{Name: "deepseek-v4-*", CCAutoMode: ptrBool(true)}},
		}},
	}}
	val := GetCCAutoModeMaxTokens(cfg, "deepseek-v4-flash")
	if val != 8192 {
		t.Errorf("expected default 8192, got %d", val)
	}
}

func TestGetCCAutoModeMaxTokens_Custom(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{{
			Models: []config.PayloadModelRule{
				{Name: "deepseek-v4-*", CCAutoMode: ptrBool(true), CCAutoModeMaxTokens: ptrInt(16384)},
			},
		}},
	}}
	val := GetCCAutoModeMaxTokens(cfg, "deepseek-v4-flash")
	if val != 16384 {
		t.Errorf("expected custom 16384, got %d", val)
	}
}

func TestGetCCAutoModeMaxTokens_NilCfg(t *testing.T) {
	val := GetCCAutoModeMaxTokens(nil, "deepseek-v4-flash")
	if val != 8192 {
		t.Errorf("expected default 8192 for nil config, got %d", val)
	}
}

func TestGetCCAutoModeMaxTokens_NoMatch(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{{
			Models: []config.PayloadModelRule{
				{Name: "gemini-*", CCAutoMode: ptrBool(true), CCAutoModeMaxTokens: ptrInt(9999)},
			},
		}},
	}}
	val := GetCCAutoModeMaxTokens(cfg, "deepseek-v4-flash")
	if val != 8192 {
		t.Errorf("expected default 8192 for non-matching model, got %d", val)
	}
}

// --- InjectAutoModeOverrides — detection formats ---

func TestInjectAutoModeOverrides_DetectsInSystemArray(t *testing.T) {
	// Claude Messages API format: system is a top-level array.
	body := []byte(classifierBody())
	result, injected := InjectAutoModeOverrides(body, 8192)
	if !injected {
		t.Fatal("expected true when phrase is in top-level system array")
	}
	mt := gjson.GetBytes(result, "max_tokens")
	if mt.Int() != 8192 {
		t.Errorf("expected max_tokens=8192, got %d", mt.Int())
	}
}

func TestInjectAutoModeOverrides_DetectsInSystemRoleMessage(t *testing.T) {
	// OpenAI format: system prompt as messages[0] with role="system".
	body := []byte(`{"model":"deepseek-v4-flash","max_tokens":2112,"messages":[{"role":"system","content":"You are a security monitor for autonomous AI coding agents."},{"role":"user","content":"hello"}]}`)
	result, injected := InjectAutoModeOverrides(body, 8192)
	if !injected {
		t.Fatal("expected true when phrase is in messages[0] role=system")
	}
	mt := gjson.GetBytes(result, "max_tokens")
	if mt.Int() != 8192 {
		t.Errorf("expected max_tokens=8192, got %d", mt.Int())
	}
}

func TestInjectAutoModeOverrides_DetectsInContentBlockNoRole(t *testing.T) {
	// Translated format where content is an array of {type, text} blocks
	// without an explicit role field.
	body := []byte(`{"model":"deepseek-v4-flash","max_tokens":2112,"messages":[{"content":[{"type":"text","text":"You are a security monitor for autonomous AI coding agents."}]}]}`)
	result, injected := InjectAutoModeOverrides(body, 8192)
	if !injected {
		t.Fatal("expected true when phrase is in content block without role")
	}
	mt := gjson.GetBytes(result, "max_tokens")
	if mt.Int() != 8192 {
		t.Errorf("expected max_tokens=8192, got %d", mt.Int())
	}
}

func TestInjectAutoModeOverrides_NoFalsePositiveOnUserRole(t *testing.T) {
	// Phrase in a message with explicit role="user" — must NOT match.
	body := []byte(`{"model":"deepseek-v4-flash","max_tokens":2112,"messages":[{"role":"user","content":"You are a security monitor for autonomous AI coding agents."}]}`)
	result, injected := InjectAutoModeOverrides(body, 8192)
	if injected {
		t.Error("expected false when phrase is in user role message")
	}
	if string(result) != string(body) {
		t.Error("expected body unchanged for false positive")
	}
}

func TestInjectAutoModeOverrides_NotClassifier(t *testing.T) {
	// Normal request without the detect phrase.
	body := []byte(`{"model":"deepseek-v4-flash","max_tokens":2112,"messages":[{"role":"user","content":"hello"}]}`)
	result, injected := InjectAutoModeOverrides(body, 8192)
	if injected {
		t.Error("expected false for non-classifier request")
	}
	if string(result) != string(body) {
		t.Error("expected body unchanged for non-classifier request")
	}
}

// --- InjectAutoModeOverrides — max_tokens override ---

func TestInjectAutoModeOverrides_IncreasesMaxTokens(t *testing.T) {
	result, injected := InjectAutoModeOverrides([]byte(classifierBody()), 8192)
	if !injected {
		t.Fatal("expected true")
	}
	mt := gjson.GetBytes(result, "max_tokens")
	if mt.Int() != 8192 {
		t.Errorf("expected max_tokens=8192, got %d", mt.Int())
	}
}

func TestInjectAutoModeOverrides_CustomMaxTokens(t *testing.T) {
	result, injected := InjectAutoModeOverrides([]byte(classifierBody()), 16384)
	if !injected {
		t.Fatal("expected true")
	}
	mt := gjson.GetBytes(result, "max_tokens")
	if mt.Int() != 16384 {
		t.Errorf("expected max_tokens=16384, got %d", mt.Int())
	}
}

func TestInjectAutoModeOverrides_SkipsWhenSufficient(t *testing.T) {
	body := []byte(classifierBody(`{"model":"deepseek-v4-flash","max_tokens":16384,"reasoning_effort":"high","system":[{"type":"text","text":"You are a security monitor for autonomous AI coding agents."}],"messages":[{"role":"user","content":"hello"}]}`))
	result, injected := InjectAutoModeOverrides(body, 8192)
	if injected {
		t.Error("expected false when max_tokens sufficient and effort <= high")
	}
	mt := gjson.GetBytes(result, "max_tokens")
	if mt.Int() != 16384 {
		t.Errorf("expected max_tokens unchanged at 16384, got %d", mt.Int())
	}
}

func TestInjectAutoModeOverrides_SetsWhenMissing(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-flash","system":[{"type":"text","text":"You are a security monitor for autonomous AI coding agents."}],"messages":[{"role":"user","content":"hello"}]}`)
	result, injected := InjectAutoModeOverrides(body, 8192)
	if !injected {
		t.Fatal("expected true")
	}
	mt := gjson.GetBytes(result, "max_tokens")
	if !mt.Exists() {
		t.Fatal("expected max_tokens to be set")
	}
	if mt.Int() != 8192 {
		t.Errorf("expected max_tokens=8192, got %d", mt.Int())
	}
}

func TestInjectAutoModeOverrides_InvalidBody(t *testing.T) {
	result, injected := InjectAutoModeOverrides([]byte(`not json`), 8192)
	if injected {
		t.Error("expected false for invalid JSON")
	}
	if string(result) != "not json" {
		t.Error("expected original body returned unchanged")
	}
}

// --- InjectAutoModeOverrides — effort capping ---

func TestInjectAutoModeOverrides_CapsMaxEffort(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-flash","max_tokens":2112,"reasoning_effort":"max","system":[{"type":"text","text":"You are a security monitor for autonomous AI coding agents."}],"messages":[{"role":"user","content":"hello"}]}`)
	result, injected := InjectAutoModeOverrides(body, 8192)
	if !injected {
		t.Fatal("expected true")
	}
	mt := gjson.GetBytes(result, "max_tokens")
	if mt.Int() != 8192 {
		t.Errorf("expected max_tokens=8192, got %d", mt.Int())
	}
	effort := gjson.GetBytes(result, "reasoning_effort")
	if effort.String() != "high" {
		t.Errorf("expected reasoning_effort capped to high, got %s", effort.String())
	}
}

func TestInjectAutoModeOverrides_CapsReasoningDotEffort(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-flash","max_tokens":2112,"reasoning":{"effort":"ultra"},"system":[{"type":"text","text":"You are a security monitor for autonomous AI coding agents."}],"messages":[{"role":"user","content":"hello"}]}`)
	result, injected := InjectAutoModeOverrides(body, 8192)
	if !injected {
		t.Fatal("expected true")
	}
	effort := gjson.GetBytes(result, "reasoning.effort")
	if effort.String() != "high" {
		t.Errorf("expected reasoning.effort capped to high, got %s", effort.String())
	}
}

func TestInjectAutoModeOverrides_KeepsHighEffort(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-flash","max_tokens":2112,"reasoning_effort":"high","system":[{"type":"text","text":"You are a security monitor for autonomous AI coding agents."}],"messages":[{"role":"user","content":"hello"}]}`)
	result, injected := InjectAutoModeOverrides(body, 8192)
	if !injected {
		t.Fatal("expected true (max_tokens changed)")
	}
	effort := gjson.GetBytes(result, "reasoning_effort")
	if effort.String() != "high" {
		t.Errorf("expected reasoning_effort unchanged at high, got %s", effort.String())
	}
	mt := gjson.GetBytes(result, "max_tokens")
	if mt.Int() != 8192 {
		t.Errorf("expected max_tokens=8192, got %d", mt.Int())
	}
}

func TestInjectAutoModeOverrides_KeepsMediumEffort(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-flash","max_tokens":2112,"reasoning_effort":"medium","system":[{"type":"text","text":"You are a security monitor for autonomous AI coding agents."}],"messages":[{"role":"user","content":"hello"}]}`)
	result, injected := InjectAutoModeOverrides(body, 8192)
	if !injected {
		t.Fatal("expected true (max_tokens changed)")
	}
	effort := gjson.GetBytes(result, "reasoning_effort")
	if effort.String() != "medium" {
		t.Errorf("expected reasoning_effort unchanged at medium, got %s", effort.String())
	}
}

func TestInjectAutoModeOverrides_NotCappedWhenMissing(t *testing.T) {
	body := []byte(classifierBody())
	result, injected := InjectAutoModeOverrides(body, 8192)
	if !injected {
		t.Fatal("expected true (max_tokens changed)")
	}
	// No effort field present — should not be added.
	if gjson.GetBytes(result, "reasoning_effort").Exists() {
		t.Error("expected reasoning_effort not to be injected when absent")
	}
}
