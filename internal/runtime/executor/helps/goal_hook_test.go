package helps

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/tidwall/gjson"
)

func TestIsCCGoalHookEnabled_NilConfig(t *testing.T) {
	if IsCCGoalHookEnabled(nil, "step-3.7-flash") {
		t.Error("expected false for nil config")
	}
}

func TestIsCCGoalHookEnabled_EmptyModel(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Default: []config.PayloadRule{{
			Models: []config.PayloadModelRule{{Name: "step-*", CCGoalHook: ptrBool(true)}},
		}},
	}}
	if IsCCGoalHookEnabled(cfg, "") {
		t.Error("expected false for empty model")
	}
}

func TestIsCCGoalHookEnabled_Matches(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Default: []config.PayloadRule{{
			Models: []config.PayloadModelRule{{Name: "step-*", CCGoalHook: ptrBool(true)}},
		}},
	}}
	if !IsCCGoalHookEnabled(cfg, "step-3.7-flash") {
		t.Error("expected true for matching model")
	}
}

func TestIsCCGoalHookEnabled_NotMatches(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Default: []config.PayloadRule{{
			Models: []config.PayloadModelRule{{Name: "step-*", CCGoalHook: ptrBool(true)}},
		}},
	}}
	if IsCCGoalHookEnabled(cfg, "deepseek-v4-flash") {
		t.Error("expected false for non-matching model")
	}
}

func TestIsCCGoalHookEnabled_Disabled(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Default: []config.PayloadRule{{
			Models: []config.PayloadModelRule{{Name: "step-*", CCGoalHook: ptrBool(false)}},
		}},
	}}
	if IsCCGoalHookEnabled(cfg, "step-3.7-flash") {
		t.Error("expected false when CCGoalHook is false")
	}
}

func TestIsCCGoalHookEnabled_ScansAllRuleLists(t *testing.T) {
	cfg := &config.Config{Payload: config.PayloadConfig{
		Default:     []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "a", CCGoalHook: ptrBool(true)}}}},
		DefaultRaw:  []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "b", CCGoalHook: ptrBool(true)}}}},
		Override:    []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "c", CCGoalHook: ptrBool(true)}}}},
		OverrideRaw: []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "d", CCGoalHook: ptrBool(true)}}}},
	}}
	if !IsCCGoalHookEnabled(cfg, "d") {
		t.Error("expected true for model matched in OverrideRaw")
	}
}

func TestInjectGoalHookConstraint_NotGoalHook(t *testing.T) {
	body := []byte(`{"system":[{"type":"text","text":"You are a helpful assistant."}],"messages":[]}`)
	result, injected := InjectGoalHookConstraint(body)
	if injected {
		t.Error("expected false for non-goal-hook request")
	}
	if string(result) != string(body) {
		t.Error("expected body unchanged for non-goal-hook request")
	}
}

func TestInjectGoalHookConstraint_GoalHookRequest(t *testing.T) {
	body := []byte(`{"system":[{"type":"text","text":"You are a Claude agent, built on Anthropic's Claude Agent SDK."},{"type":"text","text":"You are evaluating a stop-condition hook in Claude Code."}],"messages":[]}`)
	result, injected := InjectGoalHookConstraint(body)
	if !injected {
		t.Error("expected true for goal-hook request")
	}
	system := gjson.GetBytes(result, "system")
	if !system.IsArray() {
		t.Fatal("expected system to be an array")
	}
	arr := system.Array()
	if len(arr) != 3 {
		t.Fatalf("expected 3 system blocks, got %d", len(arr))
	}
	last := arr[len(arr)-1]
	if last.Get("type").String() != "text" {
		t.Errorf("expected last block type text, got %s", last.Get("type").String())
	}
	text := last.Get("text").String()
	if text != ccGoalHookSystemPrompt {
		t.Errorf("expected injected prompt, got:\n%s", text)
	}
}

func TestInjectGoalHookConstraint_InvalidJSON(t *testing.T) {
	result, injected := InjectGoalHookConstraint([]byte(`not json`))
	if injected {
		t.Error("expected false for invalid JSON")
	}
	if string(result) != "not json" {
		t.Error("expected body unchanged for invalid JSON")
	}
}
