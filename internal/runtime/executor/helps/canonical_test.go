package helps

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/tidwall/gjson"
)

func TestCanonical_IdenticalInputProducesIdenticalOutput(t *testing.T) {
	input := []byte(`{"model":"deepseek","messages":[{"role":"user","content":"hello"}]}`)
	out1 := CanonicalizeJSON(input)
	out2 := CanonicalizeJSON(input)
	if string(out1) != string(out2) {
		t.Fatalf("identical input must produce identical output:\n  %s\n  %s", string(out1), string(out2))
	}
}

func TestCanonical_DifferentWhitespaceProducesSameOutput(t *testing.T) {
	a := []byte(`{"model":"deepseek","messages":[{"role":"user","content":"hello"}]}`)
	b := []byte(`{"model":"deepseek", "messages":[{"role":"user", "content":"hello"}]}`)
	outA := CanonicalizeJSON(a)
	outB := CanonicalizeJSON(b)
	if string(outA) != string(outB) {
		t.Fatalf("different whitespace must produce same output:\n  %s\n  %s", string(outA), string(outB))
	}
}

func TestCanonical_DifferentKeyOrderProducesSameOutput(t *testing.T) {
	a := []byte(`{"a":1,"b":2}`)
	b := []byte(`{"b":2,"a":1}`)
	outA := CanonicalizeJSON(a)
	outB := CanonicalizeJSON(b)
	if string(outA) != string(outB) {
		t.Fatalf("different key order must produce same output:\n  %s\n  %s", string(outA), string(outB))
	}
}

func TestCanonical_NestedStructures(t *testing.T) {
	// Two semantically identical bodies with different serialization
	a := []byte(`{"model":"deepseek","messages":[{"role":"system","content":[{"type":"text","text":"You are Claude."}]},{"role":"user","content":"hi"}]}`)
	b := []byte(`{"messages":[{"content":[{"text":"You are Claude.","type":"text"}],"role":"system"},{"content":"hi","role":"user"}],"model":"deepseek"}`)
	outA := CanonicalizeJSON(a)
	outB := CanonicalizeJSON(b)
	if string(outA) != string(outB) {
		t.Fatalf("semantically identical nested JSON must produce same output:\n  %s\n  %s", string(outA), string(outB))
	}
}

func TestCanonical_PreservesValidation(t *testing.T) {
	input := []byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello"}],"stream":true}`)
	out := CanonicalizeJSON(input)
	if !bytes.Contains(out, []byte(`"deepseek-v4-flash"`)) {
		t.Fatalf("model name must be preserved: %s", string(out))
	}
	if !bytes.Contains(out, []byte(`"stream":true`)) {
		t.Fatalf("stream field must be preserved: %s", string(out))
	}
	if !bytes.Contains(out, []byte(`"hello"`)) {
		t.Fatalf("message content must be preserved: %s", string(out))
	}
}

func TestCanonical_InvalidJSONFallback(t *testing.T) {
	input := []byte(`{invalid}`)
	out := CanonicalizeJSON(input)
	if string(out) != string(input) {
		t.Fatalf("invalid JSON must be returned unchanged, got %s", string(out))
	}
}

func TestCanonical_EmptyBody(t *testing.T) {
	input := []byte(``)
	out := CanonicalizeJSON(input)
	if string(out) != string(input) {
		t.Fatalf("empty body must be returned unchanged, got %s", string(out))
	}
}

func TestCanonical_RealWorldBody(t *testing.T) {
	input := []byte(`{"model":"deepseek-v4-flash","messages":[{"role":"system","content":"You are Claude."},{"role":"system","content":"PreToolUse:Read hook: read in parallel."},{"role":"user","content":[{"type":"text","text":"fix the bug"}]},{"role":"assistant","content":"ok"},{"role":"tool","content":"file contents here"}],"stream":true,"stream_options":{"include_usage":true}}`)
	out := CanonicalizeJSON(input)
	if !json.Valid(out) {
		t.Fatalf("output must be valid JSON: %s", string(out))
	}
	out2 := CanonicalizeJSON(input)
	if string(out) != string(out2) {
		t.Fatalf("canonicalize must be deterministic")
	}
}

func TestCanonical_NormalizeThenCanonical(t *testing.T) {
	ptuText := `<system-reminder>
PreToolUse:Read hook additional context: Read multiple files in parallel when possible for faster analysis.
</system-reminder>`
	body := []byte(`{"model":"deepseek-v4-flash","messages":[{"role":"system","content":"You are Claude."},{"role":"user","content":[{"type":"text","text":"` + ptuText + `"}]},{"role":"assistant","content":"done"}]}`)
	normalized := NormalizePreToolUseMessages(body)
	canonical := CanonicalizeJSON(normalized)
	if !json.Valid(canonical) {
		t.Fatalf("normalize+canonicalize must produce valid JSON: %s", string(canonical))
	}
	canonical2 := CanonicalizeJSON(normalized)
	if string(canonical) != string(canonical2) {
		t.Fatalf("normalize+canonicalize must be deterministic")
	}
}

func TestToolsort_SortsAlphabetically(t *testing.T) {
	body := []byte(`{"tools":[{"type":"function","function":{"name":"zebra","description":"z"}},{"type":"function","function":{"name":"apple","description":"a"}},{"type":"function","function":{"name":"moon","description":"m"}}]}`)
	out := SortToolsByName(body)
	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	names := gjson.GetBytes(out, "tools.#.function.name").Array()
	expected := []string{"apple", "moon", "zebra"}
	for i, want := range expected {
		got := names[i].String()
		if got != want {
			t.Fatalf("position %d: expected %s, got %s", i, want, got)
		}
	}
}

func TestToolsort_NoTools(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	out := SortToolsByName(body)
	if string(out) != string(body) {
		t.Fatalf("expected no change")
	}
}

func TestToolsort_AlreadySorted(t *testing.T) {
	body := []byte(`{"tools":[{"type":"function","function":{"name":"a"}},{"type":"function","function":{"name":"b"}}]}`)
	out := SortToolsByName(body)
	// Should be unchanged (already sorted)
	if string(out) != string(body) {
		t.Fatalf("already sorted should not change: got %s", string(out))
	}
}

// --- ReorderJSONForCache tests ---

func TestReorder_Basic(t *testing.T) {
	body := []byte(`{"stream":true,"messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"test"}}],"model":"deepseek-v4-flash","stream_options":{"include_usage":true},"reasoning_effort":"xhigh"}`)
	out := ReorderJSONForCache(body)
	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	outStr := string(out)
	// Verify order: model first, tools before messages, messages last among known keys
	modelIdx := bytes.Index(out, []byte(`"model"`))
	toolsIdx := bytes.Index(out, []byte(`"tools"`))
	msgsIdx := bytes.Index(out, []byte(`"messages"`))
	streamIdx := bytes.Index(out, []byte(`"stream"`))
	if modelIdx < 0 || toolsIdx < 0 || msgsIdx < 0 {
		t.Fatalf("missing expected keys: %s", outStr)
	}
	if modelIdx > toolsIdx {
		t.Fatalf("model should be before tools: %s", outStr)
	}
	if toolsIdx > msgsIdx {
		t.Fatalf("tools should be before messages: %s", outStr)
	}
	if streamIdx > msgsIdx {
		t.Fatalf("stream* should be before messages: %s", outStr)
	}
}

func TestReorder_MessagesLast(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hello"}],"model":"test"}`)
	out := ReorderJSONForCache(body)
	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	outStr := string(out)
	modelIdx := bytes.Index(out, []byte(`"model"`))
	msgsIdx := bytes.Index(out, []byte(`"messages"`))
	if modelIdx > msgsIdx {
		t.Fatalf("messages must be last among known keys: %s", outStr)
	}
}

func TestReorder_NoTools(t *testing.T) {
	body := []byte(`{"model":"test","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	out := ReorderJSONForCache(body)
	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	outStr := string(out)
	// messages must still be last among known keys
	modelIdx := bytes.Index(out, []byte(`"model"`))
	msgsIdx := bytes.Index(out, []byte(`"messages"`))
	if modelIdx > msgsIdx {
		t.Fatalf("messages must be last: %s", outStr)
	}
}

func TestReorder_NoMessages(t *testing.T) {
	// count_tokens requests may have no messages key
	body := []byte(`{"model":"deepseek-v4-flash"}`)
	out := ReorderJSONForCache(body)
	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
}

func TestReorder_UnknownKeysAfterMessages(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hi"}],"model":"test","custom_field":"value","another":123}`)
	out := ReorderJSONForCache(body)
	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	outStr := string(out)
	msgsIdx := bytes.Index(out, []byte(`"messages"`))
	customIdx := bytes.Index(out, []byte(`"custom_field"`))
	anotherIdx := bytes.Index(out, []byte(`"another"`))
	if msgsIdx > customIdx || msgsIdx > anotherIdx {
		t.Fatalf("unknown keys must come after messages: %s", outStr)
	}
}

func TestReorder_Idempotent(t *testing.T) {
	body := []byte(`{"stream":true,"model":"test","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"f"}}]}`)
	out1 := ReorderJSONForCache(body)
	out2 := ReorderJSONForCache(out1)
	if string(out1) != string(out2) {
		t.Fatalf("must be idempotent:\n  %s\n  %s", string(out1), string(out2))
	}
}

func TestReorder_PreservesContent(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-flash","messages":[{"role":"system","content":[{"type":"text","text":"You are Claude."}]},{"role":"user","content":"hello"},{"role":"assistant","content":"hi","tool_calls":[{"id":"c1","type":"function","function":{"name":"Bash","arguments":"{}"}}]}],"tools":[{"type":"function","function":{"name":"Bash","description":"Execute a shell command","parameters":{"type":"object","properties":{"command":{"type":"string"}}}}}],"stream":true,"reasoning_effort":"xhigh"}`)
	out := ReorderJSONForCache(body)
	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	if !bytes.Contains(out, []byte(`"deepseek-v4-flash"`)) {
		t.Fatalf("model name lost")
	}
	if !bytes.Contains(out, []byte(`"You are Claude."`)) {
		t.Fatalf("system prompt lost")
	}
	if !bytes.Contains(out, []byte(`"Bash"`)) {
		t.Fatalf("tool name lost")
	}
	if !bytes.Contains(out, []byte(`"Execute a shell command"`)) {
		t.Fatalf("tool description lost")
	}
	if !bytes.Contains(out, []byte(`"xhigh"`)) {
		t.Fatalf("reasoning_effort lost")
	}
	if !bytes.Contains(out, []byte(`"tool_calls"`)) {
		t.Fatalf("assistant tool_calls lost")
	}
}

func TestReorder_InvalidJSONFallback(t *testing.T) {
	body := []byte(`{invalid}`)
	out := ReorderJSONForCache(body)
	if string(out) != string(body) {
		t.Fatalf("invalid JSON must be returned unchanged, got %s", string(out))
	}
}

func TestReorder_EmptyBody(t *testing.T) {
	body := []byte(``)
	out := ReorderJSONForCache(body)
	if string(out) != string(body) {
		t.Fatalf("empty body must be returned unchanged, got %s", string(out))
	}
}

func TestReorder_WithSortTools(t *testing.T) {
	// Verify SortToolsByName + ReorderJSONForCache work together
	body := []byte(`{"model":"test","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"z"}},{"type":"function","function":{"name":"a"}}]}`)
	sorted := SortToolsByName(body)
	out := ReorderJSONForCache(sorted)
	if !json.Valid(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	outStr := string(out)
	// tools should be before messages
	toolsIdx := bytes.Index(out, []byte(`"tools"`))
	msgsIdx := bytes.Index(out, []byte(`"messages"`))
	if toolsIdx > msgsIdx {
		t.Fatalf("tools must come before messages: %s", outStr)
	}
	// tools should be sorted: a before z
	aIdx := bytes.Index(out, []byte(`"a"`))
	zIdx := bytes.Index(out, []byte(`"z"`))
	if aIdx < 0 || zIdx < 0 {
		t.Fatalf("tool names missing: %s", outStr)
	}
	if aIdx > zIdx {
		t.Fatalf("tools must be sorted alphabetically: %s", outStr)
	}
}

func TestReorder_DeterministicAcrossEquivalentInputs(t *testing.T) {
	a := []byte(`{"messages":[{"role":"user","content":"hi"}],"model":"test","tools":[{"type":"function","function":{"name":"f"}}],"stream":true}`)
	b := []byte(`{"stream":true,"tools":[{"function":{"name":"f"},"type":"function"}],"model":"test","messages":[{"content":"hi","role":"user"}]}`)
	outA := ReorderJSONForCache(a)
	outB := ReorderJSONForCache(b)
	if string(outA) != string(outB) {
		t.Fatalf("semantically equivalent inputs must produce identical output:\n  A: %s\n  B: %s", string(outA), string(outB))
	}
}
