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
