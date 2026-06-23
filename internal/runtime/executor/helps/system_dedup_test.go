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

// --- CollapseTaskReminders tests ---

func TestCollapseTask_NoMessages(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-pro"}`)
	out := CollapseTaskReminders(body)
	if string(out) != string(body) {
		t.Fatalf("expected no change, got %s", string(out))
	}
}

func TestCollapseTask_NoTaskReminders(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"You are helpful."},{"role":"user","content":"hi"}]}`)
	out := CollapseTaskReminders(body)
	if string(out) != string(body) {
		t.Fatalf("expected no change, got %s", string(out))
	}
}

func TestCollapseTask_SingleTaskReminder(t *testing.T) {
	// Single reminder → converted to <system-reminder> user message (in-place)
	body := []byte(`{"messages":[{"role":"system","content":"You are helpful."},{"role":"system","content":"The task tools haven't been used recently. If you're working on tasks, consider using TaskCreate.\n\nHere are the existing tasks:\n\n#1. [in_progress] Do thing"}]}`)
	out := CollapseTaskReminders(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 2 {
		t.Fatalf("expected 2 messages (in-place conversion), got %d: %s", count, string(out))
	}
	// Message at index 1 should be user with <system-reminder> wrapper
	r := gjson.GetBytes(out, "messages.1.role").String()
	if r != "user" {
		t.Fatalf("expected user role, got %s", r)
	}
	c := gjson.GetBytes(out, "messages.1.content").String()
	if !strings.HasPrefix(c, "<system-reminder>") {
		t.Fatalf("expected <system-reminder> wrapper, got: %s", c[:50])
	}
	if !strings.Contains(c, "Do thing") {
		t.Fatalf("task list content should be preserved, got: %s", c)
	}
}

func TestCollapseTask_MultipleTaskReminders(t *testing.T) {
	// Two variants → collapsed to 1, converted to <system-reminder> user
	body := []byte(`{"messages":[{"role":"system","content":"You are helpful."},{"role":"user","content":"start"},{"role":"system","content":"The task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. [in_progress] Task A"},{"role":"assistant","content":"ok"},{"role":"system","content":"The task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. [in_progress] Task A\n#2. [in_progress] Task B"}]}`)
	out := CollapseTaskReminders(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 4 {
		t.Fatalf("expected 4 messages, got %d: %s", count, string(out))
	}
	// Message at index 3 should be user with <system-reminder> containing most complete task list
	r := gjson.GetBytes(out, "messages.3.role").String()
	if r != "user" {
		t.Fatalf("expected user role, got %s", r)
	}
	c := gjson.GetBytes(out, "messages.3.content").String()
	if !strings.HasPrefix(c, "<system-reminder>") {
		t.Fatalf("expected <system-reminder> wrapper, got: %s", c[:50])
	}
	if !strings.Contains(c, "Task B") {
		t.Fatalf("should contain most complete task list, got: %s", c)
	}
}

func TestCollapseTask_OnlyTaskReminders(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"The task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. Task A"},{"role":"system","content":"The task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. Task A\n#2. Task B"},{"role":"system","content":"The task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. Task A\n#2. Task B\n#3. Task C"}]}`)
	out := CollapseTaskReminders(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 1 {
		t.Fatalf("expected 1 message (converted in-place), got %d: %s", count, string(out))
	}
	r := gjson.GetBytes(out, "messages.0.role").String()
	if r != "user" {
		t.Fatalf("expected user role, got %s", r)
	}
	c := gjson.GetBytes(out, "messages.0.content").String()
	if !strings.Contains(c, "Task C") {
		t.Fatalf("should contain most complete task list, got: %s", c)
	}
}

func TestCollapseTask_MixedWithNormalMessages(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"be helpful"},{"role":"user","content":"q1"},{"role":"system","content":"The task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. Task A"},{"role":"assistant","content":"a1"},{"role":"tool","content":"t1"},{"role":"system","content":"The task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. Task A\n#2. Task B"},{"role":"user","content":"q2"}]}`)
	out := CollapseTaskReminders(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 6 {
		t.Fatalf("expected 6 messages, got %d: %s", count, string(out))
	}
	// Find the converted <system-reminder> user message
	var found bool
	for i := 0; i < int(count); i++ {
		r := gjson.GetBytes(out, fmt.Sprintf("messages.%d.role", i)).String()
		c := gjson.GetBytes(out, fmt.Sprintf("messages.%d.content", i)).String()
		if r == "user" && strings.HasPrefix(c, "<system-reminder>") && strings.Contains(c, taskReminderPrefix) {
			found = true
			if !strings.Contains(c, "Task B") {
				t.Fatalf("should contain most complete task list, got: %s", c)
			}
		}
	}
	if !found {
		t.Fatal("<system-reminder> task reminder user message not found")
	}
}

func TestCollapseTask_ArrayContentNotCollapsed(t *testing.T) {
	// System messages with array content should not be collapsed (not string content)
	body := []byte(`{"messages":[{"role":"system","content":[{"type":"text","text":"The task tools haven't been used recently. This will not match."}]},{"role":"user","content":"hi"}]}`)
	out := CollapseTaskReminders(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 2 {
		t.Fatalf("expected 2 messages (array content not matched), got %d: %s", count, string(out))
	}
}

func TestCollapseTask_DedupThenCollapse(t *testing.T) {
	// dedup removes exact copies, CollapseTaskReminders converts to <system-reminder> user
	body := []byte(`{"messages":[{"role":"system","content":"You are helpful."},{"role":"system","content":"The task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. Task A"},{"role":"system","content":"The task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. Task A"},{"role":"system","content":"The task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. Task A"},{"role":"user","content":"hi"},{"role":"system","content":"The task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. Task A\n#2. Task B"},{"role":"system","content":"The task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. Task A\n#2. Task B"}]}`)
	out := DeduplicateSystemMessages(body)
	out = CollapseTaskReminders(out)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 3 {
		t.Fatalf("expected 3 messages (sys_prompt + user_hi + <system-reminder>), got %d: %s", count, string(out))
	}
	// Find <system-reminder> user message with most complete task list
	var found bool
	for i := 0; i < int(count); i++ {
		r := gjson.GetBytes(out, fmt.Sprintf("messages.%d.role", i)).String()
		c := gjson.GetBytes(out, fmt.Sprintf("messages.%d.content", i)).String()
		if r == "user" && strings.HasPrefix(c, "<system-reminder>") && strings.Contains(c, taskReminderPrefix) {
			found = true
			if !strings.Contains(c, "Task B") {
				t.Fatalf("should contain most complete task list, got: %s", c)
			}
		}
	}
	if !found {
		t.Fatal("<system-reminder> task reminder user message not found")
	}
}

func TestCollapseTask_SplitReminder(t *testing.T) {
	// Single reminder → converted to <system-reminder> user (in-place)
	body := []byte(`{"messages":[{"role":"system","content":"You are helpful."},{"role":"user","content":"hi"},{"role":"system","content":"The task tools haven't been used recently. If you are working on tasks, consider using TaskCreate.\n\nHere are the existing tasks:\n\n#1. [in_progress] Some task\n#2. [pending] Another"}]}`)
	out := CollapseTaskReminders(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 3 {
		t.Fatalf("expected 3 messages (in-place conversion), got %d: %s", count, string(out))
	}
	c := gjson.GetBytes(out, "messages.2.content").String()
	if !strings.HasPrefix(c, "<system-reminder>") {
		t.Fatalf("expected <system-reminder>, got: %s", c[:50])
	}
	if !strings.Contains(c, "Some task") || !strings.Contains(c, "Another") {
		t.Fatalf("should contain complete task list, got: %s", c)
	}
}

func TestCollapseTask_NoTaskListMarker(t *testing.T) {
	// Reminder without task list marker → still converted to <system-reminder> user
	body := []byte(`{"messages":[{"role":"system","content":"The task tools haven't been used recently. Just a plain reminder."},{"role":"user","content":"hi"}]}`)
	out := CollapseTaskReminders(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 2 {
		t.Fatalf("expected 2 messages (in-place), got %d: %s", count, string(out))
	}
	r := gjson.GetBytes(out, "messages.0.role").String()
	if r != "user" {
		t.Fatalf("expected user role, got %s", r)
	}
	c := gjson.GetBytes(out, "messages.0.content").String()
	if !strings.HasPrefix(c, "<system-reminder>") {
		t.Fatalf("expected <system-reminder> wrapper, got: %s", c)
	}
}

func TestCollapseTask_IdempotentSplit(t *testing.T) {
	// Running collapse twice must produce identical results
	body := []byte(`{"messages":[{"role":"system","content":"You are helpful."},{"role":"system","content":"The task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. [in_progress] Task A"}]}`)
	out1 := CollapseTaskReminders(body)
	out2 := CollapseTaskReminders(out1)
	c1 := gjson.GetBytes(out1, "messages.#").Int()
	c2 := gjson.GetBytes(out2, "messages.#").Int()
	if c1 != c2 {
		t.Fatalf("idempotent: first pass=%d messages, second pass=%d messages (should be equal)", c1, c2)
	}
	if string(out1) != string(out2) {
		t.Fatalf("idempotent: bodies differ\npass1: %s\npass2: %s", string(out1), string(out2))
	}
}

func TestCollapseTask_ReplaceExistingTaskList(t *testing.T) {
	// Simulate two consecutive requests where the task list changes.
	// Request 2 injects updated task state → converted <system-reminder> should reflect it.
	body2 := []byte(`{"messages":[{"role":"system","content":"You are helpful."},{"role":"system","content":"The task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. [completed] Task A\n#2. [in_progress] Task B"},{"role":"user","content":"Continue working."}]}`)
	out2 := CollapseTaskReminders(body2)

	// Find <system-reminder> user message with latest task state
	var found bool
	for i := 0; i < int(gjson.GetBytes(out2, "messages.#").Int()); i++ {
		r := gjson.GetBytes(out2, fmt.Sprintf("messages.%d.role", i)).String()
		c := gjson.GetBytes(out2, fmt.Sprintf("messages.%d.content", i)).String()
		if r == "user" && strings.HasPrefix(c, "<system-reminder>") && strings.Contains(c, taskReminderPrefix) {
			found = true
			if !strings.Contains(c, "Task B") || !strings.Contains(c, "completed") {
				t.Fatalf("should reflect latest state (Task A completed, Task B in_progress), got: %s", c)
			}
		}
	}
	if !found {
		t.Fatal("<system-reminder> task reminder user message not found")
	}
}

func TestCollapseTask_ZombieTaskListCleanup(t *testing.T) {
	// Old [task-list] messages from previous code version + new <system-reminder> conversion.
	// Old [task-list] messages are left untouched (they're historical data), new one is converted.
	body := []byte(`{"messages":[{"role":"system","content":"You are helpful."},{"role":"user","content":"[task-list]\n#1. Old Task A\n[/task-list]"},{"role":"user","content":"something else"},{"role":"user","content":"[task-list]\n#1. Old Task B\n[/task-list]"},{"role":"assistant","content":"ok"},{"role":"user","content":"[task-list]\n#1. Old Task C\n[/task-list]"},{"role":"system","content":"The task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. Task D"}]}`)
	out := CollapseTaskReminders(body)
	count := gjson.GetBytes(out, "messages.#").Int()

	// Find the new <system-reminder> user message
	var found bool
	for i := 0; i < int(count); i++ {
		r := gjson.GetBytes(out, fmt.Sprintf("messages.%d.role", i)).String()
		c := gjson.GetBytes(out, fmt.Sprintf("messages.%d.content", i)).String()
		if r == "user" && strings.HasPrefix(c, "<system-reminder>") && strings.Contains(c, "Task D") {
			found = true
		}
	}
	if !found {
		t.Fatalf("converted <system-reminder> with Task D not found: %s", string(out))
	}
}

// --- CollapseSystemNotifications tests ---

func TestCollapseNotif_NoMessages(t *testing.T) {
	body := []byte(`{"model":"deepseek"}`)
	out := CollapseSystemNotifications(body)
	if string(out) != string(body) {
		t.Fatalf("expected no change, got %s", string(out))
	}
}

func TestCollapseNotif_NoNotifications(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"be helpful"},{"role":"user","content":"hi"}]}`)
	out := CollapseSystemNotifications(body)
	if string(out) != string(body) {
		t.Fatalf("expected no change, got %s", string(out))
	}
}

func TestCollapseNotif_SingleNotification(t *testing.T) {
	// Single notification → normalized (XML block stripped)
	body := []byte(`{"messages":[{"role":"system","content":"be helpful"},{"role":"system","content":"[SYSTEM NOTIFICATION - NOT USER INPUT]\nThis is an automated background-task event.\n\n<task-notification>\n<task-id>abc</task-id>\n<output-file>/tmp/x</output-file>\n</task-notification>"}]}`)
	out := CollapseSystemNotifications(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 2 {
		t.Fatalf("expected 2 messages, got %d: %s", count, string(out))
	}
	kept := gjson.GetBytes(out, "messages.1.content").String()
	if strings.Contains(kept, "<task-notification>") || strings.Contains(kept, "<task-id>") {
		t.Fatalf("XML block should have been stripped, got: %s", kept)
	}
	if !strings.Contains(kept, "background task completed") {
		t.Fatalf("normalized notification should have placeholder, got: %s", kept)
	}
}

func TestCollapseNotif_MultipleNotifications(t *testing.T) {
	// Multiple → collapsed to 1, then normalized
	body := []byte(`{"messages":[{"role":"system","content":"You are helpful."},{"role":"user","content":"start"},{"role":"system","content":"[SYSTEM NOTIFICATION - NOT USER INPUT]\n\n<task-notification>\n<task-id>a</task-id>\n</task-notification>"},{"role":"assistant","content":"ok"},{"role":"system","content":"[SYSTEM NOTIFICATION - NOT USER INPUT]\n\n<task-notification>\n<task-id>b</task-id>\n</task-notification>"},{"role":"system","content":"[SYSTEM NOTIFICATION - NOT USER INPUT]\n\n<task-notification>\n<task-id>c</task-id>\n</task-notification>"}]}`)
	out := CollapseSystemNotifications(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 4 {
		t.Fatalf("expected 4 messages, got %d: %s", count, string(out))
	}
	notifCount := 0
	for i := 0; i < int(count); i++ {
		c := gjson.GetBytes(out, fmt.Sprintf("messages.%d.content", i)).String()
		if strings.HasPrefix(c, sysNotificationPrefix) {
			notifCount++
			if strings.Contains(c, "<task-notification>") {
				t.Fatalf("XML block should be stripped, got: %s", c)
			}
		}
	}
	if notifCount != 1 {
		t.Fatalf("expected 1 notification, got %d", notifCount)
	}
}

func TestCollapseNotif_OnlyNotifications(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"[SYSTEM NOTIFICATION - NOT USER INPUT]\n\n<task-notification>\n<task-id>1</task-id>\n</task-notification>TASK_REMINDER_TAIL"},{"role":"system","content":"[SYSTEM NOTIFICATION - NOT USER INPUT]\n\n<task-notification>\n<task-id>2</task-id>\n</task-notification>MORE_TAIL"},{"role":"system","content":"[SYSTEM NOTIFICATION - NOT USER INPUT]\n\n<task-notification>\n<task-id>3</task-id>\n</task-notification>STUFF"}]}`)
	out := CollapseSystemNotifications(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 1 {
		t.Fatalf("expected 1 message, got %d: %s", count, string(out))
	}
	kept := gjson.GetBytes(out, "messages.0.content").String()
	if strings.Contains(kept, "STUFF") || strings.Contains(kept, "TASK_REMINDER_TAIL") {
		t.Fatalf("tail after XML should be stripped, got: %s", kept)
	}
	if !strings.Contains(kept, "background task completed") {
		t.Fatalf("should have placeholder, got: %s", kept)
	}
}

func TestCollapseNotif_Idempotent(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"be helpful"},{"role":"system","content":"[SYSTEM NOTIFICATION - NOT USER INPUT]\n\n<task-notification>\n<task-id>a</task-id>\n</task-notification>"},{"role":"system","content":"[SYSTEM NOTIFICATION - NOT USER INPUT]\n\n<task-notification>\n<task-id>b</task-id>\n</task-notification>tail"},{"role":"user","content":"hi"}]}`)
	out1 := CollapseSystemNotifications(body)
	out2 := CollapseSystemNotifications(out1)
	c1 := gjson.GetBytes(out1, "messages.#").Int()
	c2 := gjson.GetBytes(out2, "messages.#").Int()
	if c1 != c2 {
		t.Fatalf("idempotent: first pass=%d messages, second pass=%d", c1, c2)
	}
	if string(out1) != string(out2) {
		t.Fatalf("idempotent: bodies differ\npass1: %s\npass2: %s", string(out1), string(out2))
	}
	// Verify 1 notification, normalized
	notifCount := 0
	for i := 0; i < int(c1); i++ {
		c := gjson.GetBytes(out1, fmt.Sprintf("messages.%d.content", i)).String()
		if strings.HasPrefix(c, sysNotificationPrefix) {
			notifCount++
			if strings.Contains(c, "<task-notification>") || strings.Contains(c, "tail") {
				t.Fatalf("notification should be normalized, got: %s", c)
			}
		}
	}
	if notifCount != 1 {
		t.Fatalf("expected 1 notification, got %d", notifCount)
	}
}

func TestCollapseNotif_WithTaskReminders(t *testing.T) {
	// New pipeline order: notification normalization runs FIRST,
	// cleaning embedded task reminders before CollapseTaskReminders sees them.
	body := []byte(`{"messages":[{"role":"system","content":"be helpful"},{"role":"system","content":"The task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. Do thing"},{"role":"system","content":"[SYSTEM NOTIFICATION - NOT USER INPUT]\n\n<task-notification>\n<task-id>a</task-id>\n</task-notification>\n\nThe task tools haven't been used recently. EMBEDDED REMINDER"},{"role":"user","content":"u1"}]}`)
	// New order: notif collapse first, then task collapse
	out := CollapseSystemNotifications(body)
	out = CollapseTaskReminders(out)

	count := gjson.GetBytes(out, "messages.#").Int()
	notifCount := 0
	taskPreambleCount := 0
	for i := 0; i < int(count); i++ {
		r := gjson.GetBytes(out, fmt.Sprintf("messages.%d.role", i)).String()
		c := gjson.GetBytes(out, fmt.Sprintf("messages.%d.content", i)).String()
		if strings.HasPrefix(c, sysNotificationPrefix) {
			notifCount++
			if strings.Contains(c, "EMBEDDED REMINDER") || strings.Contains(c, "<task-notification>") {
				t.Fatalf("notification should be normalized (embedded reminder stripped), got: %s", c)
			}
		}
		if r == "user" && strings.HasPrefix(c, "<system-reminder>") && strings.Contains(c, taskReminderPrefix) {
			taskPreambleCount++
			if !strings.Contains(c, "Here are the existing tasks") {
				t.Fatalf("task reminder should preserve task list marker, got: %s", c)
			}
		}
	}
	if notifCount != 1 {
		t.Fatalf("expected 1 notification, got %d", notifCount)
	}
	if taskPreambleCount != 1 {
		t.Fatalf("expected 1 task reminder as <system-reminder> user, got %d", taskPreambleCount)
	}
}

func TestCollapseNotif_NoMarker(t *testing.T) {
	// Notification without <task-notification> marker → unchanged
	body := []byte(`{"messages":[{"role":"system","content":"[SYSTEM NOTIFICATION - NOT USER INPUT]\nJust a plain notification."},{"role":"user","content":"hi"}]}`)
	out := CollapseSystemNotifications(body)
	kept := gjson.GetBytes(out, "messages.0.content").String()
	if kept != "[SYSTEM NOTIFICATION - NOT USER INPUT]\nJust a plain notification." {
		t.Fatalf("without marker, content should be unchanged, got: %s", kept)
	}
}

// --- CollapseUnknownSystemMessages tests ---

func TestCollapseUnknown_None(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"Available agent types: claude, explore"},{"role":"user","content":"hi"}]}`)
	out := CollapseUnknownSystemMessages(body)
	if string(out) != string(body) {
		t.Fatalf("known type should be skipped, got %s", string(out))
	}
}

func TestCollapseUnknown_Single(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"Available agent types..."},{"role":"system","content":"The user sent a new message while you were working: stop"},{"role":"user","content":"hi"}]}`)
	out := CollapseUnknownSystemMessages(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 3 {
		t.Fatalf("single unknown should be kept, got %d: %s", count, string(out))
	}
}

func TestCollapseUnknown_Multiple(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"Available agent types..."},{"role":"user","content":"start"},{"role":"system","content":"The user sent a new message while you were working: first"},{"role":"assistant","content":"ok"},{"role":"system","content":"The user sent a new message while you were working: second"},{"role":"system","content":"The user sent a new message while you were working: third"}]}`)
	out := CollapseUnknownSystemMessages(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 4 {
		t.Fatalf("expected 4 messages (3 unknowns collapsed to 1), got %d: %s", count, string(out))
	}
	kept := gjson.GetBytes(out, "messages.3.content").String()
	if !strings.Contains(kept, "third") {
		t.Fatalf("kept should be latest (third), got: %s", kept)
	}
}

func TestCollapseUnknown_AllCollapsesTogether(t *testing.T) {
	// Notif + task reminder + unknown interruption — all three collapses run in order
	body := []byte(`{"messages":[{"role":"system","content":"be helpful"},{"role":"user","content":"u1"},{"role":"system","content":"The task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. Task A"},{"role":"system","content":"[SYSTEM NOTIFICATION - NOT USER INPUT]\n\n<task-notification>\n<task-id>a</task-id>\n</task-notification>"},{"role":"system","content":"The task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. Task B"},{"role":"system","content":"The user sent a new message while you were working: pause"},{"role":"system","content":"The user sent a new message while you were working: stop now"},{"role":"assistant","content":"a1"}]}`)
	out := CollapseSystemNotifications(body)
	out = CollapseTaskReminders(out)
	out = CollapseUnknownSystemMessages(out)
	count := gjson.GetBytes(out, "messages.#").Int()
	// Verify: 1 notif (system), 1 task reminder (converted to <system-reminder> user), 1 unknown (system), + other msgs
	notifCount := 0
	taskUserCount := 0
	taskSysCount := 0
	unknownCount := 0
	for i := 0; i < int(count); i++ {
		r := gjson.GetBytes(out, fmt.Sprintf("messages.%d.role", i)).String()
		c := gjson.GetBytes(out, fmt.Sprintf("messages.%d.content", i)).String()
		if r == "system" {
			if strings.HasPrefix(c, sysNotificationPrefix) {
				notifCount++
			}
			if strings.HasPrefix(c, taskReminderPrefix) {
				taskSysCount++
			}
			if strings.HasPrefix(c, "The user sent a new message") {
				unknownCount++
			}
		}
		if r == "user" && strings.HasPrefix(c, "<system-reminder>") && strings.Contains(c, taskReminderPrefix) {
			taskUserCount++
		}
	}
	_ = count
	if notifCount != 1 {
		t.Fatalf("expected 1 notif, got %d", notifCount)
	}
	if taskSysCount != 0 {
		t.Fatalf("expected 0 task reminders as system (should be converted to user), got %d", taskSysCount)
	}
	if taskUserCount != 1 {
		t.Fatalf("expected 1 task reminder as <system-reminder> user, got %d", taskUserCount)
	}
	if unknownCount != 1 {
		t.Fatalf("expected 1 unknown, got %d", unknownCount)
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

func TestReloc_PostToolUseFailureSystemMessage(t *testing.T) {
	// Real-world: CC injects PostToolUseFailure as a SYSTEM message (not user).
	// This was missed because isPostToolUseUserMessage only checks role=user.
	body := []byte(`{"messages":[
		{"role":"system","content":"You are Claude."},
		{"role":"user","content":"real query"},
		{"role":"system","content":"PostToolUseFailure:mcp__chrome-devtools__evaluate_script hook additional context: Tool failed. Analyze the error."},
		{"role":"assistant","content":"done"}
	]}`)

	out := RelocateHookMessages(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}

	count := gjson.GetBytes(out, "messages.#").Int()
	// 4 original, 1 PTU(PostToolUseFailure as system) stripped → 3
	if count != 3 {
		t.Fatalf("expected 3 messages, got %d: %s", count, string(out))
	}

	if gjson.GetBytes(out, "messages.0.role").String() != "system" {
		t.Fatalf("expected system at 0")
	}
	if gjson.GetBytes(out, "messages.2.role").String() != "assistant" {
		t.Fatalf("expected assistant at 2, got %s", string(out))
	}

	// Verify PostToolUseFailure was removed
	outStr := string(out)
	if strings.Contains(outStr, "PostToolUseFailure") {
		t.Fatalf("PostToolUseFailure should be stripped")
	}
}

// --- ReanchorHooks tests ---

func TestReanchor_NoHooks(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"}]}`)
	out := ReanchorHooks(body)
	if string(out) != string(body) {
		t.Fatalf("expected no change, got %s", string(out))
	}
}

func TestReanchor_PTUAnchoredToTool(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"system","content":"You are Claude."},
		{"role":"user","content":"read file"},
		{"role":"assistant","content":"ok","tool_calls":[{"id":"c1","type":"function","function":{"name":"Read","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"file contents here"},
		{"role":"system","content":"PreToolUse:Read hook additional context: Read files in parallel."},
		{"role":"assistant","content":"done"}
	]}`)

	out := ReanchorHooks(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}

	// 6 original → 5 (PTU absorbed into tool)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 5 {
		t.Fatalf("expected 5 messages, got %d: %s", count, string(out))
	}

	toolContent := gjson.GetBytes(out, "messages.3.content").String()
	if !strings.Contains(toolContent, "file contents here") {
		t.Fatalf("tool content should retain original output: %s", toolContent)
	}
	if !strings.Contains(toolContent, "---\nPreToolUse:Read") {
		t.Fatalf("tool content should contain anchored hook with separator, got: %s", toolContent)
	}
	if !strings.Contains(toolContent, "Read files in parallel") {
		t.Fatalf("tool content should contain hook text, got: %s", toolContent)
	}
}

func TestReanchor_PostToolUseAnchoredToTool(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"system","content":"You are Claude."},
		{"role":"user","content":"query"},
		{"role":"assistant","content":"ok","tool_calls":[{"id":"c1","type":"function","function":{"name":"Bash","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"command output"},
		{"role":"user","content":"PostToolUse:Bash hook additional context: Command succeeded."},
		{"role":"assistant","content":"done"}
	]}`)

	out := ReanchorHooks(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}

	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 5 {
		t.Fatalf("expected 5 messages, got %d: %s", count, string(out))
	}

	toolContent := gjson.GetBytes(out, "messages.3.content").String()
	if !strings.Contains(toolContent, "PostToolUse:Bash") {
		t.Fatalf("tool content should contain PostToolUse text, got: %s", toolContent)
	}
	if !strings.Contains(toolContent, "Command succeeded") {
		t.Fatalf("tool content should contain hook body, got: %s", toolContent)
	}
}

func TestReanchor_PostToolUseCounterPreserved(t *testing.T) {
	// Standalone PostToolUse with counter — must be preserved in full
	body := []byte(`{"messages":[
		{"role":"system","content":"You are Claude."},
		{"role":"user","content":"query"},
		{"role":"assistant","content":"ok","tool_calls":[{"id":"c1","type":"function","function":{"name":"Read","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"output"},
		{"role":"user","content":"PostToolUse:Read hook additional context: Extensive reading (12 files)."},
		{"role":"assistant","content":"done"}
	]}`)

	out := ReanchorHooks(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}

	toolContent := gjson.GetBytes(out, "messages.3.content").String()
	if !strings.Contains(toolContent, "12 files") {
		t.Fatalf("PostToolUse counter must be preserved, got: %s", toolContent)
	}
}

func TestReanchor_PostToolUseFailureAnchoredToTool(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"system","content":"You are Claude."},
		{"role":"user","content":"query"},
		{"role":"assistant","content":"ok","tool_calls":[{"id":"c1","type":"function","function":{"name":"mcp__chrome","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"error output"},
		{"role":"system","content":"PostToolUseFailure:mcp__chrome-devtools__evaluate_script hook additional context: Tool failed."},
		{"role":"assistant","content":"retrying"}
	]}`)

	out := ReanchorHooks(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}

	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 5 {
		t.Fatalf("expected 5 messages, got %d: %s", count, string(out))
	}

	toolContent := gjson.GetBytes(out, "messages.3.content").String()
	if !strings.Contains(toolContent, "PostToolUseFailure:mcp__chrome") {
		t.Fatalf("tool content should contain PostToolUseFailure text, got: %s", toolContent)
	}
	if !strings.Contains(toolContent, "Tool failed") {
		t.Fatalf("tool content should contain failure body, got: %s", toolContent)
	}
}

func TestReanchor_MultipleHooksAnchoredToSameTool(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"system","content":"You are Claude."},
		{"role":"user","content":"query"},
		{"role":"assistant","content":"ok","tool_calls":[{"id":"c1","type":"function","function":{"name":"Read","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"file output"},
		{"role":"system","content":"PreToolUse:Read hook additional context: parallel reads."},
		{"role":"user","content":"PostToolUse:Read hook additional context: read 10 files."},
		{"role":"assistant","content":"done"}
	]}`)

	out := ReanchorHooks(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}

	// 7 original, 2 hooks anchored → 5
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 5 {
		t.Fatalf("expected 5 messages, got %d: %s", count, string(out))
	}

	toolContent := gjson.GetBytes(out, "messages.3.content").String()
	if !strings.Contains(toolContent, "PreToolUse:Read") {
		t.Fatalf("missing PreToolUse in: %s", toolContent)
	}
	if !strings.Contains(toolContent, "PostToolUse:Read") {
		t.Fatalf("missing PostToolUse in: %s", toolContent)
	}
}

func TestReanchor_HookNotAdjacentToToolKeptAsIs(t *testing.T) {
	// Hook NOT after a tool and no tool within look-back → kept as standalone
	body := []byte(`{"messages":[
		{"role":"system","content":"You are Claude."},
		{"role":"user","content":"query"},
		{"role":"assistant","content":"thinking..."},
		{"role":"system","content":"PreToolUse:Read hook additional context: parallel reads."},
		{"role":"user","content":"next question"}
	]}`)

	out := ReanchorHooks(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}

	// No tool before hook → hook kept, message count unchanged
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 5 {
		t.Fatalf("expected 5 messages (no change), got %d: %s", count, string(out))
	}

	found := false
	for i := int64(0); i < count; i++ {
		c := gjson.GetBytes(out, fmt.Sprintf("messages.%d.content", i)).String()
		r := gjson.GetBytes(out, fmt.Sprintf("messages.%d.role", i)).String()
		if r == "system" && strings.Contains(c, "PreToolUse:Read") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("PTU without adjacent tool should be preserved: %s", string(out))
	}
}

func TestReanchor_PostToolUseFailureLookback(t *testing.T) {
	// PostToolUseFailure with a user message between it and the tool → anchored via look-back
	body := []byte(`{"messages":[
		{"role":"system","content":"You are Claude."},
		{"role":"user","content":"query"},
		{"role":"assistant","content":"ok","tool_calls":[{"id":"c1","type":"function","function":{"name":"Bash","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"bash output"},
		{"role":"user","content":"more context"},
		{"role":"system","content":"PostToolUseFailure:mcp__tool hook: Tool error."},
		{"role":"assistant","content":"done"}
	]}`)

	out := ReanchorHooks(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}

	// PostToolUseFailure 2 away from tool, with user in between → anchored (user is transparent)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 6 {
		t.Fatalf("expected 6 messages, got %d: %s", count, string(out))
	}

	// Check that hook was anchored
	for i := int64(0); i < count; i++ {
		c := gjson.GetBytes(out, fmt.Sprintf("messages.%d.content", i)).String()
		r := gjson.GetBytes(out, fmt.Sprintf("messages.%d.role", i)).String()
		if r == "system" && strings.Contains(c, "PostToolUseFailure") {
			t.Fatalf("PostToolUseFailure should be anchored, not standalone at %d: %s", i, c)
		}
	}
}

func TestReanchor_AssistantResetsAnchorChain(t *testing.T) {
	// Assistant message between tool and hook → chain reset → hook NOT anchored
	body := []byte(`{"messages":[
		{"role":"system","content":"You are Claude."},
		{"role":"user","content":"query"},
		{"role":"assistant","content":"ok","tool_calls":[{"id":"c1","type":"function","function":{"name":"Bash","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"bash output"},
		{"role":"assistant","content":"let me think..."},
		{"role":"system","content":"PreToolUse:Read hook additional context: parallel reads."},
		{"role":"assistant","content":"done"}
	]}`)

	out := ReanchorHooks(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}

	// Assistant between tool and PTU → chain reset → hook kept
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 7 {
		t.Fatalf("expected 7 messages (no anchoring), got %d: %s", count, string(out))
	}

	// PTU still standalone
	found := false
	for i := int64(0); i < count; i++ {
		c := gjson.GetBytes(out, fmt.Sprintf("messages.%d.content", i)).String()
		r := gjson.GetBytes(out, fmt.Sprintf("messages.%d.role", i)).String()
		if r == "system" && strings.Contains(c, "PreToolUse:Read") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("PTU after assistant should remain standalone: %s", string(out))
	}
}

func TestReanchor_PreservesConversationContent(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"system","content":"You are Claude."},
		{"role":"user","content":"what is the weather"},
		{"role":"assistant","content":"let me check","tool_calls":[{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"sunny 72F"},
		{"role":"system","content":"PreToolUse:get_weather hook additional context: Verify with secondary source."},
		{"role":"assistant","content":"The weather is sunny, 72F."}
	]}`)

	out := ReanchorHooks(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}

	if gjson.GetBytes(out, "messages.0.content").String() != "You are Claude." {
		t.Fatalf("system prompt changed")
	}
	if gjson.GetBytes(out, "messages.1.content").String() != "what is the weather" {
		t.Fatalf("user query changed")
	}
	if gjson.GetBytes(out, "messages.4.content").String() != "The weather is sunny, 72F." {
		t.Fatalf("assistant response changed")
	}
}

func TestReanchor_StripPostToolUseSuffix(t *testing.T) {
	// PTU system message with embedded PostToolUse suffix → suffix stripped
	body := []byte(`{"messages":[
		{"role":"system","content":"You are Claude."},
		{"role":"user","content":"query"},
		{"role":"assistant","content":"ok","tool_calls":[{"id":"c1","type":"function","function":{"name":"Read","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"output"},
		{"role":"system","content":"PreToolUse:Read hook additional context: parallel reads.\n\nPostToolUse:Read hook additional context: Extensive reading (12 files)."},
		{"role":"assistant","content":"done"}
	]}`)

	out := ReanchorHooks(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}

	toolContent := gjson.GetBytes(out, "messages.3.content").String()
	// Embedded PostToolUse suffix should be stripped from PTU
	if strings.Contains(toolContent, "12 files") {
		t.Fatalf("embedded PostToolUse suffix should be stripped from PTU, got: %s", toolContent)
	}
	// Core PTU content should remain
	if !strings.Contains(toolContent, "parallel reads") {
		t.Fatalf("core PTU content should be preserved: %s", toolContent)
	}
}

func TestReanchor_SystemReminderWrapperStripped(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"system","content":"You are Claude."},
		{"role":"user","content":"query"},
		{"role":"assistant","content":"ok","tool_calls":[{"id":"c1","type":"function","function":{"name":"Edit","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"edited"},
		{"role":"user","content":"<system-reminder>\nPostToolUse:Edit hook additional context: Verify syntax after edit.\n</system-reminder>"},
		{"role":"assistant","content":"done"}
	]}`)

	out := ReanchorHooks(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}

	toolContent := gjson.GetBytes(out, "messages.3.content").String()
	if strings.Contains(toolContent, "<system-reminder>") {
		t.Fatalf("system-reminder wrapper should be stripped, got: %s", toolContent)
	}
	if !strings.Contains(toolContent, "Verify syntax after edit") {
		t.Fatalf("hook text should be preserved: %s", toolContent)
	}
}

func TestReanchor_ArrayHookContentSkipped(t *testing.T) {
	// Hook with array content but no matching text element → skipped, kept as-is
	body := []byte(`{"messages":[
		{"role":"system","content":"You are Claude."},
		{"role":"user","content":"query"},
		{"role":"assistant","content":"ok","tool_calls":[{"id":"c1","type":"function","function":{"name":"Bash","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"output"},
		{"role":"user","content":[{"type":"image","url":"data:..."}]},
		{"role":"assistant","content":"done"}
	]}`)

	out := ReanchorHooks(body)
	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}

	// Array content with no PreToolUse/PostToolUse text → not detected as hook
	// → nothing changed
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 6 {
		t.Fatalf("expected 6 messages (no change), got %d: %s", count, string(out))
	}
}

func TestReanchor_FullPipelineWithReanchor(t *testing.T) {
	// Full pipeline: normalize → dedup → reanchor → canonical
	ptuText := `<system-reminder>
PreToolUse:Read hook additional context: Read multiple files in parallel when possible for faster analysis.
</system-reminder>`
	postText := `<system-reminder>
PostToolUse:Read hook additional context: Extensive reading (5 files).
</system-reminder>`
	body := []byte(`{"messages":[
		{"role":"system","content":"You are Claude."},
		{"role":"user","content":"read the file"},
		{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"Read","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"tool output"},
		{"role":"user","content":[{"type":"text","text":"` + ptuText + `"}]},
		{"role":"user","content":[{"type":"text","text":"` + postText + `"}]},
		{"role":"assistant","content":"end"}
	]}`)
	out := NormalizePreToolUseMessages(body)
	out = DeduplicateSystemMessages(out)
	out = ReanchorHooks(out)
	out = CanonicalizeJSON(out)

	if !gjson.ValidBytes(out) {
		t.Fatalf("output is not valid JSON: %s", string(out))
	}

	// After pipeline: sys, usr, asst, tool(anchored), asst = 5
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 5 {
		t.Fatalf("expected 5 messages, got %d: %s", count, string(out))
	}

	toolContent := gjson.GetBytes(out, "messages.3.content").String()
	if !strings.Contains(toolContent, "PreToolUse:Read") {
		t.Fatalf("tool content should contain PreToolUse hook, got: %s", toolContent)
	}
	if !strings.Contains(toolContent, "PostToolUse:Read") {
		t.Fatalf("tool content should contain PostToolUse hook, got: %s", toolContent)
	}
	if !strings.Contains(toolContent, "parallel") {
		t.Fatalf("tool content should contain PreToolUse body: %s", toolContent)
	}
	// PostToolUse counter preserved
	if !strings.Contains(toolContent, "5 files") {
		t.Fatalf("PostToolUse counter should be preserved, got: %s", toolContent)
	}

	// No standalone hook messages
	sysCount := 0
	for i := int64(0); i < count; i++ {
		r := gjson.GetBytes(out, fmt.Sprintf("messages.%d.role", i)).String()
		if r == "system" {
			sysCount++
		}
	}
	if sysCount != 1 {
		t.Fatalf("expected 1 system message, got %d: %s", sysCount, string(out))
	}

	outStr := string(out)
	hookRoles := 0
	for i := int64(0); i < count; i++ {
		r := gjson.GetBytes(out, fmt.Sprintf("messages.%d.role", i)).String()
		c := gjson.GetBytes(out, fmt.Sprintf("messages.%d.content", i)).String()
		if (r == "system" || r == "user") && (strings.Contains(c, "PreToolUse:") || strings.Contains(c, "PostToolUse:")) {
			hookRoles++
		}
	}
	if hookRoles > 0 {
		t.Fatalf("hooks should be anchored into tool content, not standalone: %s", outStr)
	}
}

func TestSkillListing_NoMessages(t *testing.T) {
	out := ConvertSkillListingToUser([]byte(`{}`))
	if !gjson.ValidBytes(out) {
		t.Fatal("output should be valid JSON")
	}
}

func TestSkillListing_NoSkillListing(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"You are Claude Code."},{"role":"user","content":"hello"}]}`)
	out := ConvertSkillListingToUser(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 2 {
		t.Fatalf("expected 2 messages, got %d", count)
	}
	r0 := gjson.GetBytes(out, "messages.0.role").String()
	if r0 != "system" {
		t.Fatalf("expected system, got %s", r0)
	}
}

func TestSkillListing_SingleConvertedToUser(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"The following skills are available for use with the Skill tool:\n\n- dev-flow: A development workflow skill\n- code-review: Review code"}]}`)
	out := ConvertSkillListingToUser(body)

	r := gjson.GetBytes(out, "messages.0.role").String()
	if r != "user" {
		t.Fatalf("expected role=user, got %s", r)
	}

	c := gjson.GetBytes(out, "messages.0.content").String()
	if !strings.HasPrefix(c, "<system-reminder>\n") {
		t.Fatalf("expected <system-reminder> wrapper, got: %s", c[:min(50, len(c))])
	}
	if !strings.HasSuffix(c, "\n</system-reminder>") {
		t.Fatalf("expected </system-reminder> suffix, got: %s", c[max(0, len(c)-50):])
	}
	if !strings.Contains(c, "The following skills are available") {
		t.Fatal("original content should be preserved inside wrapper")
	}
}

func TestSkillListing_AlreadyUserUnchanged(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"The following skills are available for use with the Skill tool:\n\n- dev-flow: skill"}]}`)
	out := ConvertSkillListingToUser(body)

	r := gjson.GetBytes(out, "messages.0.role").String()
	if r != "user" {
		t.Fatalf("expected role=user, got %s", r)
	}
	// Should NOT double-wrap
	c := gjson.GetBytes(out, "messages.0.content").String()
	if strings.HasPrefix(c, "<system-reminder>\n<system-reminder>") {
		t.Fatal("should not double-wrap already-user message")
	}
}

func TestSkillListing_NonStringContentIgnored(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":[{"type":"text","text":"The following skills are available for use with the Skill tool:\n\n- dev-flow: skill"}]}]}`)
	out := ConvertSkillListingToUser(body)

	r := gjson.GetBytes(out, "messages.0.role").String()
	if r != "system" {
		t.Fatalf("array content should not be converted, got role=%s", r)
	}
}

func TestSkillListing_MixedMessages(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"system","content":"You are Claude Code."},
		{"role":"user","content":"hello"},
		{"role":"system","content":"The following skills are available for use with the Skill tool:\n\n- dev-flow: skill"},
		{"role":"assistant","content":"Hi!"}
	]}`)
	out := ConvertSkillListingToUser(body)

	// [0] system unchanged
	r0 := gjson.GetBytes(out, "messages.0.role").String()
	if r0 != "system" {
		t.Fatalf("msg[0] should stay system, got %s", r0)
	}
	// [1] user unchanged
	r1 := gjson.GetBytes(out, "messages.1.role").String()
	if r1 != "user" {
		t.Fatalf("msg[1] should stay user, got %s", r1)
	}
	// [2] skill listing converted
	r2 := gjson.GetBytes(out, "messages.2.role").String()
	if r2 != "user" {
		t.Fatalf("msg[2] skill listing should be user, got %s", r2)
	}
	c2 := gjson.GetBytes(out, "messages.2.content").String()
	if !strings.HasPrefix(c2, "<system-reminder>") {
		t.Fatal("msg[2] should be wrapped in <system-reminder>")
	}
	// [3] assistant unchanged
	r3 := gjson.GetBytes(out, "messages.3.role").String()
	if r3 != "assistant" {
		t.Fatalf("msg[3] should stay assistant, got %s", r3)
	}
}

func TestSkillListing_Idempotent(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"The following skills are available for use with the Skill tool:\n\n- dev-flow: skill"}]}`)

	first := ConvertSkillListingToUser(body)
	second := ConvertSkillListingToUser(first)

	// Second pass should not change anything (message is already user, detection skips it)
	if string(first) != string(second) {
		t.Fatalf("ConvertSkillListingToUser should be idempotent\nfirst:  %s\nsecond: %s", string(first), string(second))
	}
}

func TestSkillListing_WithTaskCollapse(t *testing.T) {
	// Simulate full pipeline: task collapse then skill convert then unknown collapse
	body := []byte(`{"messages":[
		{"role":"system","content":"The task tools haven't been used recently. If you're working on tasks that would benefit from tracking progress, consider using TaskCreate to add new tasks and TaskUpdate to update task status.\n\nHere are the existing tasks:\n\n#1. [in_progress] Fix cache bug"},
		{"role":"system","content":"The task tools haven't been used recently. If you're working on tasks that would benefit from tracking progress, consider using TaskCreate."},
		{"role":"user","content":"continue"},
		{"role":"system","content":"The following skills are available for use with the Skill tool:\n\n- dev-flow: skill\n- code-review: skill"},
		{"role":"system","content":"The user sent a new message while you were working. Interrupt your current work."},
		{"role":"assistant","content":"ok"}
	]}`)

	// Step 1: CollapseTaskReminders
	out := CollapseTaskReminders(body)
	// Step 2: ConvertSkillListingToUser
	out = ConvertSkillListingToUser(out)
	// Step 3: CollapseUnknownSystemMessages
	out = CollapseUnknownSystemMessages(out)

	outStr := string(out)
	msgs := gjson.GetBytes(out, "messages").Array()

	// Count system messages after pipeline
	sysCount := 0
	userSkillCount := 0
	for _, m := range msgs {
		r := m.Get("role").String()
		if r == "system" {
			sysCount++
		}
		if r == "user" {
			c := m.Get("content").String()
			if strings.HasPrefix(c, "<system-reminder>") && strings.Contains(c, "following skills are available") {
				userSkillCount++
			}
		}
	}

	// System messages should be: PROMPT (not present) + AGENT (not present) + TASK_PREAMBLE (split result) + INTERRUPT (unknown, kept as last)
	if sysCount > 3 {
		t.Fatalf("too many system messages after pipeline: %d\n%s", sysCount, outStr)
	}
	if userSkillCount != 1 {
		t.Fatalf("expected 1 skill listing converted to user, got %d\n%s", userSkillCount, outStr)
	}
}

// TestSkillListing_MultiTurnStability simulates 3 consecutive conversation turns
// to verify the conversion doesn't produce zombies or interfere with pipeline steps.
func TestSkillListing_MultiTurnStability(t *testing.T) {
	// Turn 1: first skill listing appears as system message
	turn1 := []byte(`{"messages":[
		{"role":"system","content":"[PROMPT]"},
		{"role":"system","content":"Available agent types:\n- claude: catch-all"},
		{"role":"user","content":"hello"},
		{"role":"system","content":"The following skills are available for use with the Skill tool:\n\n- dev-flow: A dev workflow\n- code-review: Review code"},
		{"role":"assistant","content":"Hi!"}
	]}`)

	out1 := ConvertSkillListingToUser(turn1)

	// Verify Turn 1: skill listing converted to user
	msgs1 := gjson.GetBytes(out1, "messages").Array()
	if len(msgs1) != 5 {
		t.Fatalf("turn1: expected 5 msgs, got %d", len(msgs1))
	}
	r3 := msgs1[3].Get("role").String()
	if r3 != "user" {
		t.Fatalf("turn1 msg[3]: expected user after conversion, got %s", r3)
	}
	c3 := msgs1[3].Get("content").String()
	if !strings.HasPrefix(c3, "<system-reminder>") {
		t.Fatal("turn1: skill listing should be wrapped")
	}

	// Turn 2: conversation grew. Old skill listing is now history (user),
	// CC injects a NEW skill listing (content changed — skill added).
	turn2 := []byte(`{"messages":[
		{"role":"system","content":"[PROMPT]"},
		{"role":"system","content":"Available agent types:\n- claude: catch-all"},
		{"role":"user","content":"hello"},
		{"role":"user","content":"<system-reminder>\nThe following skills are available for use with the Skill tool:\n\n- dev-flow: A dev workflow\n- code-review: Review code\n</system-reminder>"},
		{"role":"assistant","content":"Hi!"},
		{"role":"user","content":"do code review"},
		{"role":"system","content":"The following skills are available for use with the Skill tool:\n\n- dev-flow: A dev workflow\n- code-review: Review code\n- deploy: Deploy to production"}
	]}`)

	out2 := ConvertSkillListingToUser(turn2)

	// Verify Turn 2:
	msgs2 := gjson.GetBytes(out2, "messages").Array()
	if len(msgs2) != 7 {
		t.Fatalf("turn2: expected 7 msgs, got %d", len(msgs2))
	}
	// [3] old skill listing — already user, should be UNCHANGED (idempotent)
	r3_old := msgs2[3].Get("role").String()
	c3_old := msgs2[3].Get("content").String()
	if r3_old != "user" {
		t.Fatalf("turn2 msg[3]: old listing should stay user, got %s", r3_old)
	}
	if !strings.HasPrefix(c3_old, "<system-reminder>") {
		t.Fatal("turn2 msg[3]: old listing wrapper should be preserved")
	}
	// Should NOT have double-wrapped
	if strings.Count(c3_old, "<system-reminder>") > 1 {
		t.Fatalf("turn2 msg[3]: DOUBLE-WRAPPED! content: %s", c3_old[:200])
	}

	// [6] new skill listing — should be converted
	r6 := msgs2[6].Get("role").String()
	if r6 != "user" {
		t.Fatalf("turn2 msg[6]: new listing should be user, got %s", r6)
	}
	c6 := msgs2[6].Get("content").String()
	if !strings.HasPrefix(c6, "<system-reminder>") {
		t.Fatal("turn2 msg[6]: new listing should be wrapped")
	}

	// Turn 3: no new skill listing injected. Verify stability.
	turn3 := []byte(`{"messages":[
		{"role":"system","content":"[PROMPT]"},
		{"role":"system","content":"Available agent types:\n- claude: catch-all"},
		{"role":"user","content":"hello"},
		{"role":"user","content":"<system-reminder>\nThe following skills are available for use with the Skill tool:\n\n- dev-flow: A dev workflow\n- code-review: Review code\n</system-reminder>"},
		{"role":"assistant","content":"Hi!"},
		{"role":"user","content":"do code review"},
		{"role":"user","content":"<system-reminder>\nThe following skills are available for use with the Skill tool:\n\n- dev-flow: A dev workflow\n- code-review: Review code\n- deploy: Deploy to production\n</system-reminder>"},
		{"role":"assistant","content":"Running code-review..."},
		{"role":"tool","content":"Review complete: no issues","tool_call_id":"call_1"},
		{"role":"assistant","content":"Code review done. All good."},
		{"role":"user","content":"now deploy"}
	]}`)

	out3 := ConvertSkillListingToUser(turn3)

	// Verify Turn 3: no changes — no system skill listing to convert
	msgs3 := gjson.GetBytes(out3, "messages").Array()
	if len(msgs3) != 11 {
		t.Fatalf("turn3: expected 11 msgs, got %d", len(msgs3))
	}
	// [3] and [6] are user-role skill listings from previous turns — should be unchanged
	for _, idx := range []int{3, 6} {
		r := msgs3[idx].Get("role").String()
		if r != "user" {
			t.Fatalf("turn3 msg[%d]: should stay user, got %s", idx, r)
		}
		c := msgs3[idx].Get("content").String()
		if strings.Count(c, "<system-reminder>") > 1 {
			t.Fatalf("turn3 msg[%d]: DOUBLE-WRAPPED!", idx)
		}
	}
	// No system skill listing remains
	for i, m := range msgs3 {
		if m.Get("role").String() == "system" {
			c := m.Get("content").String()
			if strings.HasPrefix(c, skillListPrefix) {
				t.Fatalf("turn3 msg[%d]: system skill listing not converted!", i)
			}
		}
	}
}

// TestSkillListing_FullPipelineStability verifies the skill listing conversion
// doesn't interfere with other pipeline steps across the full CPA pipeline.
func TestSkillListing_FullPipelineStability(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"system","content":"[PROMPT]"},
		{"role":"user","content":"<system-reminder>\nPreToolUse: Bash hook summary\n</system-reminder>"},
		{"role":"system","content":"Available agent types:\n- claude: catch-all"},
		{"role":"user","content":"hello"},
		{"role":"system","content":"The task tools haven't been used recently. If you're working on tasks that would benefit from tracking progress, consider using TaskCreate.\n\nHere are the existing tasks:\n\n#1. [in_progress] Fix bug"},
		{"role":"system","content":"The following skills are available for use with the Skill tool:\n\n- dev-flow: Dev workflow\n- code-review: Review code"},
		{"role":"assistant","content":"ok"}
	]}`)

	// Full pipeline: Norm → Dedup → TaskCollapse → SkillConvert → UnknownCollapse
	out := NormalizePreToolUseMessages(body)
	out = DeduplicateSystemMessages(out)
	out = CollapseSystemNotifications(out)
	out = CollapseTaskReminders(out)
	out = ConvertSkillListingToUser(out)
	out = CollapseUnknownSystemMessages(out)

	msgs := gjson.GetBytes(out, "messages").Array()

	// Verify: no system message contains skill listing prefix
	for i, m := range msgs {
		r := m.Get("role").String()
		c := m.Get("content").String()
		if r == "system" && strings.HasPrefix(c, skillListPrefix) {
			t.Fatalf("msg[%d]: skill listing still system after full pipeline", i)
		}
	}

	// Verify: exactly one user message contains the skill listing wrapped in <system-reminder>
	skillUserCount := 0
	for _, m := range msgs {
		r := m.Get("role").String()
		c := m.Get("content").String()
		if r == "user" && strings.HasPrefix(c, "<system-reminder>") && strings.Contains(c, "following skills are available") {
			skillUserCount++
		}
	}
	if skillUserCount != 1 {
		t.Fatalf("expected 1 skill listing user message, got %d", skillUserCount)
	}

	// Verify: Norm step correctly processed the PreToolUse user message
	hasSystemPTU := false
	for _, m := range msgs {
		r := m.Get("role").String()
		c := m.Get("content").String()
		if r == "system" && strings.Contains(c, "PreToolUse:") {
			hasSystemPTU = true
			break
		}
	}
	if !hasSystemPTU {
		t.Fatal("PreToolUse should have been normalized to system message")
	}

	// Verify: Task reminder was converted to <system-reminder> user message
	hasTaskReminderUser := false
	for _, m := range msgs {
		r := m.Get("role").String()
		c := m.Get("content").String()
		if r == "user" && strings.HasPrefix(c, "<system-reminder>") && strings.Contains(c, taskReminderPrefix) {
			hasTaskReminderUser = true
		}
	}
	if !hasTaskReminderUser {
		t.Fatal("task reminder should be converted to <system-reminder> user message")
	}
}

func TestCombinedSplit_NoMessages(t *testing.T) {
	out := SplitCombinedSystemMessages([]byte(`{}`))
	if !gjson.ValidBytes(out) {
		t.Fatal("output should be valid JSON")
	}
}

func TestCombinedSplit_NoCombinedMessage(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"system","content":"You are Claude Code."},
		{"role":"user","content":"hello"}
	]}`)
	out := SplitCombinedSystemMessages(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 2 {
		t.Fatalf("expected 2 messages, got %d", count)
	}
}

func TestCombinedSplit_SplitsCombinedMessage(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"system","content":"The following skills are available for use with the Skill tool:\n\n- dev-flow: skill\n- code-review: skill\n\nThe task tools haven't been used recently. If you're working on tasks that would benefit from tracking progress, consider using TaskCreate to add new tasks and TaskUpdate to update task status.\n\nHere are the existing tasks:\n\n#1. [completed] Setup\n#2. [in_progress] Fix bug"},
		{"role":"user","content":"hello"}
	]}`)
	out := SplitCombinedSystemMessages(body)

	msgs := gjson.GetBytes(out, "messages").Array()

	// Should have 3 messages: skill(system) + user + task(system appended)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	// [0] should be skill portion only
	r0 := msgs[0].Get("role").String()
	c0 := msgs[0].Get("content").String()
	if r0 != "system" {
		t.Fatalf("msg[0] should be system, got %s", r0)
	}
	if !strings.HasPrefix(c0, skillListPrefix) {
		t.Fatalf("msg[0] should start with skill prefix, got: %s", c0[:50])
	}
	if strings.Contains(c0, taskListMarker) {
		t.Fatal("msg[0] should not contain task list marker after split")
	}
	if strings.Contains(c0, taskReminderPrefix) {
		t.Fatal("msg[0] should not contain task reminder after split")
	}

	// [1] should be unchanged user message
	r1 := msgs[1].Get("role").String()
	if r1 != "user" {
		t.Fatalf("msg[1] should be user, got %s", r1)
	}

	// [2] should be the task reminder appended at end
	r2 := msgs[2].Get("role").String()
	c2 := msgs[2].Get("content").String()
	if r2 != "system" {
		t.Fatalf("msg[2] should be system, got %s", r2)
	}
	if !strings.HasPrefix(c2, taskReminderPrefix) {
		t.Fatalf("msg[2] should start with task prefix, got: %s", c2[:50])
	}
	if !strings.Contains(c2, taskListMarker) {
		t.Fatal("msg[2] should contain task list marker")
	}
}

func TestCombinedSplit_SkillOnlyUnchanged(t *testing.T) {
	// Skill listing without task reminder — should pass through unchanged
	body := []byte(`{"messages":[
		{"role":"system","content":"The following skills are available for use with the Skill tool:\n\n- dev-flow: skill"},
		{"role":"user","content":"hello"}
	]}`)
	out := SplitCombinedSystemMessages(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 2 {
		t.Fatalf("expected 2 messages (unchanged), got %d", count)
	}
	c0 := gjson.GetBytes(out, "messages.0.content").String()
	if !strings.HasPrefix(c0, skillListPrefix) {
		t.Fatal("skill listing should be unchanged")
	}
}

func TestCombinedSplit_TaskOnlyUnchanged(t *testing.T) {
	// Standalone task reminder — should pass through unchanged
	body := []byte(`{"messages":[
		{"role":"system","content":"The task tools haven't been used recently. If you're working on tasks that would benefit from tracking progress.\n\nHere are the existing tasks:\n\n#1. [completed] Setup"},
		{"role":"user","content":"hello"}
	]}`)
	out := SplitCombinedSystemMessages(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 2 {
		t.Fatalf("task-only should be unchanged, got %d", count)
	}
}

func TestCombinedSplit_Idempotent(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"system","content":"The following skills are available for use with the Skill tool:\n\n- dev-flow: skill\n\nThe task tools haven't been used recently. If you're working on tasks.\n\nHere are the existing tasks:\n\n#1. [completed] Setup"},
		{"role":"user","content":"hello"}
	]}`)

	first := SplitCombinedSystemMessages(body)
	second := SplitCombinedSystemMessages(first)

	// Second pass should be no-op: skill portion no longer has taskListMarker
	if string(first) != string(second) {
		t.Fatalf("SplitCombinedSystemMessages should be idempotent\nfirst:  %s\nsecond: %s", string(first), string(second))
	}
}

func TestCombinedSplit_FullPipelineIntegration(t *testing.T) {
	// Simulate the full pipeline with a combined message
	body := []byte(`{"messages":[
		{"role":"system","content":"[PROMPT]"},
		{"role":"system","content":"Available agent types:\n- claude: catch-all"},
		{"role":"user","content":"hello"},
		{"role":"system","content":"The following skills are available for use with the Skill tool:\n\n- dev-flow: skill\n- code-review: skill\n\nThe task tools haven't been used recently. If you're working on tasks that would benefit from tracking progress, consider using TaskCreate.\n\nHere are the existing tasks:\n\n#1. [completed] Setup\n#2. [in_progress] Fix bug"},
		{"role":"system","content":"The task tools haven't been used recently. If you're working on tasks that would benefit from tracking progress.\n\nHere are the existing tasks:\n\n#1. [completed] Setup\n#2. [in_progress] Fix bug"},
		{"role":"assistant","content":"ok"}
	]}`)

	// Full pipeline: Norm → Dedup → CollapseNotif → SplitCombined → CollapseTask → ConvertSkill → UnknownCollapse
	out := NormalizePreToolUseMessages(body)
	out = DeduplicateSystemMessages(out)
	out = CollapseSystemNotifications(out)
	out = SplitCombinedSystemMessages(out)
	out = CollapseTaskReminders(out)
	out = ConvertSkillListingToUser(out)
	out = CollapseUnknownSystemMessages(out)

	msgs := gjson.GetBytes(out, "messages").Array()

	// Verify: no system message contains the skill listing prefix
	for i, m := range msgs {
		r := m.Get("role").String()
		c := m.Get("content").String()
		if r == "system" && strings.HasPrefix(c, skillListPrefix) {
			t.Fatalf("msg[%d]: skill listing still system after full pipeline", i)
		}
	}

	// Verify: skill listing converted to user with <system-reminder>
	skillUserCount := 0
	for _, m := range msgs {
		r := m.Get("role").String()
		c := m.Get("content").String()
		if r == "user" && strings.HasPrefix(c, "<system-reminder>") && strings.Contains(c, "following skills are available") {
			skillUserCount++
		}
	}
	if skillUserCount != 1 {
		t.Fatalf("expected 1 skill listing user message, got %d", skillUserCount)
	}

	// Verify: task reminder was converted to <system-reminder> user message
	hasTaskReminderUser := false
	for _, m := range msgs {
		r := m.Get("role").String()
		c := m.Get("content").String()
		if r == "user" && strings.HasPrefix(c, "<system-reminder>") && strings.Contains(c, taskReminderPrefix) {
			hasTaskReminderUser = true
		}
	}
	if !hasTaskReminderUser {
		t.Fatal("task reminder should be converted to <system-reminder> user message")
	}

	// Verify: task list contains the correct data (from the combined message's task portion)
	// Both task reminders have same content in this test, so the collapsed result should have #2
	if !strings.Contains(string(out), "#2. [in_progress] Fix bug") {
		t.Fatal("task list should contain #2 from the task reminders")
	}
}

func TestCombinedSplit_NonStringContentIgnored(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"system","content":[{"type":"text","text":"The following skills are available for use with the Skill tool:\n\n- dev-flow: skill\n\nThe task tools haven't been used recently."}]},
		{"role":"user","content":"hello"}
	]}`)
	out := SplitCombinedSystemMessages(body)
	count := gjson.GetBytes(out, "messages.#").Int()
	if count != 2 {
		t.Fatalf("array content should be left unchanged, got %d", count)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func TestCountMessagesByRole(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"system","content":"prompt"},
		{"role":"system","content":"agent types"},
		{"role":"user","content":"hello"},
		{"role":"assistant","content":"hi"},
		{"role":"tool","content":"result","tool_call_id":"c1"},
		{"role":"user","content":"thanks"}
	]}`)
	counts := CountMessagesByRole(body)
	if counts["system"] != 2 {
		t.Fatalf("expected 2 system, got %d", counts["system"])
	}
	if counts["user"] != 2 {
		t.Fatalf("expected 2 user, got %d", counts["user"])
	}
	if counts["assistant"] != 1 {
		t.Fatalf("expected 1 assistant, got %d", counts["assistant"])
	}
	if counts["tool"] != 1 {
		t.Fatalf("expected 1 tool, got %d", counts["tool"])
	}
}

func TestCountMessagesByRole_Empty(t *testing.T) {
	counts := CountMessagesByRole([]byte(`{}`))
	if len(counts) != 0 {
		t.Fatalf("expected empty map, got %v", counts)
	}
}

// TestCollapseTask_MultiTurnStability simulates 3 consecutive turns with
// the convert-to-<system-reminder> approach, verifying:
//   - Turn 1: first task reminder converted to <system-reminder> user
//   - Turn 2: new reminder converted; old one stays untouched
//   - Turn 3: no new reminder → no change; no zombie accumulation
//   - CC can read taskListMarker from <system-reminder> user messages
func TestCollapseTask_MultiTurnStability(t *testing.T) {
	// Turn 1: initial request with 1 task reminder
	turn1 := []byte(`{"messages":[
		{"role":"system","content":"[PROMPT]"},
		{"role":"system","content":"Available agent types:\n- claude: catch-all"},
		{"role":"user","content":"hello"},
		{"role":"system","content":"The task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. [in_progress] Setup project"}
	]}`)
	out1 := CollapseTaskReminders(turn1)
	msgs1 := gjson.GetBytes(out1, "messages").Array()

	// Verify: 4 messages (prompt + agent + user + converted task)
	if len(msgs1) != 4 {
		t.Fatalf("turn1: expected 4 messages, got %d", len(msgs1))
	}
	// [3] should be user with <system-reminder> + taskListMarker preserved
	r3 := msgs1[3].Get("role").String()
	c3 := msgs1[3].Get("content").String()
	if r3 != "user" {
		t.Fatalf("turn1[3]: expected user, got %s", r3)
	}
	if !strings.HasPrefix(c3, "<system-reminder>") {
		t.Fatal("turn1[3]: missing <system-reminder> wrapper")
	}
	if !strings.Contains(c3, taskListMarker) {
		t.Fatal("turn1[3]: taskListMarker must be preserved for CC to read task state")
	}
	if !strings.Contains(c3, "Setup project") {
		t.Fatal("turn1[3]: task content lost")
	}

	// Turn 2: conversation grew. Old <system-reminder> is in history.
	// CC injects NEW task reminder with updated state.
	turn2 := []byte(`{"messages":[
		{"role":"system","content":"[PROMPT]"},
		{"role":"system","content":"Available agent types:\n- claude: catch-all"},
		{"role":"user","content":"hello"},
		{"role":"user","content":"<system-reminder>\nThe task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. [in_progress] Setup project\n</system-reminder>"},
		{"role":"assistant","content":"hi"},
		{"role":"user","content":"add feature X"},
		{"role":"system","content":"The task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. [completed] Setup project\n#2. [in_progress] Add feature X"}
	]}`)
	out2 := CollapseTaskReminders(turn2)
	msgs2 := gjson.GetBytes(out2, "messages").Array()

	// Verify: 7 messages (old 6 in history + new converted task at [6])
	if len(msgs2) != 7 {
		t.Fatalf("turn2: expected 7 messages, got %d", len(msgs2))
	}
	// [3] old task — should still be user with old state
	r3old := msgs2[3].Get("role").String()
	c3old := msgs2[3].Get("content").String()
	if r3old != "user" {
		t.Fatalf("turn2[3]: old task should stay user, got %s", r3old)
	}
	if !strings.Contains(c3old, "Setup project") {
		t.Fatal("turn2[3]: old task content should be preserved")
	}
	if strings.Count(c3old, "<system-reminder>") > 1 {
		t.Fatal("turn2[3]: DOUBLE-WRAPPED old task message")
	}
	// [6] new task — should be user with updated state
	r6 := msgs2[6].Get("role").String()
	c6 := msgs2[6].Get("content").String()
	if r6 != "user" {
		t.Fatalf("turn2[6]: new task should be user, got %s", r6)
	}
	if !strings.Contains(c6, "Add feature X") {
		t.Fatal("turn2[6]: should have latest task (Add feature X)")
	}
	if !strings.Contains(c6, "completed") {
		t.Fatal("turn2[6]: should reflect completed status")
	}
	if !strings.Contains(c6, taskListMarker) {
		t.Fatal("turn2[6]: taskListMarker must be preserved for CC")
	}

	// Turn 3: no new task reminder. Body unchanged.
	turn3 := []byte(`{"messages":[
		{"role":"system","content":"[PROMPT]"},
		{"role":"system","content":"Available agent types:\n- claude: catch-all"},
		{"role":"user","content":"hello"},
		{"role":"user","content":"<system-reminder>\nThe task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. [in_progress] Setup project\n</system-reminder>"},
		{"role":"assistant","content":"hi"},
		{"role":"user","content":"add feature X"},
		{"role":"user","content":"<system-reminder>\nThe task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. [completed] Setup project\n#2. [in_progress] Add feature X\n</system-reminder>"},
		{"role":"assistant","content":"working..."},
		{"role":"user","content":"status?"}
	]}`)
	out3 := CollapseTaskReminders(turn3)

	// Turn 3: no system task reminders → body unchanged
	if string(out3) != string(turn3) {
		t.Fatalf("turn3: should be unchanged (no system task reminders)\ngot: %s", string(out3))
	}
}

// TestCollapseTask_FullPipelineStability verifies the complete CPA pipeline
// with the new convert approach doesn't break any other step.
func TestCollapseTask_FullPipelineStability(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"system","content":"[PROMPT]"},
		{"role":"system","content":"Available agent types:\n- claude: catch-all"},
		{"role":"user","content":"hello"},
		{"role":"system","content":"The task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. [in_progress] Fix bug\n#2. [pending] Add test"},
		{"role":"system","content":"The following skills are available for use with the Skill tool:\n\n- dev-flow: skill"},
		{"role":"system","content":"The user sent a new message while you were working"},
		{"role":"system","content":"The task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. [completed] Fix bug\n#2. [in_progress] Add test\n#3. [pending] Deploy"},
		{"role":"assistant","content":"ok"}
	]}`)

	// Full pipeline without Norm (Norm operates on user-role PTU patterns, not tested here)
	out := DeduplicateSystemMessages(body)
	out = CollapseSystemNotifications(out)
	out = CollapseTaskReminders(out)
	out = ConvertSkillListingToUser(out)
	out = CollapseUnknownSystemMessages(out)

	msgs := gjson.GetBytes(out, "messages").Array()

	taskUserCount := 0
	skillUserCount := 0
	unknownCount := 0
	taskSysCount := 0

	for _, m := range msgs {
		r := m.Get("role").String()
		c := m.Get("content").String()

		// Task: should be <system-reminder> user, NOT system
		if r == "system" && strings.HasPrefix(c, taskReminderPrefix) {
			taskSysCount++
		}
		if r == "user" && strings.HasPrefix(c, "<system-reminder>") && strings.Contains(c, taskReminderPrefix) {
			taskUserCount++
			if !strings.Contains(c, taskListMarker) {
				t.Fatalf("task reminder missing taskListMarker: %s", c[:200])
			}
			if !strings.Contains(c, "Deploy") {
				t.Fatalf("should contain latest task (Deploy), got: %s", c[:200])
			}
		}
		// Skill: should be <system-reminder> user
		if r == "user" && strings.HasPrefix(c, "<system-reminder>") && strings.Contains(c, "following skills are available") {
			skillUserCount++
		}
		// Unknown: should be collapsed to 1 system
		if r == "system" && strings.HasPrefix(c, "The user sent a new message") {
			unknownCount++
		}
	}

	if taskSysCount != 0 {
		t.Fatalf("Task: expected 0 system task reminders, got %d", taskSysCount)
	}
	if taskUserCount != 1 {
		t.Fatalf("Task: expected 1 <system-reminder> user task reminder, got %d", taskUserCount)
	}
	if skillUserCount != 1 {
		t.Fatalf("Skill: expected 1 <system-reminder> user skill, got %d", skillUserCount)
	}
	if unknownCount != 1 {
		t.Fatalf("Unknown: expected 1 unknown message (collapsed from 2), got %d", unknownCount)
	}
}

// TestCollapseTask_CCScannerCompat verifies that CC task state scanner
// can find taskListMarker in the converted <system-reminder> user messages.
// CC's own injection format is <system-reminder> user messages, so its scanner
// must support reading task state from them.
func TestCollapseTask_CCScannerCompat(t *testing.T) {
	// Simulate what CC sees after CPA conversion:
	// All task reminders are in <system-reminder> user messages with taskListMarker preserved.
	history := []byte(`{"messages":[
		{"role":"system","content":"[PROMPT]"},
		{"role":"user","content":"<system-reminder>\nThe task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. [completed] Old task\n</system-reminder>"},
		{"role":"assistant","content":"done"},
		{"role":"user","content":"<system-reminder>\nThe task tools haven't been used recently.\n\nHere are the existing tasks:\n\n#1. [completed] Old task\n#2. [in_progress] New task\n</system-reminder>"},
		{"role":"assistant","content":"working"}
	]}`)

	// A scanner looking for the LATEST task state should find:
	// - The last <system-reminder> user message containing taskListMarker
	// - Should contain the most complete task list (Task #2)
	msgs := gjson.GetBytes(history, "messages").Array()

	var lastTaskContent string
	for i := len(msgs) - 1; i >= 0; i-- {
		r := msgs[i].Get("role").String()
		c := msgs[i].Get("content").String()
		if r == "user" && strings.HasPrefix(c, "<system-reminder>") && strings.Contains(c, taskListMarker) {
			lastTaskContent = c
			break
		}
	}

	if lastTaskContent == "" {
		t.Fatal("CC scanner: should find task state in <system-reminder> user messages")
	}
	if !strings.Contains(lastTaskContent, "New task") {
		t.Fatalf("CC scanner: should find latest task, got: %s", lastTaskContent)
	}
	if !strings.Contains(lastTaskContent, "Old task") {
		t.Fatal("CC scanner: should preserve complete task history")
	}
}
