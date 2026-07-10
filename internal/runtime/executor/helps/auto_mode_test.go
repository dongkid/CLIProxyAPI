package helps

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/tidwall/gjson"
)

func ptrInt(v int) *int       { return &v }
func ptrStr(v string) *string { return &v }

// classifierBody returns a classifier body with system array format.
func classifierBody() string {
	return `{"model":"deepseek-v4-flash","max_tokens":2112,"system":[{"type":"text","text":"You are a security monitor for autonomous AI coding agents."}],"messages":[{"role":"user","content":"hello"}]}`
}

// --- IsCCAutoModeEnabled ---

func TestIsCCAutoModeEnabled_NilConfig(t *testing.T) {
	if IsCCAutoModeEnabled(nil, "deepseek-v4-flash") {
		t.Error("expected false")
	}
}

func TestIsCCAutoModeEnabled_EmptyModel(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "ds-*", CCAutoMode: ptrBool(true)}}}},
	}}
	if IsCCAutoModeEnabled(cfg, "") {
		t.Error("expected false")
	}
}

func TestIsCCAutoModeEnabled_Matches(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "ds-*", CCAutoMode: ptrBool(true)}}}},
	}}
	if !IsCCAutoModeEnabled(cfg, "ds-v4") {
		t.Error("expected true")
	}
}

func TestIsCCAutoModeEnabled_NotMatches(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "other-*", CCAutoMode: ptrBool(true)}}}},
	}}
	if IsCCAutoModeEnabled(cfg, "ds-v4") {
		t.Error("expected false")
	}
}

func TestIsCCAutoModeEnabled_Disabled(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "ds-*", CCAutoMode: ptrBool(false)}}}},
	}}
	if IsCCAutoModeEnabled(cfg, "ds-v4") {
		t.Error("expected false when false")
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
		t.Error("expected true from override-raw")
	}
}

// --- GetCCAutoModeRedirect ---

func TestGetCCAutoModeRedirect_NotConfigured(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "ds-*", CCAutoMode: ptrBool(true)}}}},
	}}
	if v := GetCCAutoModeRedirect(cfg, "ds-v4"); v != "" {
		t.Errorf("expected empty, got %q", v)
	}
}

func TestGetCCAutoModeRedirect_Configured(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{{Models: []config.PayloadModelRule{
			{Name: "ds-*", CCAutoMode: ptrBool(true), CCAutoModeRedirect: ptrStr("target-model")},
		}}},
	}}
	if v := GetCCAutoModeRedirect(cfg, "ds-v4"); v != "target-model" {
		t.Errorf("expected target-model, got %q", v)
	}
}

func TestGetCCAutoModeRedirect_NilCfg(t *testing.T) {
	if v := GetCCAutoModeRedirect(nil, "ds-v4"); v != "" {
		t.Errorf("expected empty for nil cfg, got %q", v)
	}
}

func TestGetCCAutoModeRedirect_NoMatch(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{{Models: []config.PayloadModelRule{
			{Name: "other-*", CCAutoMode: ptrBool(true), CCAutoModeRedirect: ptrStr("x")},
		}}},
	}}
	if v := GetCCAutoModeRedirect(cfg, "ds-v4"); v != "" {
		t.Errorf("expected empty for no match, got %q", v)
	}
}

// --- GetCCAutoModeMaxTokens ---

func TestGetCCAutoModeMaxTokens_NotConfigured(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "ds-*", CCAutoMode: ptrBool(true)}}}},
	}}
	if v := GetCCAutoModeMaxTokens(cfg, "ds-v4"); v != 0 {
		t.Errorf("expected 0 (not configured), got %d", v)
	}
}

func TestGetCCAutoModeMaxTokens_Configured(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{{Models: []config.PayloadModelRule{
			{Name: "ds-*", CCAutoMode: ptrBool(true), CCAutoModeMaxTokens: ptrInt(16384)},
		}}},
	}}
	if v := GetCCAutoModeMaxTokens(cfg, "ds-v4"); v != 16384 {
		t.Errorf("expected 16384, got %d", v)
	}
}

func TestGetCCAutoModeMaxTokens_NilCfg(t *testing.T) {
	if v := GetCCAutoModeMaxTokens(nil, "ds-v4"); v != 0 {
		t.Errorf("expected 0 for nil cfg, got %d", v)
	}
}

func TestGetCCAutoModeMaxTokens_NoMatch(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{{Models: []config.PayloadModelRule{
			{Name: "other-*", CCAutoMode: ptrBool(true), CCAutoModeMaxTokens: ptrInt(999)},
		}}},
	}}
	if v := GetCCAutoModeMaxTokens(cfg, "ds-v4"); v != 0 {
		t.Errorf("expected 0 for no match, got %d", v)
	}
}

// --- GetCCAutoModeReasoningEffort ---

func TestGetCCAutoModeReasoningEffort_NotConfigured(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "ds-*", CCAutoMode: ptrBool(true)}}}},
	}}
	if v := GetCCAutoModeReasoningEffort(cfg, "ds-v4"); v != "" {
		t.Errorf("expected empty, got %q", v)
	}
}

func TestGetCCAutoModeReasoningEffort_Configured(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{{Models: []config.PayloadModelRule{
			{Name: "ds-*", CCAutoMode: ptrBool(true), CCAutoModeReasoningEffort: ptrStr("high")},
		}}},
	}}
	if v := GetCCAutoModeReasoningEffort(cfg, "ds-v4"); v != "high" {
		t.Errorf("expected high, got %q", v)
	}
}

func TestGetCCAutoModeReasoningEffort_NilCfg(t *testing.T) {
	if v := GetCCAutoModeReasoningEffort(nil, "ds-v4"); v != "" {
		t.Errorf("expected empty for nil cfg, got %q", v)
	}
}

// --- InjectAutoModeOverrides — detection ---

func TestInject_DetectsInSystemArray(t *testing.T) {
	body := []byte(classifierBody())
	result, injected := InjectAutoModeOverrides(body, 8192, "")
	if !injected {
		t.Fatal("expected true")
	}
	if gjson.GetBytes(result, "max_tokens").Int() != 8192 {
		t.Error("expected max_tokens=8192")
	}
}

func TestInject_DetectsInSystemRoleMessage(t *testing.T) {
	body := []byte(`{"model":"ds","max_tokens":2112,"messages":[{"role":"system","content":"You are a security monitor for autonomous AI coding agents."},{"role":"user","content":"hello"}]}`)
	result, injected := InjectAutoModeOverrides(body, 8192, "")
	if !injected {
		t.Fatal("expected true")
	}
	if gjson.GetBytes(result, "max_tokens").Int() != 8192 {
		t.Error("expected max_tokens=8192")
	}
}

func TestInject_DetectsInContentBlockNoRole(t *testing.T) {
	body := []byte(`{"model":"ds","max_tokens":2112,"messages":[{"content":[{"type":"text","text":"You are a security monitor for autonomous AI coding agents."}]}]}`)
	result, injected := InjectAutoModeOverrides(body, 8192, "")
	if !injected {
		t.Fatal("expected true")
	}
	if gjson.GetBytes(result, "max_tokens").Int() != 8192 {
		t.Error("expected max_tokens=8192")
	}
}

func TestInject_NoFalsePositiveOnUserRole(t *testing.T) {
	body := []byte(`{"model":"ds","max_tokens":2112,"messages":[{"role":"user","content":"You are a security monitor for autonomous AI coding agents."}]}`)
	if _, injected := InjectAutoModeOverrides(body, 8192, ""); injected {
		t.Error("expected false when phrase in user message")
	}
}

func TestInject_NotClassifier(t *testing.T) {
	body := []byte(`{"model":"ds","max_tokens":2112,"messages":[{"role":"user","content":"hello"}]}`)
	if _, injected := InjectAutoModeOverrides(body, 8192, ""); injected {
		t.Error("expected false for non-classifier")
	}
}

// --- InjectAutoModeOverrides — max_tokens ---

func TestInject_MaxTokensOverride(t *testing.T) {
	result, injected := InjectAutoModeOverrides([]byte(classifierBody()), 16384, "")
	if !injected {
		t.Fatal("expected true")
	}
	if v := gjson.GetBytes(result, "max_tokens").Int(); v != 16384 {
		t.Errorf("expected 16384, got %d", v)
	}
}

func TestInject_MaxTokens_0Skips(t *testing.T) {
	body := []byte(classifierBody())
	result, injected := InjectAutoModeOverrides(body, 0, "")
	if injected {
		t.Error("expected false when max_tokens=0")
	}
	if gjson.GetBytes(result, "max_tokens").Int() != 2112 {
		t.Error("expected max_tokens unchanged at 2112")
	}
}

func TestInject_MaxTokens_NegativeSkips(t *testing.T) {
	body := []byte(classifierBody())
	result, injected := InjectAutoModeOverrides(body, -1, "")
	if injected {
		t.Error("expected false when max_tokens=-1")
	}
	if gjson.GetBytes(result, "max_tokens").Int() != 2112 {
		t.Error("expected max_tokens unchanged at 2112")
	}
}

func TestInject_MaxTokens_InvalidBody(t *testing.T) {
	result, injected := InjectAutoModeOverrides([]byte(`not json`), 8192, "")
	if injected {
		t.Error("expected false for invalid JSON")
	}
	if string(result) != "not json" {
		t.Error("expected original body returned")
	}
}

// --- InjectAutoModeOverrides — reasoning_effort ---

func TestInject_ReasoningEffortOverride(t *testing.T) {
	body := []byte(`{"model":"ds","max_tokens":2112,"reasoning_effort":"max","reasoning":{"effort":"max"},"system":[{"type":"text","text":"You are a security monitor for autonomous AI coding agents."}],"messages":[{"role":"user","content":"h"}]}`)
	result, injected := InjectAutoModeOverrides(body, 0, "high")
	if !injected {
		t.Fatal("expected true")
	}
	if v := gjson.GetBytes(result, "reasoning_effort").String(); v != "high" {
		t.Errorf("expected reasoning_effort=high, got %q", v)
	}
	if v := gjson.GetBytes(result, "reasoning.effort").String(); v != "high" {
		t.Errorf("expected reasoning.effort=high, got %q", v)
	}
}

func TestInject_ReasoningEffortEmptySkips(t *testing.T) {
	body := []byte(`{"model":"ds","max_tokens":2112,"reasoning_effort":"max","system":[{"type":"text","text":"You are a security monitor for autonomous AI coding agents."}],"messages":[{"role":"user","content":"h"}]}`)
	result, injected := InjectAutoModeOverrides(body, 0, "")
	if injected {
		t.Error("expected false when both params are zero/empty")
	}
	if v := gjson.GetBytes(result, "reasoning_effort").String(); v != "max" {
		t.Errorf("expected reasoning_effort unchanged at max, got %q", v)
	}
}

func TestInject_ReasoningEffortSkipsWhenAlreadySet(t *testing.T) {
	body := []byte(`{"model":"ds","max_tokens":2112,"reasoning_effort":"high","reasoning":{"effort":"high"},"system":[{"type":"text","text":"You are a security monitor for autonomous AI coding agents."}],"messages":[{"role":"user","content":"h"}]}`)
	result, injected := InjectAutoModeOverrides(body, 0, "high")
	if injected {
		t.Error("expected false when effort already matches")
	}
	if v := gjson.GetBytes(result, "reasoning_effort").String(); v != "high" {
		t.Errorf("expected reasoning_effort=high, got %q", v)
	}
}

func TestInject_BothParams(t *testing.T) {
	body := []byte(`{"model":"ds","max_tokens":2112,"reasoning_effort":"max","system":[{"type":"text","text":"You are a security monitor for autonomous AI coding agents."}],"messages":[{"role":"user","content":"h"}]}`)
	result, injected := InjectAutoModeOverrides(body, 8192, "low")
	if !injected {
		t.Fatal("expected true")
	}
	if gjson.GetBytes(result, "max_tokens").Int() != 8192 {
		t.Error("expected max_tokens=8192")
	}
	if gjson.GetBytes(result, "reasoning_effort").String() != "low" {
		t.Error("expected reasoning_effort=low")
	}
}

// --- InjectAutoModeOverrides — combined scenarios ---

func TestInject_NotConfiguredAtAll(t *testing.T) {
	result, injected := InjectAutoModeOverrides([]byte(classifierBody()), 0, "")
	if injected {
		t.Error("expected false when neither param is set")
	}
	if gjson.GetBytes(result, "max_tokens").Int() != 2112 {
		t.Error("expected max_tokens unchanged")
	}
}
