package executor

import (
	"testing"

	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

// TestTranslateRequestCompat_DeepseekClaudeToOpenAI_PreservesThinking verifies that a
// deepseek-prefixed Claude->OpenAI conversion routes through the compat translator,
// which preserves empty-signature thinking blocks as reasoning_content. This is the
// feature's core purpose: without it, DeepSeek returns a 400 on multi-turn thinking.
func TestTranslateRequestCompat_DeepseekClaudeToOpenAI_PreservesThinking(t *testing.T) {
	e := &OpenAICompatExecutor{}
	payload := []byte(`{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reason","signature":""},{"type":"tool_use","id":"call_1","name":"Read","input":{}}]}]}`)

	out := e.translateRequestCompat(sdktranslator.FormatClaude, sdktranslator.FormatOpenAI, "deepseek-v4-flash", payload, false)

	if got := gjson.GetBytes(out, "messages.0.reasoning_content").String(); got != "reason" {
		t.Fatalf("expected compat translator to preserve thinking as reasoning_content, got %q; output=%s", got, out)
	}
	if !gjson.GetBytes(out, "messages.0.tool_calls").Exists() {
		t.Fatalf("expected tool_calls preserved; output=%s", out)
	}
}

// TestTranslateRequestCompat_NonDeepseekClaudeToOpenAI_UsesStrict verifies that a
// non-deepseek Claude->OpenAI conversion keeps the strict translator (no thinking
// preservation for empty-signature blocks). This prevents compat leaking thinking to
// channels that don't need it.
func TestTranslateRequestCompat_NonDeepseekClaudeToOpenAI_UsesStrict(t *testing.T) {
	e := &OpenAICompatExecutor{}
	payload := []byte(`{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reason","signature":""},{"type":"tool_use","id":"call_1","name":"Read","input":{}}]}]}`)

	out := e.translateRequestCompat(sdktranslator.FormatClaude, sdktranslator.FormatOpenAI, "gpt-5", payload, false)

	if gjson.GetBytes(out, "messages.0.reasoning_content").Exists() {
		t.Fatalf("expected strict translator to drop empty-signature thinking for non-deepseek, got reasoning_content; output=%s", out)
	}
}

// TestTranslateRequestCompat_NonClaudePath_UsesDefault verifies that non-Claude source
// formats are not routed through compat even for deepseek models.
func TestTranslateRequestCompat_NonClaudePath_UsesDefault(t *testing.T) {
	e := &OpenAICompatExecutor{}
	payload := []byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hi"}]}`)

	out := e.translateRequestCompat(sdktranslator.FormatOpenAI, sdktranslator.FormatOpenAI, "deepseek-v4-flash", payload, false)

	// OpenAI->OpenAI is identity; the payload must pass through with model unchanged.
	if gjson.GetBytes(out, "model").String() != "deepseek-v4-flash" {
		t.Fatalf("expected default identity translation, got model=%q; output=%s", gjson.GetBytes(out, "model").String(), out)
	}
}

// TestTranslateRequestCompat_DeepseekCaseInsensitive verifies the deepseek prefix match
// is case-insensitive (uppercase model names also route to compat).
func TestTranslateRequestCompat_DeepseekCaseInsensitive(t *testing.T) {
	e := &OpenAICompatExecutor{}
	payload := []byte(`{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reason","signature":""}]}]}`)

	out := e.translateRequestCompat(sdktranslator.FormatClaude, sdktranslator.FormatOpenAI, "DeepSeek-V4-Pro", payload, false)

	if got := gjson.GetBytes(out, "messages.0.reasoning_content").String(); got != "reason" {
		t.Fatalf("expected case-insensitive deepseek prefix to route compat, got reasoning_content=%q; output=%s", got, out)
	}
}
