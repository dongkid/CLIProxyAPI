package helps

import (
	"fmt"
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

// TestEnsureReasoning_MalformedMessages_NoGarbageKey verifies that a non-array messages
// field is left untouched (previously the implementation wrapped it into a single-element
// slice and wrote a garbage "0" key).
func TestEnsureReasoning_MalformedMessages_NoGarbageKey(t *testing.T) {
	bodies := []string{
		`{"model":"deepseek-chat","messages":{}}`,
		`{"model":"deepseek-chat","messages":"not-an-array"}`,
		`{"model":"deepseek-chat"}`,
		`{"model":"deepseek-chat","messages":null}`,
	}
	for _, bodyStr := range bodies {
		body := []byte(bodyStr)
		out := EnsureReasoningContentInAssistantMessages(body)
		if got, want := string(out), bodyStr; got != want {
			t.Fatalf("expected malformed messages untouched,\n got  %s\n want %s", got, want)
		}
	}
}

// TestEnsureReasoning_NullReasoningTreatedAsAbsent verifies reasoning_content:null is
// treated as absent (EDGE-01 fix: a serializer emitting null for an unset field must
// not spuriously arm thinking). Without any other thinking signal, the request is
// left untouched.
func TestEnsureReasoning_NullReasoningTreatedAsAbsent(t *testing.T) {
	body := []byte(`{
		"model":"deepseek-chat",
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"a","reasoning_content":null},
			{"role":"assistant","content":"b"}
		]
	}`)

	out := EnsureReasoningContentInAssistantMessages(body)

	// Null reasoning_content does NOT activate thinking (no other signal present),
	// so the request is left byte-identical.
	if got, want := string(out), string(body); got != want {
		t.Fatalf("expected null reasoning_content to be treated as absent (no patch),\n got  %s\n want %s", got, want)
	}
}

// TestEnsureReasoning_NullEffortTreatedAsAbsent verifies reasoning_effort:null does not
// arm signal 1 (EDGE-02 fix).
func TestEnsureReasoning_NullEffortTreatedAsAbsent(t *testing.T) {
	body := []byte(`{
		"model":"deepseek-chat",
		"reasoning_effort":null,
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"a"},
			{"role":"assistant","content":"b"}
		]
	}`)

	out := EnsureReasoningContentInAssistantMessages(body)

	if got, want := string(out), string(body); got != want {
		t.Fatalf("expected null reasoning_effort to be treated as absent (no patch),\n got  %s\n want %s", got, want)
	}
}

// TestEnsureReasoning_NullToolsTreatedAsAbsent verifies tools:null does not let signal 3
// fire on tool_calls (EDGE-02 fix).
func TestEnsureReasoning_NullToolsTreatedAsAbsent(t *testing.T) {
	body := []byte(`{
		"model":"deepseek-chat",
		"tools":null,
		"messages":[
			{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"read","arguments":"{}"}}]}
		]
	}`)

	out := EnsureReasoningContentInAssistantMessages(body)

	// tools:null means signal 3 must not fire; without any thinking signal, untouched.
	if got, want := string(out), string(body); got != want {
		t.Fatalf("expected null tools to be treated as absent (no patch),\n got  %s\n want %s", got, want)
	}
}

// TestEnsureReasoning_NullReasoningNormalizedWhenActive verifies that when a thinking
// signal IS present (reasoning_effort), a reasoning_content:null message is normalized
// to "" (P6 finding: the rebuild predicate must align with presentValue, otherwise a
// null-valued field is flagged missing but left untouched, and DeepSeek may reject null).
func TestEnsureReasoning_NullReasoningNormalizedWhenActive(t *testing.T) {
	body := []byte(`{
		"model":"deepseek-chat",
		"reasoning_effort":"high",
		"messages":[
			{"role":"assistant","content":"a","reasoning_content":null}
		]
	}`)

	out := EnsureReasoningContentInAssistantMessages(body)

	if got := gjson.GetBytes(out, "messages.0.reasoning_content").Raw; got != `""` {
		t.Fatalf("expected reasoning_content:null normalized to empty string when thinking active, got %q; output=%s", got, out)
	}
}

// TestEnsureReasoning_BackwardPatch verifies that a signal at index k also patches
// earlier missing assistant messages (backward-patch semantics).
func TestEnsureReasoning_BackwardPatch(t *testing.T) {
	body := []byte(`{
		"model":"deepseek-chat",
		"reasoning_effort":"high",
		"messages":[
			{"role":"assistant","content":"early"},
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"late","reasoning_content":"r"}
		]
	}`)

	out := EnsureReasoningContentInAssistantMessages(body)

	// The early assistant message (index 0) lacks reasoning_content and must be patched.
	if !gjson.GetBytes(out, "messages.0.reasoning_content").Exists() {
		t.Fatalf("expected early assistant message patched (backward patch), got missing; output=%s", out)
	}
	// The user message must be untouched.
	if gjson.GetBytes(out, "messages.1.reasoning_content").Exists() {
		t.Fatalf("user message must not be patched; output=%s", out)
	}
}

// TestEnsureReasoning_Deterministic verifies repeated calls produce identical output.
func TestEnsureReasoning_Deterministic(t *testing.T) {
	body := []byte(`{
		"model":"deepseek-chat",
		"tools":[{"type":"function","function":{"name":"read","description":"r","parameters":{"type":"object","properties":{}}}}],
		"messages":[
			{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"read","arguments":"{}"}}]},
			{"role":"user","content":"hi"}
		]
	}`)

	first := EnsureReasoningContentInAssistantMessages(body)
	for i := 0; i < 3; i++ {
		out := EnsureReasoningContentInAssistantMessages(body)
		if got, want := string(out), string(first); got != want {
			t.Fatalf("non-deterministic output at iteration %d,\n got  %s\n want %s", i, got, want)
		}
	}
}

// TestEnsureReasoning_LargeConversation verifies functional correctness on a large
// multi-turn tool-calling conversation (backward patch across many messages).
func TestEnsureReasoning_LargeConversation(t *testing.T) {
	var sb []byte
	sb = append(sb, `{"model":"deepseek-chat","tools":[{"type":"function","function":{"name":"read","description":"r","parameters":{"type":"object","properties":{}}}}],"messages":[`...)
	for i := 0; i < 1000; i++ {
		if i > 0 {
			sb = append(sb, ',')
		}
		if i%3 == 0 {
			sb = append(sb, []byte(`{"role":"user","content":"u`)...)
			sb = append(sb, []byte(string(rune('a'+i%26)))...)
			sb = append(sb, []byte(`"}`)...)
		} else {
			sb = append(sb, []byte(`{"role":"assistant","content":null,"tool_calls":[{"id":"c`)...)
			sb = append(sb, []byte(fmt.Sprintf("%d", i))...)
			sb = append(sb, []byte(`","type":"function","function":{"name":"read","arguments":"{}"}}]}`)...)
		}
	}
	sb = append(sb, `]}`...)
	body := sb

	out := EnsureReasoningContentInAssistantMessages(body)

	// Every assistant message must be patched with reasoning_content.
	asstPatched := 0
	asstTotal := 0
	gjson.GetBytes(out, "messages").ForEach(func(_, m gjson.Result) bool {
		if m.Get("role").String() == "assistant" {
			asstTotal++
			if m.Get("reasoning_content").Exists() {
				asstPatched++
			}
		}
		return true
	})
	if asstTotal == 0 {
		t.Fatalf("expected assistant messages in output")
	}
	if asstPatched != asstTotal {
		t.Fatalf("expected all %d assistant messages patched, got %d; output len=%d", asstTotal, asstPatched, len(out))
	}
}

// genReasoningBenchBody builds a synthetic conversation body for benchmarking.
func genReasoningBenchBody(msgCount int) []byte {
	s := `{"model":"deepseek-v4-flash","reasoning_effort":"high","tools":[{"type":"function","function":{"name":"read","description":"r","parameters":{"type":"object","properties":{}}}}],"messages":[`
	for i := 0; i < msgCount; i++ {
		if i > 0 {
			s += ","
		}
		if i%3 == 0 {
			s += `{"role":"user","content":"msg` + fmt.Sprint(i) + `"}`
		} else if i%3 == 1 {
			s += `{"role":"assistant","content":"ans` + fmt.Sprint(i) + `","reasoning_content":"think` + fmt.Sprint(i) + `"}`
		} else {
			s += `{"role":"assistant","content":null,"tool_calls":[{"id":"c` + fmt.Sprint(i) + `","type":"function","function":{"name":"read","arguments":"{}"}}]}`
		}
	}
	s += `]}`
	return []byte(s)
}

// BenchmarkEnsureReasoningContent_200 benchmarks the linear rebuild path at 200 messages.
func BenchmarkEnsureReasoningContent_200(b *testing.B) { benchReasoningContent(b, 200) }

// BenchmarkEnsureReasoningContent_500 benchmarks the linear rebuild path at 500 messages.
func BenchmarkEnsureReasoningContent_500(b *testing.B) { benchReasoningContent(b, 500) }

// BenchmarkEnsureReasoningContent_1000 benchmarks the linear rebuild path at 1000 messages.
func BenchmarkEnsureReasoningContent_1000(b *testing.B) { benchReasoningContent(b, 1000) }

// BenchmarkEnsureReasoningContent_NoEffort_1000 benchmarks the signal-2/3 detection
// fast path (no reasoning_effort, no thinking signal) at 1000 messages, verifying the
// early-exit scan stays cheap on non-thinking DeepSeek requests.
func BenchmarkEnsureReasoningContent_NoEffort_1000(b *testing.B) {
	body := genReasoningBenchBodyNoEffort(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EnsureReasoningContentInAssistantMessages(body)
	}
}

func benchReasoningContent(b *testing.B, n int) {
	body := genReasoningBenchBody(n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EnsureReasoningContentInAssistantMessages(body)
	}
}

// genReasoningBenchBodyNoEffort builds a conversation body without reasoning_effort
// and without any thinking signal (plain user/assistant turns), exercising the
// signal-2/3 detection fast path that returns the body unchanged.
func genReasoningBenchBodyNoEffort(msgCount int) []byte {
	s := `{"model":"deepseek-v4-flash","messages":[`
	for i := 0; i < msgCount; i++ {
		if i > 0 {
			s += ","
		}
		if i%2 == 0 {
			s += `{"role":"user","content":"msg` + fmt.Sprint(i) + `"}`
		} else {
			s += `{"role":"assistant","content":"ans` + fmt.Sprint(i) + `"}`
		}
	}
	s += `]}`
	return []byte(s)
}
