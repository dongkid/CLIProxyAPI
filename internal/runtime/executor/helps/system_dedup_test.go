package helps

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestDedup_NoMessages(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-flash"}`)
	out := DeduplicateSystemMessages(body)
	if string(out) != string(body) {
		t.Fatalf("expected no change, got %s", string(out))
	}
}

func TestDedup_EmptyMessages(t *testing.T) {
	body := []byte(`{"messages":[]}`)
	out := DeduplicateSystemMessages(body)
	if string(out) != string(body) {
		t.Fatalf("expected no change, got %s", string(out))
	}
}

func TestDedup_NoSystem(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"}]}`)
	out := DeduplicateSystemMessages(body)
	if string(out) != string(body) {
		t.Fatalf("expected no change, got %s", string(out))
	}
}

func TestDedup_SingleSystem(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"be helpful"},{"role":"user","content":"hello"}]}`)
	out := DeduplicateSystemMessages(body)
	if string(out) != string(body) {
		t.Fatalf("expected no change, got %s", string(out))
	}
}

func TestDedup_NoDuplicate(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"A"},{"role":"user","content":"hello"},{"role":"system","content":"B"}]}`)
	out := DeduplicateSystemMessages(body)
	if string(out) != string(body) {
		t.Fatalf("expected no change, got %s", string(out))
	}
}

func TestDedup_ConsecutiveIdentical(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"A"},{"role":"system","content":"A"},{"role":"system","content":"A"}]}`)
	out := DeduplicateSystemMessages(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 1 {
		t.Fatalf("expected 1 message, got %d: %s", count, string(out))
	}
	if gjson.GetBytes(out, "messages.0.role").String() != "system" {
		t.Fatalf("expected role=system, got %s", gjson.GetBytes(out, "messages.0.role").String())
	}
}

func TestDedup_MixedPattern(t *testing.T) {
	// [sys:A][sys:A][sys:A][usr][sys:B][sys:B] → [sys:A][usr][sys:B]
	body := []byte(`{"messages":[{"role":"system","content":"A"},{"role":"system","content":"A"},{"role":"system","content":"A"},{"role":"user","content":"hello"},{"role":"system","content":"B"},{"role":"system","content":"B"}]}`)
	out := DeduplicateSystemMessages(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 3 {
		t.Fatalf("expected 3 messages, got %d: %s", count, string(out))
	}
	roles := make([]string, 0, count)
	for i := 0; i < int(count); i++ {
		role := gjson.GetBytes(out, fmt.Sprintf("messages.%d.role", i)).String()
		roles = append(roles, role)
	}
	expected := []string{"system", "user", "system"}
	for i, r := range expected {
		if roles[i] != r {
			t.Fatalf("position %d: expected role=%q, got %q", i, r, roles[i])
		}
	}
}

func TestDedup_NonConsecutiveDuplicatesRemoved(t *testing.T) {
	// [sys:A][usr][sys:A] → [sys:A][usr] (global dedup removes 2nd copy)
	body := []byte(`{"messages":[{"role":"system","content":"A"},{"role":"user","content":"hello"},{"role":"system","content":"A"}]}`)
	out := DeduplicateSystemMessages(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 2 {
		t.Fatalf("expected 2 messages, got %d: %s", count, string(out))
	}
	role1 := gjson.GetBytes(out, "messages.0.role").String()
	role2 := gjson.GetBytes(out, "messages.1.role").String()
	if role1 != "system" || role2 != "user" {
		t.Fatalf("expected [system, user], got [%s, %s]", role1, role2)
	}
}

func TestDedup_KeepsUniqueSerialSystemMessages(t *testing.T) {
	// [sys:A][sys:B][sys:C] → unchanged (all unique)
	body := []byte(`{"messages":[{"role":"system","content":"A"},{"role":"system","content":"B"},{"role":"system","content":"C"}]}`)
	out := DeduplicateSystemMessages(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 3 {
		t.Fatalf("expected 3 messages (all unique), got %d: %s", count, string(out))
	}
}

func TestDedup_ContentString(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"The task tools haven't been used recently."},{"role":"system","content":"The task tools haven't been used recently."},{"role":"system","content":"The task tools haven't been used recently."}]}`)
	out := DeduplicateSystemMessages(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 1 {
		t.Fatalf("expected 1 message, got %d: %s", count, string(out))
	}
}

func TestDedup_ContentArray(t *testing.T) {
	// System messages with array content
	body := []byte(`{"messages":[{"role":"system","content":[{"type":"text","text":"instructions"}]},{"role":"system","content":[{"type":"text","text":"instructions"}]}]}`)
	out := DeduplicateSystemMessages(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 1 {
		t.Fatalf("expected 1 message, got %d: %s", count, string(out))
	}
}

func TestDedup_ContentArrayDifferent(t *testing.T) {
	// Array content that differs → no dedup
	body := []byte(`{"messages":[{"role":"system","content":[{"type":"text","text":"A"}]},{"role":"system","content":[{"type":"text","text":"B"}]}]}`)
	out := DeduplicateSystemMessages(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 2 {
		t.Fatalf("expected 2 messages (different content), got %d: %s", count, string(out))
	}
}

func TestDedup_RealWorldPattern(t *testing.T) {
	// Simulating the actual CC pattern: MCP + multiple identical task reminders (consecutive)
	body := []byte(`{"model":"deepseek","messages":[{"role":"system","content":"# MCP Server Instructions\n\nUse context7 for docs."},{"role":"system","content":"The task tools haven't been used recently. If you're working on tasks that would benefit from tracking progress, consider using TaskCreate."},{"role":"system","content":"The task tools haven't been used recently. If you're working on tasks that would benefit from tracking progress, consider using TaskCreate."},{"role":"system","content":"The task tools haven't been used recently. If you're working on tasks that would benefit from tracking progress, consider using TaskCreate."},{"role":"user","content":"hello"}]}`)
	out := DeduplicateSystemMessages(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 3 {
		t.Fatalf("expected 3 messages (MCP + TASK + user), got %d: %s", count, string(out))
	}
}

func TestDedup_ScatteredDuplicates(t *testing.T) {
	// [sys:A][usr][asst][tool][sys:A][usr][sys:A]
	// → [sys:A][usr][asst][tool][usr] (indices 4 and 6 removed, 7→5)
	body := []byte(`{"messages":[{"role":"system","content":"A"},{"role":"user","content":"u1"},{"role":"assistant","content":"a1"},{"role":"tool","content":"t1"},{"role":"system","content":"A"},{"role":"user","content":"u2"},{"role":"system","content":"A"}]}`)
	out := DeduplicateSystemMessages(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 5 {
		t.Fatalf("expected 5 messages, got %d: %s", count, string(out))
	}
	// First message should still be the system message
	if gjson.GetBytes(out, "messages.0.role").String() != "system" {
		t.Fatalf("position 0: expected system, got %s", gjson.GetBytes(out, "messages.0.role").String())
	}
	// No other system messages should remain
	for i := 1; i < int(count); i++ {
		role := gjson.GetBytes(out, fmt.Sprintf("messages.%d.role", i)).String()
		if role == "system" {
			t.Fatalf("position %d: unexpected system message (should have been deduped)", i)
		}
	}
}

// --- AppendEphemeralSystemMessages tests ---

func TestEph_NoEphemeral(t *testing.T) {
	// No PreToolUse messages → no change
	body := []byte(`{"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"hello"}]}`)
	out := AppendEphemeralSystemMessages(body)
	if string(out) != string(body) {
		t.Fatalf("expected no change, got %s", string(out))
	}
}

func TestEph_SingleEphemeral(t *testing.T) {
	// One PreToolUse in the middle → moved to end
	body := []byte(`{"messages":[{"role":"user","content":"u1"},{"role":"system","content":"PreToolUse:Read hook: read in parallel."},{"role":"assistant","content":"a1"}]}`)
	out := AppendEphemeralSystemMessages(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 3 {
		t.Fatalf("expected 3 messages, got %d: %s", count, string(out))
	}
	// Ephemeral should be at the last position
	lastIdx := count - 1
	lastRole := gjson.GetBytes(out, fmt.Sprintf("messages.%d.role", lastIdx)).String()
	if lastRole != "system" {
		t.Fatalf("expected last message role=system, got %s", lastRole)
	}
	lastContent := gjson.GetBytes(out, fmt.Sprintf("messages.%d.content", lastIdx)).String()
	if !strings.Contains(lastContent, "PreToolUse:") {
		t.Fatalf("expected last message to contain PreToolUse:, got %s", lastContent)
	}
}

func TestEph_MultipleEphemeral(t *testing.T) {
	// Multiple PreToolUse scattered → all at end, preserve order
	body := []byte(`{"messages":[{"role":"user","content":"u1"},{"role":"system","content":"PreToolUse:Read hook: read in parallel."},{"role":"assistant","content":"a1"},{"role":"system","content":"PreToolUse:Edit hook: verify changes."},{"role":"user","content":"u2"}]}`)
	out := AppendEphemeralSystemMessages(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 5 {
		t.Fatalf("expected 5 messages, got %d: %s", count, string(out))
	}
	// Both ephemeral should be at the end
	last := gjson.GetBytes(out, fmt.Sprintf("messages.%d.content", count-1)).String()
	secondLast := gjson.GetBytes(out, fmt.Sprintf("messages.%d.content", count-2)).String()
	if !strings.Contains(secondLast, "PreToolUse:") || !strings.Contains(last, "PreToolUse:") {
		t.Fatalf("expected last two messages to be ephemeral, got [%s, %s]", secondLast, last)
	}
	// Order preserved: Read before Edit
	if !strings.Contains(secondLast, "Read") || !strings.Contains(last, "Edit") {
		t.Fatalf("expected Read before Edit, got [%s, %s]", secondLast, last)
	}
}

func TestEph_MixedSystem(t *testing.T) {
	// Mix of ephemeral and regular system messages → only ephemeral moved
	body := []byte(`{"messages":[{"role":"system","content":"You are a helpful assistant."},{"role":"user","content":"u1"},{"role":"system","content":"PreToolUse:Read hook: read in parallel."},{"role":"assistant","content":"a1"},{"role":"system","content":"The task tools haven't been used recently."}]}`)
	out := AppendEphemeralSystemMessages(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 5 {
		t.Fatalf("expected 5 messages, got %d: %s", count, string(out))
	}
	// First message should be identity (not ephemeral)
	firstRole := gjson.GetBytes(out, "messages.0.role").String()
	firstContent := gjson.GetBytes(out, "messages.0.content").String()
	if firstRole != "system" || strings.Contains(firstContent, "PreToolUse:") {
		t.Fatalf("expected first message to be identity sys, got role=%s content=%s", firstRole, firstContent)
	}
	// Last message should be the ephemeral
	lastContent := gjson.GetBytes(out, fmt.Sprintf("messages.%d.content", count-1)).String()
	if !strings.Contains(lastContent, "PreToolUse:") {
		t.Fatalf("expected last message to be ephemeral, got %s", lastContent)
	}
	// Task reminder should stay in its original position among stable messages
	taskFound := false
	afterEph := false
	for i := 0; i < int(count); i++ {
		c := gjson.GetBytes(out, fmt.Sprintf("messages.%d.content", i)).String()
		if strings.Contains(c, "task tools") {
			if afterEph {
				t.Fatalf("task reminder found after ephemeral messages")
			}
			taskFound = true
		}
		if strings.Contains(c, "PreToolUse:") {
			afterEph = true
		}
	}
	if !taskFound {
		t.Fatalf("task reminder should still be present")
	}
}

func TestEph_NonStringContent(t *testing.T) {
	// PreToolUse in non-string content (e.g., array) → not moved
	body := []byte(`{"messages":[{"role":"user","content":"u1"},{"role":"system","content":[{"type":"text","text":"PreToolUse:Read hook in array"}]},{"role":"assistant","content":"a1"}]}`)
	out := AppendEphemeralSystemMessages(body)
	if string(out) != string(body) {
		t.Fatalf("expected no change for non-string content, got %s", string(out))
	}
}

func TestEph_NoMessages(t *testing.T) {
	body := []byte(`{"model":"test"}`)
	out := AppendEphemeralSystemMessages(body)
	if string(out) != string(body) {
		t.Fatalf("expected no change, got %s", string(out))
	}
}

func TestEph_AllEphemeral(t *testing.T) {
	// All messages are ephemeral → order unchanged
	body := []byte(`{"messages":[{"role":"system","content":"PreToolUse:A"},{"role":"system","content":"PreToolUse:B"},{"role":"system","content":"PreToolUse:C"}]}`)
	out := AppendEphemeralSystemMessages(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 3 {
		t.Fatalf("expected 3 messages, got %d: %s", count, string(out))
	}
	// Order should be preserved
	content0 := gjson.GetBytes(out, "messages.0.content").String()
	content1 := gjson.GetBytes(out, "messages.1.content").String()
	content2 := gjson.GetBytes(out, "messages.2.content").String()
	if !strings.Contains(content0, "PreToolUse:A") || !strings.Contains(content1, "PreToolUse:B") || !strings.Contains(content2, "PreToolUse:C") {
		t.Fatalf("expected order A,B,C, got [%s, %s, %s]", content0, content1, content2)
	}
}

func TestEph_ValidJSON(t *testing.T) {
	body := []byte(`{"model":"test","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"hello"},{"role":"system","content":"PreToolUse:Read hook."},{"role":"user","content":"next"}]}`)
	if !gjson.ValidBytes(body) {
		t.Fatalf("input is not valid JSON")
	}
	out := AppendEphemeralSystemMessages(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	msgs := gjson.GetBytes(out, "messages")
	if !msgs.IsArray() {
		t.Fatalf("messages is not an array")
	}
	if len(msgs.Array()) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs.Array()))
	}
	// Last message should be the ephemeral one
	lastContent := gjson.GetBytes(out, "messages.3.content").String()
	if !strings.Contains(lastContent, "PreToolUse:") {
		t.Fatalf("expected last message to contain PreToolUse:, got %s", lastContent)
	}
}

func TestEph_Phase1ThenEph(t *testing.T) {
	// Phase 1 dedup first, then append ephemeral
	body := []byte(`{"messages":[{"role":"user","content":"u1"},{"role":"system","content":"PreToolUse:Read hook: read in parallel."},{"role":"system","content":"PreToolUse:Read hook: read in parallel."},{"role":"assistant","content":"a1"},{"role":"system","content":"PreToolUse:Edit hook: verify changes."}]}`)
	// Phase 1
	out := DeduplicateSystemMessages(body)
	dedupCount := gjson.GetBytes(out, "messages.#").Int()
	if dedupCount != 4 {
		t.Fatalf("Phase 1: expected 4 messages, got %d: %s", dedupCount, string(out))
	}
	// Phase 2.5
	out = AppendEphemeralSystemMessages(out)
	ephCount := gjson.GetBytes(out, "messages.#").Int()
	if ephCount != 4 {
		t.Fatalf("Phase 2.5: expected 4 messages, got %d: %s", ephCount, string(out))
	}
	// Last two should be the two ephemeral messages (Read, then Edit)
	lastTwo := gjson.GetBytes(out, "messages.2.content").String()
	lastOne := gjson.GetBytes(out, "messages.3.content").String()
	if !strings.Contains(lastTwo, "Read") || !strings.Contains(lastOne, "Edit") {
		t.Fatalf("expected Read then Edit at end, got [%s, %s]", lastTwo, lastOne)
	}
}
