package claude

import (
	"testing"

	"github.com/tidwall/gjson"
)

// TestConvertClaudeRequestToOpenAIWithCompatPreservesEmptySignatureThinking verifies that
// the compat variant preserves thinking text even when the signature is empty, while the
// default variant drops it (strict signature check).
func TestConvertClaudeRequestToOpenAIWithCompatPreservesEmptySignatureThinking(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reason","signature":""}]}]}`)

	withoutCompat := ConvertClaudeRequestToOpenAI("deepseek-v4", payload, false)
	if gjson.GetBytes(withoutCompat, "messages.0.reasoning_content").Exists() {
		t.Fatalf("default translation preserved empty-signature reasoning: %s", withoutCompat)
	}

	withCompat := ConvertClaudeRequestToOpenAIWithCompat("deepseek-v4", payload, false)
	if gjson.GetBytes(withCompat, "messages.0.reasoning_content").String() != "reason" {
		t.Fatalf("compat translation missing reasoning_content: %s", withCompat)
	}
}

// TestConvertClaudeRequestToOpenAIWithCompatPreservesThinkingWithToolCalls verifies that
// reasoning_content and tool_calls coexist on the same assistant message.
func TestConvertClaudeRequestToOpenAIWithCompatPreservesThinkingWithToolCalls(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reason","signature":""},{"type":"text","text":"Reading files."},{"type":"tool_use","id":"call_1","name":"Read","input":{"path":"main.go"}}]}]}`)

	result := ConvertClaudeRequestToOpenAIWithCompat("deepseek-v4", payload, false)
	assistant := gjson.GetBytes(result, "messages.0")
	if got := assistant.Get("reasoning_content").String(); got != "reason" {
		t.Fatalf("reasoning_content = %q, want %q; output: %s", got, "reason", result)
	}
	if !assistant.Get("tool_calls").Exists() {
		t.Fatalf("tool_calls missing from compatible translation: %s", result)
	}
}

// TestConvertClaudeRequestToOpenAIWithCompatDoesNotAddReasoningWithoutThinking verifies that
// compat does not invent reasoning_content when there is no thinking block.
func TestConvertClaudeRequestToOpenAIWithCompatDoesNotAddReasoningWithoutThinking(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"Read","input":{}}]}]}`)

	result := ConvertClaudeRequestToOpenAIWithCompat("deepseek-v4", payload, false)
	assistant := gjson.GetBytes(result, "messages.0")
	if assistant.Get("reasoning_content").Exists() {
		t.Fatalf("compatible translation added reasoning_content without thinking: %s", result)
	}
	if !assistant.Get("tool_calls").Exists() {
		t.Fatalf("tool_calls missing from compatible translation: %s", result)
	}
}

// TestConvertClaudeRequestToOpenAIWithCompatPreservesIncompatibleThinking verifies that
// the compat variant preserves thinking even with an incompatible (opaque) signature,
// which the strict path would drop.
func TestConvertClaudeRequestToOpenAIWithCompatPreservesIncompatibleThinking(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reason","signature":"claude#opaque"},{"type":"tool_use","id":"call_1","name":"Read","input":{}}]}]}`)

	result := ConvertClaudeRequestToOpenAIWithCompat("deepseek-v4", payload, false)
	assistant := gjson.GetBytes(result, "messages.0")
	if got := assistant.Get("reasoning_content").String(); got != "reason" {
		t.Fatalf("reasoning_content = %q, want %q; output: %s", got, "reason", result)
	}
	if !assistant.Get("tool_calls").Exists() {
		t.Fatalf("tool_calls missing from compatible translation: %s", result)
	}
}

// TestConvertClaudeRequestToOpenAIWithoutCompatDoesNotAddReasoningForToolCalls verifies that
// the default (non-compat) path does not add reasoning_content for tool-call-only messages.
func TestConvertClaudeRequestToOpenAIWithoutCompatDoesNotAddReasoningForToolCalls(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"Read","input":{}}]}]}`)

	result := ConvertClaudeRequestToOpenAI("deepseek-v4", payload, false)
	if gjson.GetBytes(result, "messages.0.reasoning_content").Exists() {
		t.Fatalf("default translation added reasoning_content: %s", result)
	}
}

// TestConvertClaudeRequestToOpenAIWithCompatJoinsMultipleThinkingBlocks verifies that
// multiple thinking blocks are joined with "\n\n" into reasoning_content (order preserved).
func TestConvertClaudeRequestToOpenAIWithCompatJoinsMultipleThinkingBlocks(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reason1","signature":""},{"type":"thinking","thinking":"reason2","signature":""}]}]}`)

	result := ConvertClaudeRequestToOpenAIWithCompat("deepseek-v4", payload, false)
	if got := gjson.GetBytes(result, "messages.0.reasoning_content").String(); got != "reason1\n\nreason2" {
		t.Fatalf("reasoning_content = %q, want %q; output: %s", got, "reason1\n\nreason2", result)
	}
}

// TestConvertClaudeRequestToOpenAIWithCompatSkipsEmptyThinking verifies that empty or
// whitespace-only thinking text is not written to reasoning_content even in compat mode.
func TestConvertClaudeRequestToOpenAIWithCompatSkipsEmptyThinking(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"   ","signature":""},{"type":"text","text":"hi"}]}]}`)

	result := ConvertClaudeRequestToOpenAIWithCompat("deepseek-v4", payload, false)
	if gjson.GetBytes(result, "messages.0.reasoning_content").Exists() {
		t.Fatalf("compat translation wrote empty reasoning_content: %s", result)
	}
}

// TestConvertClaudeRequestToOpenAIWithCompatIgnoresUserThinking verifies that the
// role==assistant guard still applies in compat mode (AC4): thinking in user/system
// roles must not be mapped to reasoning_content even with preserveThinkingBlocks.
func TestConvertClaudeRequestToOpenAIWithCompatIgnoresUserThinking(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"thinking","thinking":"should not leak","signature":""}]},{"role":"assistant","content":[{"type":"thinking","thinking":"real reasoning","signature":""}]}]}`)

	result := ConvertClaudeRequestToOpenAIWithCompat("deepseek-v4", payload, false)
	// The user message contains only a thinking block (no text/tool), so it is dropped
	// entirely. The assistant message must keep its reasoning_content.
	if got := gjson.GetBytes(result, "messages.0.reasoning_content").String(); got != "real reasoning" {
		t.Fatalf("assistant reasoning_content = %q, want %q; output: %s", got, "real reasoning", result)
	}
	if got := gjson.GetBytes(result, "messages.0.role").String(); got != "assistant" {
		t.Fatalf("expected only the assistant message to survive, got role=%q; output: %s", got, result)
	}
}

// TestConvertClaudeRequestToOpenAIWithCompatIgnoresRedactedThinking verifies that
// redacted_thinking is never mapped to reasoning_content (AC2), even in compat mode.
func TestConvertClaudeRequestToOpenAIWithCompatIgnoresRedactedThinking(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"assistant","content":[{"type":"redacted_thinking","data":"redacted"},{"type":"text","text":"answer"}]}]}`)

	result := ConvertClaudeRequestToOpenAIWithCompat("deepseek-v4", payload, false)
	if gjson.GetBytes(result, "messages.0.reasoning_content").Exists() {
		t.Fatalf("redacted_thinking leaked into reasoning_content: %s", result)
	}
}
