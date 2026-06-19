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

// --- NormalizePreToolUseMessages tests ---

func TestNorm_NoMessages(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-flash"}`)
	out := NormalizePreToolUseMessages(body)
	if string(out) != string(body) {
		t.Fatalf("expected no change, got %s", string(out))
	}
}

func TestNorm_NoUserPTU(t *testing.T) {
	// User string content → normalized to array format for byte-stability
	body := []byte(`{"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"}]}`)
	out := NormalizePreToolUseMessages(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 2 {
		t.Fatalf("expected 2 messages, got %d: %s", count, string(out))
	}
	// User msg content normalized to array
	c0 := gjson.GetBytes(out, "messages.0.content")
	if !c0.IsArray() || c0.Get("0.text").String() != "hello" {
		t.Fatalf("expected user content as array, got %s", c0.Raw)
	}
}

func TestNorm_SingleUserPTUExtracted(t *testing.T) {
	// Single user-role message with <system-reminder> wrapped PTU
	ptuText := `<system-reminder>
PreToolUse:Read hook additional context: Read multiple files in parallel when possible for faster analysis.
</system-reminder>`
	body := []byte(`{"messages":[{"role":"assistant","content":"ok"},{"role":"user","content":[{"type":"text","text":"` + ptuText + `"}]},{"role":"assistant","content":"done"}]}`)
	out := NormalizePreToolUseMessages(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	count := gjson.GetBytes(out, "messages.#").Int()
	// Original 3 messages, user PTU deleted (-1), system PTU inserted at same position (+1) → 3
	if count != 3 {
		t.Fatalf("expected 3 messages, got %d: %s", count, string(out))
	}
	// PTU should be at position 1 (replacing the deleted user message)
	ptu := gjson.GetBytes(out, "messages.1")
	if ptu.Get("role").String() != "system" {
		t.Fatalf("expected messages.1 role=system, got %s", ptu.Get("role").String())
	}
	if !strings.Contains(ptu.Get("content").String(), "PreToolUse:Read hook") {
		t.Fatalf("expected messages.1 to contain PreToolUse:Read hook, got %s", ptu.Get("content").String())
	}
	// Content should NOT have <system-reminder> wrapper
	if strings.Contains(ptu.Get("content").String(), "<system-reminder>") {
		t.Fatalf("extracted PTU should not contain <system-reminder> wrapper")
	}
	// Assistant messages preserved at original positions
	if gjson.GetBytes(out, "messages.0.content").String() != "ok" {
		t.Fatalf("expected messages.0 to be 'ok', got %s", gjson.GetBytes(out, "messages.0.content").String())
	}
	if gjson.GetBytes(out, "messages.2.content").String() != "done" {
		t.Fatalf("expected messages.2 to be 'done', got %s", gjson.GetBytes(out, "messages.2.content").String())
	}
}

func TestNorm_MultipleUserPTUDeduped(t *testing.T) {
	// Two identical user PTU messages → extracted, deduped to one, inserted at first deletion position
	ptuText := `<system-reminder>
PreToolUse:Read hook additional context: Read multiple files in parallel when possible for faster analysis.
</system-reminder>`
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"` + ptuText + `"}]},{"role":"assistant","content":"mid"},{"role":"user","content":[{"type":"text","text":"` + ptuText + `"}]},{"role":"assistant","content":"end"}]}`)
	out := NormalizePreToolUseMessages(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	count := gjson.GetBytes(out, "messages.#").Int()
	// Original 4, 2 user PTU deleted (-2), 1 system PTU inserted at first deletion position (+1) → 3
	if count != 3 {
		t.Fatalf("expected 3 messages, got %d: %s", count, string(out))
	}
	// System PTU at position 0 (first deleted user PTU, not at end)
	ptu := gjson.GetBytes(out, "messages.0")
	if ptu.Get("role").String() != "system" {
		t.Fatalf("expected messages.0 role=system (PTU at first deletion), got %s", ptu.Get("role").String())
	}
	if !strings.Contains(ptu.Get("content").String(), "PreToolUse:") {
		t.Fatalf("expected messages.0 to contain PreToolUse:, got %s", ptu.Get("content").String())
	}
	// Assistant messages preserved in order after PTU
	mid := gjson.GetBytes(out, "messages.1")
	if mid.Get("role").String() != "assistant" || mid.Get("content").String() != "mid" {
		t.Fatalf("expected messages.1 to be assistant 'mid', got %s", mid.Get("content").String())
	}
	end := gjson.GetBytes(out, "messages.2")
	if end.Get("role").String() != "assistant" || end.Get("content").String() != "end" {
		t.Fatalf("expected messages.2 to be assistant 'end', got %s", end.Get("content").String())
	}
}

func TestNorm_MultiElementExtractsPTU(t *testing.T) {
	// Multi-element array: PTU + real content → system PTU at deletion pos, real content in user msg after
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"real content"},{"type":"text","text":"<system-reminder>\nPreToolUse:Read hook.\n</system-reminder>"}]},{"role":"assistant","content":"ok"}]}`)
	out := NormalizePreToolUseMessages(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	count := gjson.GetBytes(out, "messages.#").Int()
	// Original 2, user deleted → system PTU + user(real content) → 3 messages
	if count != 3 {
		t.Fatalf("expected 3 messages, got %d: %s", count, string(out))
	}
	// First message: system PTU
	if gjson.GetBytes(out, "messages.0.role").String() != "system" {
		t.Fatalf("expected messages.0 to be system PTU, got %s", gjson.GetBytes(out, "messages.0.role").String())
	}
	if !strings.Contains(gjson.GetBytes(out, "messages.0.content").String(), "PreToolUse:Read hook.") {
		t.Fatalf("expected messages.0 to contain PTU, got %s", gjson.GetBytes(out, "messages.0.content").String())
	}
	// Second message: user with real content
	if gjson.GetBytes(out, "messages.1.role").String() != "user" {
		t.Fatalf("expected messages.1 to be user, got %s", gjson.GetBytes(out, "messages.1.role").String())
	}
	if !strings.Contains(gjson.GetBytes(out, "messages.1.content.0.text").String(), "real content") {
		t.Fatalf("expected messages.1 to contain real content, got %s", gjson.GetBytes(out, "messages.1.content.0.text").String())
	}
	// Third message: assistant
	if gjson.GetBytes(out, "messages.2.content").String() != "ok" {
		t.Fatalf("expected messages.2 to be 'ok', got %s", gjson.GetBytes(out, "messages.2.content").String())
	}
}

func TestNorm_MultiElementAllPTUExtracted(t *testing.T) {
	// All elements are PTU → all extracted as system messages
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"<system-reminder>\nPreToolUse:Read hook A.\n</system-reminder>"},{"type":"text","text":"<system-reminder>\nPreToolUse:Read hook B.\n</system-reminder>"}]},{"role":"assistant","content":"ok"}]}`)
	out := NormalizePreToolUseMessages(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	count := gjson.GetBytes(out, "messages.#").Int()
	// Original 2, user deleted → 2 system PTU + assistant = 3
	if count != 3 {
		t.Fatalf("expected 3 messages, got %d: %s", count, string(out))
	}
	// Messages 0 and 1 should both be system PTU
	for i := 0; i < 2; i++ {
		if gjson.GetBytes(out, fmt.Sprintf("messages.%d.role", i)).String() != "system" {
			t.Fatalf("expected messages.%d to be system PTU, got %s", i, gjson.GetBytes(out, fmt.Sprintf("messages.%d.role", i)).String())
		}
	}
	// Assistant preserved
	if gjson.GetBytes(out, "messages.2.content").String() != "ok" {
		t.Fatalf("expected messages.2 to be 'ok', got %s", gjson.GetBytes(out, "messages.2.content").String())
	}
}

func TestNorm_MultiElementPreAndPostToolUse(t *testing.T) {
	// Real-world PreToolUse + PostToolUse in one array → PTU extracted, PostToolUse stays as user
	content := `[{"type":"text","text":"<system-reminder>\nPreToolUse:Edit hook additional context: Verify changes.\n</system-reminder>"},{"type":"text","text":"<system-reminder>\nPostToolUse:Edit hook additional context: Code modified.\n</system-reminder>"}]`
	body := []byte(`{"messages":[{"role":"user","content":` + content + `},{"role":"assistant","content":"done"}]}`)
	out := NormalizePreToolUseMessages(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	count := gjson.GetBytes(out, "messages.#").Int()
	// Original 2, user deleted → system PTU + user(PostToolUse) = 3
	if count != 3 {
		t.Fatalf("expected 3 messages, got %d: %s", count, string(out))
	}
	// Message 0: system PTU
	if gjson.GetBytes(out, "messages.0.role").String() != "system" {
		t.Fatalf("expected messages.0 to be system PTU, got %s", gjson.GetBytes(out, "messages.0.role").String())
	}
	if !strings.Contains(gjson.GetBytes(out, "messages.0.content").String(), "PreToolUse:Edit hook") {
		t.Fatalf("expected messages.0 to contain PTU, got %s", gjson.GetBytes(out, "messages.0.content").String())
	}
	// Message 1: user with PostToolUse preserved
	if gjson.GetBytes(out, "messages.1.role").String() != "user" {
		t.Fatalf("expected messages.1 to be user, got %s", gjson.GetBytes(out, "messages.1.role").String())
	}
	// Message 2: assistant
	if gjson.GetBytes(out, "messages.2.role").String() != "assistant" || gjson.GetBytes(out, "messages.2.content").String() != "done" {
		t.Fatalf("expected messages.2 to be assistant 'done', got %s", gjson.GetBytes(out, "messages.2.content").String())
	}
}

func TestNorm_NoSystemReminderPrefix(t *testing.T) {
	// User message contains PreToolUse but NOT wrapped in <system-reminder>
	// → NOT extracted (safety: real user message could contain "PreToolUse" text)
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"I found PreToolUse:Read hook in the logs"}]},{"role":"assistant","content":"ok"}]}`)
	out := NormalizePreToolUseMessages(body)
	if string(out) != string(body) {
		t.Fatalf("expected no change for non-system-reminder text, got %s", string(out))
	}
}

func TestNorm_NonTextElementIgnored(t *testing.T) {
	// User message with non-text element (e.g., image) → not processed
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"abc"}}]},{"role":"assistant","content":"ok"}]}`)
	out := NormalizePreToolUseMessages(body)
	if string(out) != string(body) {
		t.Fatalf("expected no change for non-text element, got %s", string(out))
	}
}

func TestNorm_AlreadySystemPTUUnchanged(t *testing.T) {
	// Existing system-role PTU → stays system. User string → array normalized.
	body := []byte(`{"messages":[{"role":"user","content":"hi"},{"role":"system","content":"PreToolUse:Read hook: read in parallel."},{"role":"assistant","content":"ok"}]}`)
	out := NormalizePreToolUseMessages(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	// User "hi" should be array now
	c0 := gjson.GetBytes(out, "messages.0.content")
	if !c0.IsArray() || c0.Get("0.text").String() != "hi" {
		t.Fatalf("expected user content as array, got %s", c0.Raw)
	}
	// System PTU unchanged
	if gjson.GetBytes(out, "messages.1.role").String() != "system" {
		t.Fatalf("expected system PTU unchanged")
	}
}

func TestNorm_StringContentPTUExtracted(t *testing.T) {
	// Content is a string wrapped in <system-reminder> → extracted as system PTU
	body := []byte(`{"messages":[{"role":"user","content":"<system-reminder>\nPreToolUse:Read hook.\n</system-reminder>"},{"role":"assistant","content":"ok"}]}`)
	out := NormalizePreToolUseMessages(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 2 {
		t.Fatalf("expected 2 messages, got %d: %s", count, string(out))
	}
	// Message 0: system PTU
	if gjson.GetBytes(out, "messages.0.role").String() != "system" {
		t.Fatalf("expected messages.0 to be system PTU, got %s", gjson.GetBytes(out, "messages.0.role").String())
	}
	if !strings.Contains(gjson.GetBytes(out, "messages.0.content").String(), "PreToolUse:Read hook") {
		t.Fatalf("expected messages.0 to contain PTU, got %s", gjson.GetBytes(out, "messages.0.content").String())
	}
	// No <system-reminder> wrapper
	if strings.Contains(gjson.GetBytes(out, "messages.0.content").String(), "<system-reminder>") {
		t.Fatalf("extracted PTU should not contain <system-reminder> wrapper")
	}
	// Message 1: assistant
	if gjson.GetBytes(out, "messages.1.content").String() != "ok" {
		t.Fatalf("expected messages.1 to be 'ok', got %s", gjson.GetBytes(out, "messages.1.content").String())
	}
}

func TestNorm_RawStringPTUExtracted(t *testing.T) {
	// CC Format B: raw string starting with "PreToolUse:" → extracted as system PTU
	body := []byte(`{"messages":[{"role":"user","content":"PreToolUse:Read hook additional context: Read multiple files in parallel when possible for faster analysis.\n\nPostToolUse:Read hook additional context: Extensive reading."},{"role":"assistant","content":"ok"}]}`)
	out := NormalizePreToolUseMessages(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 2 {
		t.Fatalf("expected 2 messages, got %d: %s", count, string(out))
	}
	// Message 0: system PTU
	if gjson.GetBytes(out, "messages.0.role").String() != "system" {
		t.Fatalf("expected messages.0 to be system PTU, got %s", gjson.GetBytes(out, "messages.0.role").String())
	}
	if !strings.Contains(gjson.GetBytes(out, "messages.0.content").String(), "PreToolUse:Read hook") {
		t.Fatalf("expected messages.0 to contain PTU, got %s", gjson.GetBytes(out, "messages.0.content").String())
	}
	// Message 1: assistant
	if gjson.GetBytes(out, "messages.1.content").String() != "ok" {
		t.Fatalf("expected messages.1 to be 'ok', got %s", gjson.GetBytes(out, "messages.1.content").String())
	}
}

func TestNorm_PlainStringContentNormalized(t *testing.T) {
	// Plain user string that does NOT look like hook injection →
	// content format normalized to array for byte-stability between rounds
	body := []byte(`{"messages":[{"role":"user","content":"What is PreToolUse in this context?"},{"role":"assistant","content":"ok"}]}`)
	out := NormalizePreToolUseMessages(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	c0 := gjson.GetBytes(out, "messages.0.content")
	if !c0.IsArray() || !strings.Contains(c0.Get("0.text").String(), "PreToolUse") {
		t.Fatalf("expected user content normalized to array, got %s", c0.Raw)
	}
}

func TestNorm_RealWorldPattern(t *testing.T) {
	// Simulates the actual e81 pattern: conversation with inline PTU user messages
	ptuText := `<system-reminder>
PreToolUse:Read hook additional context: Read multiple files in parallel when possible for faster analysis.
</system-reminder>`
	body := []byte(`{"model":"deepseek-v4-flash","messages":[
		{"role":"system","content":[{"type":"text","text":"You are Claude."}]},
		{"role":"user","content":[{"type":"text","text":"<system-reminder>\nAs you answer..."}]},
		{"role":"assistant","content":[{"type":"thinking","thinking":"..."}]},
		{"role":"user","content":[{"type":"text","text":"normal user message"}]},
		{"role":"tool","content":"tool output here"},
		{"role":"user","content":[{"type":"text","text":"` + ptuText + `"}]},
		{"role":"assistant","content":"response after PTU"},
		{"role":"tool","content":"more tool output"},
		{"role":"user","content":[{"type":"text","text":"` + ptuText + `"}]},
		{"role":"assistant","content":"final response"}
	]}`)
	out := NormalizePreToolUseMessages(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	count := gjson.GetBytes(out, "messages.#").Int()
	// original 10, 2 user PTU deleted (-2), 1 system PTU inserted at first deletion position (+1) → 9
	if count != 9 {
		t.Fatalf("expected 9 messages, got %d: %s", count, string(out))
	}
	// System PTU should be at position 5 (where the first user PTU was deleted),
	// not at the end.
	ptuRole := gjson.GetBytes(out, "messages.5.role").String()
	if ptuRole != "system" {
		t.Fatalf("expected messages.5 role=system (PTU at first deletion), got %s", ptuRole)
	}
	ptuContent := gjson.GetBytes(out, "messages.5.content").String()
	if !strings.Contains(ptuContent, "PreToolUse:Read hook") {
		t.Fatalf("expected messages.5 to contain PreToolUse:, got %s", ptuContent)
	}
	// The assistant "response after PTU" stays at position 6 (unchanged)
	asstContent := gjson.GetBytes(out, "messages.6.content").String()
	if !strings.Contains(asstContent, "response after PTU") {
		t.Fatalf("expected messages.6 to be 'response after PTU', got %s", asstContent)
	}
	// "final response" should be at position 8 (was 9, shifted by 1 due to second PTU deletion)
	finalContent := gjson.GetBytes(out, "messages.8.content").String()
	if !strings.Contains(finalContent, "final response") {
		t.Fatalf("expected messages.8 to be 'final response', got %s", finalContent)
	}
}

func TestNorm_StripSystemReminder(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "<system-reminder>\nPreToolUse:Read.\n</system-reminder>",
			expected: "PreToolUse:Read.",
		},
		{
			input:    "<system-reminder>\nPreToolUse:Read hook: read in parallel.\n</system-reminder>",
			expected: "PreToolUse:Read hook: read in parallel.",
		},
		{
			input:    "<system-reminder>PreToolUse:Inline.</system-reminder>",
			expected: "PreToolUse:Inline.",
		},
		{
			input:    "<system-reminder>PreToolUse:Read hook.\n\nMulti line.\n</system-reminder>",
			expected: "PreToolUse:Read hook.\n\nMulti line.",
		},
	}
	for _, tt := range tests {
		got := stripSystemReminder(tt.input)
		if got != tt.expected {
			t.Fatalf("stripSystemReminder(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestNorm_IsSystemInjectedPTU(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{input: "<system-reminder>\nPreToolUse:Read.\n</system-reminder>", expected: true},
		{input: "normal text PreToolUse:Read.", expected: false},
		{input: "<system-reminder>\nPreToolUse:Read.", expected: false},                                // missing close tag
		{input: "PreToolUse:Read.\n</system-reminder>", expected: false},                               // missing open tag
		{input: "<system-reminder>\nSomeOtherTag:Read.\n</system-reminder>", expected: false},          // no PreToolUse
		{input: "I found <system-reminder>PreToolUse:Read</system-reminder> in logs", expected: false}, // extra text after close tag
	}
	for _, tt := range tests {
		got := isSystemInjectedPTU(tt.input)
		if got != tt.expected {
			t.Fatalf("isSystemInjectedPTU(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestNorm_FullPipelineWithDedup(t *testing.T) {
	// Full pipeline: normalise → dedup (no eph)
	ptuText := `<system-reminder>
PreToolUse:Read hook additional context: Read multiple files in parallel when possible for faster analysis.
</system-reminder>`
	body := []byte(`{"messages":[
		{"role":"system","content":"You are Claude."},
		{"role":"user","content":[{"type":"text","text":"` + ptuText + `"}]},
		{"role":"assistant","content":"mid"},
		{"role":"user","content":[{"type":"text","text":"` + ptuText + `"}]},
		{"role":"assistant","content":"end"}
	]}`)
	out := NormalizePreToolUseMessages(body)
	out = DeduplicateSystemMessages(out)

	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	count := gjson.GetBytes(out, "messages.#").Int()
	// original 5, 2 user PTU deleted (-2), 1 system PTU inserted at first deletion position (+1) → 4
	if count != 4 {
		t.Fatalf("expected 4 messages, got %d: %s", count, string(out))
	}
	// System PTU inserted at position of first deleted user PTU (index 1)
	// Expected order: [0] sys(identity), [1] sys(PTU), [2] asst("mid"), [3] asst("end")
	msg1Role := gjson.GetBytes(out, "messages.1.role").String()
	if msg1Role != "system" {
		t.Fatalf("expected messages.1 role=system (PTU inserted at first deletion), got %s", msg1Role)
	}
	msg1Content := gjson.GetBytes(out, "messages.1.content").String()
	if !strings.Contains(msg1Content, "PreToolUse:Read hook") {
		t.Fatalf("expected messages.1 to contain PreToolUse:, got %s", msg1Content)
	}
	// Assistant messages preserved in order
	firstRole := gjson.GetBytes(out, "messages.0.role").String()
	if firstRole != "system" || !strings.Contains(gjson.GetBytes(out, "messages.0.content").String(), "Claude") {
		t.Fatalf("expected messages.0 to be system identity, got role=%s", firstRole)
	}
	midContent := gjson.GetBytes(out, "messages.2.content").String()
	if midContent != "mid" {
		t.Fatalf("expected messages.2 to be 'mid', got %s", midContent)
	}
	endContent := gjson.GetBytes(out, "messages.3.content").String()
	if endContent != "end" {
		t.Fatalf("expected messages.3 to be 'end', got %s", endContent)
	}
}

// --- RelocateHookMessages tests ---

func TestReloc_NoHooks(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"}]}`)
	out := RelocateHookMessages(body)
	if string(out) != string(body) {
		t.Fatalf("expected no change, got %s", string(out))
	}
}

func TestReloc_PTURelocatedToEnd(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hello"},{"role":"system","content":"PreToolUse:Read hook additional context: Read files in parallel."},{"role":"assistant","content":"done"}]}`)
	out := RelocateHookMessages(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	count := gjson.GetBytes(out, "messages.#").Int()
	// 3 original, PTU stripped → 2
	if count != 2 {
		t.Fatalf("expected 2 messages, got %d: %s", count, string(out))
	}
	// User message still at position 0
	if gjson.GetBytes(out, "messages.0.content").String() != "hello" {
		t.Fatalf("expected hello at position 0")
	}
	// Assistant at position 1
	if gjson.GetBytes(out, "messages.1.content").String() != "done" {
		t.Fatalf("expected done at position 1")
	}
}

func TestReloc_PostToolUseRemoved(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hello"},{"role":"user","content":"PostToolUse:Read hook additional context: Read 5 files."},{"role":"assistant","content":"done"}]}`)
	out := RelocateHookMessages(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	count := gjson.GetBytes(out, "messages.#").Int()
	// 3 original, PostToolUse removed → 2
	if count != 2 {
		t.Fatalf("expected 2 messages, got %d: %s", count, string(out))
	}
	if gjson.GetBytes(out, "messages.0.content").String() != "hello" {
		t.Fatalf("expected hello at position 0")
	}
	if gjson.GetBytes(out, "messages.1.content").String() != "done" {
		t.Fatalf("expected done at position 1")
	}
}

func TestReloc_MultiplePTUDedupedAtEnd(t *testing.T) {
	// Two identical PTU messages → both stripped
	body := []byte(`{"messages":[{"role":"system","content":"PreToolUse:Read hook: read in parallel."},{"role":"user","content":"real query"},{"role":"system","content":"PreToolUse:Read hook: read in parallel."},{"role":"assistant","content":"ok"}]}`)
	out := RelocateHookMessages(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	count := gjson.GetBytes(out, "messages.#").Int()
	// 4 original, 2 PTU stripped → 2
	if count != 2 {
		t.Fatalf("expected 2 messages, got %d: %s", count, string(out))
	}
	if gjson.GetBytes(out, "messages.0.content").String() != "real query" {
		t.Fatalf("expected real query at position 0")
	}
	if gjson.GetBytes(out, "messages.1.content").String() != "ok" {
		t.Fatalf("expected ok at position 1")
	}
}

func TestReloc_MixedPTUAndPostToolUse(t *testing.T) {
	// Mix of PTU, PostToolUse, and real conversation → all hooks stripped
	body := []byte(`{"messages":[{"role":"user","content":"query"},{"role":"system","content":"PreToolUse:Read hook: parallel reads."},{"role":"user","content":"PostToolUse:Read hook: read 10 files."},{"role":"assistant","content":"response"},{"role":"system","content":"PreToolUse:Edit hook: verify changes."}]}`)
	out := RelocateHookMessages(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	count := gjson.GetBytes(out, "messages.#").Int()
	// 5 original, 2 PTU stripped, 1 PostToolUse stripped → 2 (query + assistant)
	if count != 2 {
		t.Fatalf("expected 2 messages, got %d: %s", count, string(out))
	}
	if gjson.GetBytes(out, "messages.0.role").String() != "user" {
		t.Fatalf("expected user at position 0")
	}
	if gjson.GetBytes(out, "messages.1.role").String() != "assistant" {
		t.Fatalf("expected assistant at position 1")
	}
}

func TestReloc_NoMessages(t *testing.T) {
	body := []byte(`{"model":"test"}`)
	out := RelocateHookMessages(body)
	if string(out) != string(body) {
		t.Fatalf("expected no change, got %s", string(out))
	}
}

func TestReloc_FullPipeline(t *testing.T) {
	// Full pipeline: normalize → dedup → relocate → canonical
	ptuText := `<system-reminder>
PreToolUse:Read hook additional context: Read multiple files in parallel when possible for faster analysis.
</system-reminder>`
	postText := `<system-reminder>
PostToolUse:Read hook additional context: Extensive reading (5 files).
</system-reminder>`
	body := []byte(`{"messages":[
		{"role":"system","content":"You are Claude."},
		{"role":"user","content":[{"type":"text","text":"` + ptuText + `"}]},
		{"role":"assistant","content":"mid"},
		{"role":"user","content":[{"type":"text","text":"` + ptuText + `"}]},
		{"role":"user","content":[{"type":"text","text":"` + postText + `"}]},
		{"role":"assistant","content":"end"}
	]}`)
	out := NormalizePreToolUseMessages(body)
	out = DeduplicateSystemMessages(out)
	out = RelocateHookMessages(out)
	out = CanonicalizeJSON(out)

	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}
	// After pipeline: sys, asst(mid), asst(end) = 3
	// All PTU and PostToolUse stripped
	messages := gjson.GetBytes(out, "messages")
	count := messages.Get("#").Int()
	if count != 3 {
		t.Fatalf("expected 3 messages, got %d: %s", count, string(out))
	}
	// Verify no PostToolUse anywhere
	outStr := string(out)
	if strings.Contains(outStr, "PostToolUse:") {
		t.Fatalf("PostToolUse should be removed: %s", outStr)
	}
	// Verify no PTU anywhere
	if strings.Contains(outStr, "PreToolUse:") {
		t.Fatalf("PTU should be stripped: %s", outStr)
	}
	// Conversation preserved: sys, mid, end
	if messages.Get("0.content").String() != "You are Claude." {
		t.Fatalf("expected sys prompt at 0")
	}
	if messages.Get("1.content").String() != "mid" {
		t.Fatalf("expected mid at 1")
	}
	if messages.Get("2.content").String() != "end" {
		t.Fatalf("expected end at 2")
	}
}

func TestReloc_PTUDedupWithPostToolUseSuffix(t *testing.T) {
	// PTU with PostToolUse counters are stripped entirely
	body := []byte(`{"messages":[
		{"role":"system","content":"You are Claude."},
		{"role":"user","content":"real query"},
		{"role":"system","content":"PreToolUse:Read hook additional context: parallel reads.\n\nPostToolUse:Read hook additional context: Extensive reading (12 files)."},
		{"role":"system","content":"PreToolUse:Read hook additional context: parallel reads.\n\nPostToolUse:Read hook additional context: Extensive reading (13 files)."},
		{"role":"system","content":"PreToolUse:Read hook additional context: parallel reads.\n\nPostToolUse:Read hook additional context: Extensive reading (14 files)."},
		{"role":"assistant","content":"end"}
	]}`)

	out := RelocateHookMessages(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}

	count := gjson.GetBytes(out, "messages.#").Int()
	// 6 original, 3 PTU stripped → 3 (sys, user, assistant)
	if count != 3 {
		t.Fatalf("expected 3 messages, got %d: %s", count, string(out))
	}

	// Verify conversation preserved
	if gjson.GetBytes(out, "messages.0.role").String() != "system" {
		t.Fatalf("expected system at 0")
	}
	if gjson.GetBytes(out, "messages.1.role").String() != "user" {
		t.Fatalf("expected user at 1")
	}
	if gjson.GetBytes(out, "messages.2.role").String() != "assistant" {
		t.Fatalf("expected assistant at 2")
	}
}

func TestStripPostToolUseSuffix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			"PreToolUse:Read hook\n\nPostToolUse:Read hook (12 files)",
			"PreToolUse:Read hook",
		},
		{
			"PreToolUse:Read hook\nPostToolUse:Read hook (12 files)",
			"PreToolUse:Read hook",
		},
		{
			"PreToolUse:Read hook\n\nPostToolUseFailure:something failed",
			"PreToolUse:Read hook",
		},
		{
			"PreToolUse:Edit hook: verify changes.",
			"PreToolUse:Edit hook: verify changes.",
		},
		{
			"Plain text without PostToolUse",
			"Plain text without PostToolUse",
		},
	}
	for _, tc := range tests {
		got := stripPostToolUseSuffix(tc.input)
		if got != tc.expected {
			t.Errorf("stripPostToolUseSuffix(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestReloc_RoundBoundarySimulation(t *testing.T) {
	// Simulate cross-round: prev round has PTU+PostToolUse, new round has different hooks
	// Both should produce the same conversation prefix
	ptuA := "PreToolUse:Read hook: read in parallel."
	ptuB := "PreToolUse:Edit hook: verify changes."
	postA := "PostToolUse:Read hook: read 10 files."

	prevBody := []byte(`{"messages":[
		{"role":"system","content":"You are Claude."},
		{"role":"user","content":"real query"},
		{"role":"assistant","content":"response"},
		{"role":"tool","content":"result"},
		{"role":"system","content":"` + ptuA + `"},
		{"role":"user","content":"` + postA + `"}
	]}`)

	nextBody := []byte(`{"messages":[
		{"role":"system","content":"You are Claude."},
		{"role":"user","content":"real query"},
		{"role":"assistant","content":"response"},
		{"role":"tool","content":"result"},
		{"role":"system","content":"` + ptuB + `"}
	]}`)

	outPrev := RelocateHookMessages(prevBody)
	outNext := RelocateHookMessages(nextBody)

	if !gjson.ValidBytes(outPrev) || !gjson.ValidBytes(outNext) {
		t.Fatalf("output is not valid JSON")
	}

	// Both should have conversation prefix [sys, user, assistant, tool]
	// as the first N messages, differing only in PTU at end
	for i := 0; i < 4; i++ {
		rPrev := gjson.GetBytes(outPrev, fmt.Sprintf("messages.%d.role", i)).String()
		rNext := gjson.GetBytes(outNext, fmt.Sprintf("messages.%d.role", i)).String()
		if rPrev != rNext {
			t.Fatalf("position %d: expected %s == %s for cache prefix match", i, rPrev, rNext)
		}
	}
}

func TestReorder_SystemMsgsToFront(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"system","content":"sys1"},
		{"role":"user","content":"u1"},
		{"role":"system","content":"sys2"},
		{"role":"assistant","content":"a1"},
		{"role":"tool","content":"t1"}
	]}`)

	out := ReorderSystemMessagesToFront(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}

	// After reorder: [sys1, sys2, u1, a1, t1]
	expected := []string{"system", "system", "user", "assistant", "tool"}
	for i, want := range expected {
		got := gjson.GetBytes(out, fmt.Sprintf("messages.%d.role", i)).String()
		if got != want {
			t.Fatalf("position %d: expected %s, got %s", i, want, got)
		}
	}
}

func TestReorder_NoSystemMsgs(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"u1"},{"role":"assistant","content":"a1"}]}`)
	out := ReorderSystemMessagesToFront(body)
	if string(out) != string(body) {
		t.Fatalf("expected no change, got %s", string(out))
	}
}

func TestReorder_PreservesConversationOrder(t *testing.T) {
	// Conversation messages keep their relative order
	body := []byte(`{"messages":[
		{"role":"system","content":"s1"},
		{"role":"user","content":"u1"},
		{"role":"system","content":"s2"},
		{"role":"assistant","content":"a1"},
		{"role":"user","content":"u2"},
		{"role":"system","content":"s3"},
		{"role":"tool","content":"t1"},
		{"role":"assistant","content":"a2"}
	]}`)

	out := ReorderSystemMessagesToFront(body)
	// After: [s1, s2, s3, u1, a1, u2, t1, a2]
	expectedRoles := []string{"system", "system", "system", "user", "assistant", "user", "tool", "assistant"}
	for i, want := range expectedRoles {
		got := gjson.GetBytes(out, fmt.Sprintf("messages.%d.role", i)).String()
		if got != want {
			t.Fatalf("position %d: expected %s, got %s", i, want, got)
		}
	}
	// Conversation relative order: u1, a1, u2, t1, a2
	if gjson.GetBytes(out, "messages.3.content").String() != "u1" {
		t.Fatalf("conversation order broken")
	}
	if gjson.GetBytes(out, "messages.7.content").String() != "a2" {
		t.Fatalf("conversation order broken at end")
	}
}
