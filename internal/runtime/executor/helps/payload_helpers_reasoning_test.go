package helps

import (
	"testing"

	"github.com/tidwall/gjson"
)

// TestEnsureReasoning_ThinkingActiveButNoEffort reproduces the failure mode where
// DeepSeek is already in thinking mode (a prior assistant message carries
// reasoning_content) but the current request does not carry reasoning_effort.
//
// Regression: EnsureReasoningContentInAssistantMessages keys its trigger on
// reasoning_effort presence. When a multi-turn tool-calling conversation omits
// reasoning_effort on a later turn, a bare assistant message with no
// reasoning_content slips through unpatched, and DeepSeek rejects the request
// with "The reasoning_content in the thinking mode must be passed back to the
// API." See litellm#26660 / #28057 for the same failure mode.
func TestEnsureReasoning_ThinkingActiveButNoEffort(t *testing.T) {
	// Prior assistant turn already returned reasoning_content -> thinking mode active.
	body := []byte(`{
		"model": "deepseek-chat",
		"messages": [
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"first answer","reasoning_content":"let me think"},
			{"role":"user","content":"continue"},
			{"role":"assistant","content":null,"tool_calls":[
				{"id":"call_1","type":"function","function":{"name":"read","arguments":"{}"}}
			]}
		]
	}`)

	out := EnsureReasoningContentInAssistantMessages(body)

	// The tool-call assistant message (index 3) lacks reasoning_content and must be patched.
	msg3 := gjson.GetBytes(out, "messages.3")
	if !msg3.Get("reasoning_content").Exists() {
		t.Fatalf("expected assistant tool-call message to receive reasoning_content patch in thinking mode, got missing; output=%s", out)
	}
}

// TestEnsureReasoning_ToolCallsNoEffort reproduces the OpenCode zen/go failure:
// the request carries tools, assistant messages have tool_calls but NO reasoning_effort
// and NO prior reasoning_content in history. DeepSeek docs require reasoning_content
// on every assistant turn once tools are involved, so all assistant messages must be patched.
func TestEnsureReasoning_ToolCallsNoEffort(t *testing.T) {
	body := []byte(`{
		"model":"deepseek-chat",
		"tools":[{"type":"function","function":{"name":"read","description":"r","parameters":{"type":"object","properties":{}}}}],
		"messages":[
			{"role":"user","content":"use the tool"},
			{"role":"assistant","content":null,"tool_calls":[
				{"id":"call_1","type":"function","function":{"name":"read","arguments":"{}"}}
			]},
			{"role":"tool","tool_call_id":"call_1","content":"result"},
			{"role":"user","content":"now summarize"},
			{"role":"assistant","content":null,"tool_calls":[
				{"id":"call_2","type":"function","function":{"name":"read","arguments":"{}"}}
			]}
		]
	}`)

	out := EnsureReasoningContentInAssistantMessages(body)

	// Assistant tool-call messages (index 1 and 4) must be patched with reasoning_content.
	if !gjson.GetBytes(out, "messages.1.reasoning_content").Exists() {
		t.Fatalf("expected assistant tool-call message (idx 1) to be patched, got missing; output=%s", out)
	}
	if !gjson.GetBytes(out, "messages.4.reasoning_content").Exists() {
		t.Fatalf("expected assistant tool-call message (idx 4) to be patched, got missing; output=%s", out)
	}
}

// TestEnsureReasoning_ToolCallsButNoToolsParam_NoInjection ensures the tool_calls signal
// only fires when the request also carries the tools parameter (avoiding pollution of
// plain deepseek-chat conversations that happen to include historical tool calls).
func TestEnsureReasoning_ToolCallsButNoToolsParam_NoInjection(t *testing.T) {
	body := []byte(`{
		"model":"deepseek-chat",
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":null,"tool_calls":[
				{"id":"call_1","type":"function","function":{"name":"read","arguments":"{}"}}
			]}
		]
	}`)

	out := EnsureReasoningContentInAssistantMessages(body)

	if gjson.GetBytes(out, "messages.1.reasoning_content").Exists() {
		t.Fatalf("expected NO injection when tools param absent even with tool_calls, got present; output=%s", out)
	}
}

// TestEnsureReasoning_EffortPresent_NoReasoningInHistory verifies the historical behavior:
// reasoning_effort present but no assistant message in history carries reasoning_content
// (conversation not yet in thinking mode). The patch should still apply because the
// request explicitly asks for reasoning (this is the pre-fix trigger).
func TestEnsureReasoning_EffortPresent_NoReasoningInHistory(t *testing.T) {
	body := []byte(`{
		"model":"deepseek-chat",
		"reasoning_effort":"high",
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"answer"},
			{"role":"user","content":"again"},
			{"role":"assistant","content":null,"tool_calls":[
				{"id":"call_1","type":"function","function":{"name":"read","arguments":"{}"}}
			]}
		]
	}`)

	out := EnsureReasoningContentInAssistantMessages(body)

	if !gjson.GetBytes(out, "messages.1.reasoning_content").Exists() {
		t.Fatalf("expected assistant text message to be patched, got missing; output=%s", out)
	}
	if !gjson.GetBytes(out, "messages.3.reasoning_content").Exists() {
		t.Fatalf("expected assistant tool-call message to be patched, got missing; output=%s", out)
	}
}

// TestEnsureReasoning_NotThinking_NoInjection ensures a conversation that has never
// returned reasoning_content (thinking mode never activated) is left untouched.
func TestEnsureReasoning_NotThinking_NoInjection(t *testing.T) {
	body := []byte(`{
		"model":"deepseek-chat",
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"plain answer"},
			{"role":"user","content":"again"},
			{"role":"assistant","content":"plain answer 2"}
		]
	}`)

	out := EnsureReasoningContentInAssistantMessages(body)

	if gjson.GetBytes(out, "messages.1.reasoning_content").Exists() {
		t.Fatalf("expected NO reasoning_content injection for non-thinking conversation, got present; output=%s", out)
	}
	if gjson.GetBytes(out, "messages.3.reasoning_content").Exists() {
		t.Fatalf("expected NO reasoning_content injection for non-thinking conversation, got present; output=%s", out)
	}
}

// TestEnsureReasoning_NonDeepSeekModel_NoInjection ensures non-DeepSeek models are untouched.
func TestEnsureReasoning_NonDeepSeekModel_NoInjection(t *testing.T) {
	body := []byte(`{
		"model":"gpt-4o",
		"reasoning_effort":"high",
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"answer","reasoning_content":"think"}
		]
	}`)

	out := EnsureReasoningContentInAssistantMessages(body)

	if gjson.GetBytes(out, "messages.1.reasoning_content").String() != "think" {
		t.Fatalf("expected reasoning_content preserved untouched for non-deepseek model, got %q; output=%s",
			gjson.GetBytes(out, "messages.1.reasoning_content").String(), out)
	}
}

// TestEnsureReasoning_AllMessagesAlreadyHaveReasoning_NoChange ensures already-complete
// messages are not modified and no structural change occurs.
func TestEnsureReasoning_AllMessagesAlreadyHaveReasoning_NoChange(t *testing.T) {
	body := []byte(`{
		"model":"deepseek-chat",
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"a","reasoning_content":"r1"},
			{"role":"assistant","content":"b","reasoning_content":"r2"}
		]
	}`)

	out := EnsureReasoningContentInAssistantMessages(body)

	if got, want := string(out), string(body); got != want {
		t.Fatalf("expected no structural change when all assistant messages already have reasoning_content,\n got  %s\n want %s", got, want)
	}
}
