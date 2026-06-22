package helps

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// DeduplicateSystemMessages removes duplicate system messages with
// byte-identical content from the messages array. For each unique content,
// only the first occurrence is kept; all subsequent identical copies are
// removed regardless of position. Messages with other roles (user,
// assistant, tool) are never touched.
//
// This is a lossless transformation: a system message with the same exact
// content conveys no additional information beyond its first appearance.
//
// Logs dedup statistics at debug level when any removal occurs.
func DeduplicateSystemMessages(body []byte) []byte {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() || len(messages.Array()) < 2 {
		return body
	}

	arr := messages.Array()
	type msgEntry struct {
		idx     int
		role    string
		content string
	}
	entries := make([]msgEntry, 0, len(arr))
	origSysCount := 0

	for i := range arr {
		role := arr[i].Get("role").String()
		content := arr[i].Get("content").Raw
		entries = append(entries, msgEntry{
			idx:     i,
			role:    role,
			content: content,
		})
		if role == "system" {
			origSysCount++
		}
	}

	if origSysCount < 2 {
		return body
	}

	toDelete := make([]int, 0, origSysCount)
	seenContent := make(map[string]int, origSysCount)

	for _, e := range entries {
		if e.role != "system" {
			continue
		}
		if firstIdx, ok := seenContent[e.content]; ok {
			log.WithFields(log.Fields{
				"module":       "system_dedup",
				"first_at":     firstIdx,
				"current_at":   e.idx,
				"content_hash": fmt.Sprintf("%x", hashContent(e.content)),
			}).Debug("system_dedup: marking duplicate system message for removal")
			toDelete = append(toDelete, e.idx)
			continue
		}
		seenContent[e.content] = e.idx
	}

	if len(toDelete) == 0 {
		return body
	}

	out := body
	for i := len(toDelete) - 1; i >= 0; i-- {
		path := fmt.Sprintf("messages.%d", toDelete[i])
		var err error
		out, err = sjson.DeleteBytes(out, path)
		if err != nil {
			log.WithField("module", "system_dedup").Warnf("dedup: failed to delete messages.%d: %v", toDelete[i], err)
			return body
		}
	}

	log.WithFields(log.Fields{
		"module":  "system_dedup",
		"removed": len(toDelete),
		"from":    origSysCount,
		"to":      origSysCount - len(toDelete),
	}).Debug("system_dedup: removed duplicate system messages [cpa-dedup]")

	return out
}

// taskReminderPrefix is the fixed preamble Claude Code uses for periodic
// task tool reminders. CollapseTaskReminders uses it for identification.
const taskReminderPrefix = "The task tools haven't been used recently"

// taskListMarker separates the stable preamble from the variable task list
// inside CC's task reminders.
const taskListMarker = "\n\nHere are the existing tasks:"

// anchorTag is placed in the system reminder preamble after the task list
// is extracted, telling the model where to find it.
const anchorTag = "\n\n[current task list follows in conversation]"

// CollapseTaskReminders collapses multiple Claude Code task reminder
// system messages into a single message and splits it into two parts:
//
//   - The stable preamble stays as a role=system message, keeping its
//     high attention priority in DeepSeek's system-prompt block.
//   - The variable task list is extracted into a separate role=user
//     message, wrapped in [task-list] markers. If a task-list user
//     message from a previous split already exists its content is
//     replaced rather than duplicated.
//
// On subsequent requests the split is idempotent: the preamble no
// longer contains the task-list marker, so splitTaskReminder returns
// an empty taskList and the function returns without changes.
//
// This solves the KV cache problem: the system block (preamble only)
// is now byte-stable across all requests, so the tools section never
// shifts. The model retains access to both the task-tool prompt
// (system, high priority) and the current task list (user, stable cache).
//
// Logs at debug level when reminders are collapsed or split
// [cpa-task-collapse].
func CollapseTaskReminders(body []byte) []byte {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() || len(messages.Array()) < 2 {
		return body
	}

	arr := messages.Array()
	reminderIdxs := make([]int, 0)
	var fullContent string

	for i := range arr {
		role := arr[i].Get("role").String()
		if role != "system" {
			continue
		}
		content := arr[i].Get("content")
		if content.Type == gjson.String && strings.HasPrefix(content.String(), taskReminderPrefix) {
			reminderIdxs = append(reminderIdxs, i)
			fullContent = content.String()
		}
	}

	if len(reminderIdxs) == 0 {
		return body
	}

	origKeepIdx := reminderIdxs[len(reminderIdxs)-1]

	out := body

	if len(reminderIdxs) > 1 {
		removeIdxs := reminderIdxs[:len(reminderIdxs)-1]

		for _, idx := range removeIdxs {
			log.WithFields(log.Fields{
				"module":  "system_dedup",
				"at":      idx,
				"keep_at": origKeepIdx,
			}).Debug("system_dedup: collapsing stale task reminder [cpa-task-collapse]")
		}

		collapsed := 0
		for i := len(removeIdxs) - 1; i >= 0; i-- {
			path := fmt.Sprintf("messages.%d", removeIdxs[i])
			var err error
			out, err = sjson.DeleteBytes(out, path)
			if err != nil {
				log.WithField("module", "system_dedup").Warnf("task-collapse: failed to delete messages.%d: %v", removeIdxs[i], err)
				return body
			}
			collapsed++
		}

		log.WithFields(log.Fields{
			"module":    "system_dedup",
			"collapsed": collapsed,
		}).Debug("system_dedup: collapsed task reminders [cpa-task-collapse]")
	}

	// Re-parse after deletions to find the adjusted keep index.
	outMsgs := gjson.GetBytes(out, "messages").Array()
	keepIdx := -1
	for i, m := range outMsgs {
		if m.Get("role").String() == "system" &&
			m.Get("content").Type == gjson.String &&
			strings.HasPrefix(m.Get("content").String(), taskReminderPrefix) {
			if m.Get("content").String() == fullContent || keepIdx < 0 {
				keepIdx = i
			}
		}
	}
	if keepIdx < 0 {
		return out
	}

	keepContent := outMsgs[keepIdx].Get("content").String()
	preamble, taskList := splitTaskReminder(keepContent)
	if taskList == "" {
		// Already split or no task list marker — idempotent, nothing to do.
		return out
	}

	// Replace the system reminder content with the stable preamble + anchor.
	contentPath := fmt.Sprintf("messages.%d.content", keepIdx)
	var setErr error
	out, setErr = sjson.SetBytes(out, contentPath, preamble+anchorTag)
	if setErr != nil {
		log.WithField("module", "system_dedup").Warnf("task-collapse: failed to set preamble for messages.%d: %v", keepIdx, setErr)
		return body
	}

	// Build the user message carrying the variable task list.
	taskListMsg, err := json.Marshal(map[string]interface{}{
		"role":    "user",
		"content": "[task-list]\n" + taskList + "\n[/task-list]",
	})
	if err != nil {
		log.WithField("module", "system_dedup").Warnf("task-collapse: failed to marshal task-list message: %v", err)
		return body
	}

	// Scan all task-list user messages from previous splits.
	// Delete every zombie copy except the last, then replace its content.
	taskListIdxs := make([]int, 0)
	for i := len(outMsgs) - 1; i >= 0; i-- {
		r := outMsgs[i].Get("role").String()
		c := outMsgs[i].Get("content")
		if r == "user" && c.Type == gjson.String && strings.HasPrefix(c.String(), "[task-list]") {
			taskListIdxs = append([]int{i}, taskListIdxs...)
		}
	}

	replaced := len(taskListIdxs) > 0
	if len(taskListIdxs) > 1 {
		// Delete all but the last zombie copy.
		zombies := taskListIdxs[:len(taskListIdxs)-1]
		for i := len(zombies) - 1; i >= 0; i-- {
			path := fmt.Sprintf("messages.%d", zombies[i])
			var delErr error
			out, delErr = sjson.DeleteBytes(out, path)
			if delErr != nil {
				log.WithField("module", "system_dedup").Warnf("task-collapse: failed to delete zombie task-list messages.%d: %v", zombies[i], delErr)
				return body
			}
		}
		log.WithFields(log.Fields{
			"module":  "system_dedup",
			"zombies": len(zombies),
		}).Debug("system_dedup: removed zombie task-list copies [cpa-task-collapse]")
	}

	// Scan again after deletions to find the surviving task-list index.
	outMsgs2 := gjson.GetBytes(out, "messages").Array()
	taskListPath := ""
	for i := len(outMsgs2) - 1; i >= 0; i-- {
		if outMsgs2[i].Get("role").String() == "user" {
			c := outMsgs2[i].Get("content")
			if c.Type == gjson.String && strings.HasPrefix(c.String(), "[task-list]") {
				taskListPath = fmt.Sprintf("messages.%d.content", i)
				break
			}
		}
	}

	if taskListPath != "" {
		raw := gjson.GetBytes(taskListMsg, "content").Raw
		out, setErr = sjson.SetRawBytes(out, taskListPath, []byte(raw))
	} else {
		out, setErr = sjson.SetRawBytes(out, "messages.-1", taskListMsg)
	}
	if setErr != nil {
		log.WithField("module", "system_dedup").Warnf("task-collapse: failed to write task-list message: %v", setErr)
		return body
	}

	log.WithFields(log.Fields{
		"module":         "system_dedup",
		"sys_at":         keepIdx,
		"list_extracted": len(taskList),
		"list_replaced":  replaced,
	}).Debug("system_dedup: split task reminder into system preamble + user task-list [cpa-task-collapse]")

	return out
}

// splitTaskReminder separates a CC task reminder into its stable preamble
// and variable task list. Returns the preamble and the extracted task list
// (without the "Here are the existing tasks:" header). If no task list
// marker is found, the entire content is returned as preamble and taskList
// is empty.
func splitTaskReminder(content string) (preamble, taskList string) {
	idx := strings.Index(content, taskListMarker)
	if idx < 0 {
		return content, ""
	}
	preamble = strings.TrimRight(content[:idx], "\n\r")
	raw := strings.TrimLeft(content[idx+len(taskListMarker):], "\n\r ")
	if raw == "" {
		return content, ""
	}
	return preamble, raw
}

// sysNotificationPrefix identifies CC background-task completion notifications
// injected as role=system. CollapseSystemNotifications uses it for detection.
const sysNotificationPrefix = "[SYSTEM NOTIFICATION"

// CollapseSystemNotifications collapses multiple CC background-task
// completion notification system messages into one (the most recent)
// and normalizes its content to a stable byte form.
//
// CC injects a unique system notification for every background command
// that finishes. Each has different task details (<task-id>, <output-file>,
// <summary>) and CC occasionally appends a task reminder after the
// </task-notification> tag. Both forms of variation change the
// system-prompt block token count and shift the tools section out of
// KV cache alignment.
//
// After collapsing to a single notification, the function strips the
// <task-notification> XML block and everything after it, replacing them
// with a fixed placeholder. The model retains the high-attention
// system-level "background task completed" signal while the specific
// output (already consumed via tool results) stays visible in the
// conversation.
//
// Logs at debug level when notifications are collapsed or normalized
// [cpa-task-collapse].
func CollapseSystemNotifications(body []byte) []byte {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() || len(messages.Array()) < 2 {
		return body
	}

	arr := messages.Array()
	notifIdxs := make([]int, 0)

	for i := range arr {
		role := arr[i].Get("role").String()
		if role != "system" {
			continue
		}
		content := arr[i].Get("content")
		if content.Type == gjson.String && strings.HasPrefix(content.String(), sysNotificationPrefix) {
			notifIdxs = append(notifIdxs, i)
		}
	}

	if len(notifIdxs) == 0 {
		return body
	}

	out := body

	// Collapse: keep only the last notification.
	if len(notifIdxs) > 1 {
		keepIdx := notifIdxs[len(notifIdxs)-1]
		removeIdxs := notifIdxs[:len(notifIdxs)-1]

		for _, idx := range removeIdxs {
			log.WithFields(log.Fields{
				"module":  "system_dedup",
				"at":      idx,
				"keep_at": keepIdx,
			}).Debug("system_dedup: collapsing stale system notification [cpa-task-collapse]")
		}

		collapsed := 0
		for i := len(removeIdxs) - 1; i >= 0; i-- {
			path := fmt.Sprintf("messages.%d", removeIdxs[i])
			var err error
			out, err = sjson.DeleteBytes(out, path)
			if err != nil {
				log.WithField("module", "system_dedup").Warnf("task-collapse: failed to delete notification messages.%d: %v", removeIdxs[i], err)
				return body
			}
			collapsed++
		}

		log.WithFields(log.Fields{
			"module":    "system_dedup",
			"collapsed": collapsed,
		}).Debug("system_dedup: collapsed system notifications [cpa-task-collapse]")
	}

	// Normalize: strip the variable <task-notification> block from the
	// surviving notification so every request produces identical tokens.
	keepIdx := notifIdxs[len(notifIdxs)-1]
	if len(notifIdxs) > 1 {
		keepIdx -= len(notifIdxs) - 1
	}

	keepContent := arr[notifIdxs[len(notifIdxs)-1]].Get("content").String()
	if normalized := normalizeNotification(keepContent); normalized != keepContent {
		contentPath := fmt.Sprintf("messages.%d.content", keepIdx)
		var setErr error
		out, setErr = sjson.SetBytes(out, contentPath, normalized)
		if setErr != nil {
			log.WithField("module", "system_dedup").Warnf("task-collapse: failed to normalize notification messages.%d: %v", keepIdx, setErr)
			return body
		}

		log.WithFields(log.Fields{
			"module":     "system_dedup",
			"at":         keepIdx,
			"trimmed_by": len(keepContent) - len(normalized),
		}).Debug("system_dedup: normalized notification content [cpa-task-collapse]")
	}

	return out
}

// notificationPlaceholder replaces the variable per-task XML block inside
// CC system notifications so the content is byte-stable across requests.
const notificationPlaceholder = "\n\n[background task completed — output in conversation]"

// normalizeNotification strips the <task-notification> XML block and
// everything after it from a system notification, replacing the tail
// with a fixed placeholder. Returns the input unchanged if the marker
// is not found.
func normalizeNotification(content string) string {
	idx := strings.Index(content, "\n<task-notification>")
	if idx < 0 {
		return content
	}
	return strings.TrimRight(content[:idx], "\n\r") + notificationPlaceholder
}

// CollapseUnknownSystemMessages keeps only the most recent system message
// that is not already handled by the targeted collapse functions (PROMPT,
// AGENT, task reminders, and notifications). This covers any CC-injected
// message type we haven't explicitly catalogued — e.g. user-interruption
// notifications ("The user sent a new message while you were working").
//
// Without this, each new CC-injected system message permanently increments
// the syncount and causes a KV cache snowball.
//
// Logs at debug level when unknown messages are collapsed [cpa-task-collapse].
func CollapseUnknownSystemMessages(body []byte) []byte {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() || len(messages.Array()) < 2 {
		return body
	}

	arr := messages.Array()
	unknownIdxs := make([]int, 0)

	for i := range arr {
		role := arr[i].Get("role").String()
		if role != "system" {
			continue
		}
		content := arr[i].Get("content")
		if content.Type != gjson.String {
			continue
		}
		s := content.String()
		if strings.HasPrefix(s, "Available agent types") ||
			strings.HasPrefix(s, taskReminderPrefix) ||
			strings.HasPrefix(s, sysNotificationPrefix) {
			continue
		}
		unknownIdxs = append(unknownIdxs, i)
	}

	if len(unknownIdxs) <= 1 {
		return body
	}

	keepIdx := unknownIdxs[len(unknownIdxs)-1]
	removeIdxs := unknownIdxs[:len(unknownIdxs)-1]

	for _, idx := range removeIdxs {
		log.WithFields(log.Fields{
			"module":  "system_dedup",
			"at":      idx,
			"keep_at": keepIdx,
		}).Debug("system_dedup: collapsing unknown system message [cpa-task-collapse]")
	}

	out := body
	collapsed := 0
	for i := len(removeIdxs) - 1; i >= 0; i-- {
		path := fmt.Sprintf("messages.%d", removeIdxs[i])
		var err error
		out, err = sjson.DeleteBytes(out, path)
		if err != nil {
			log.WithField("module", "system_dedup").Warnf("task-collapse: failed to delete unknown messages.%d: %v", removeIdxs[i], err)
			return body
		}
		collapsed++
	}

	log.WithFields(log.Fields{
		"module":  "system_dedup",
		"removed": collapsed,
		"kept":    keepIdx,
	}).Debug("system_dedup: collapsed unknown system messages [cpa-task-collapse]")

	return out
}

// isKnownSystemMessage returns true for CC-injected system message types
// that are already covered by targeted collapse functions (and therefore
// should be excluded from the generic unknown-message collapse).
func isKnownSystemMessage(content gjson.Result) bool {
	if content.Type != gjson.String {
		return false
	}
	s := content.String()
	return strings.HasPrefix(s, "Available agent types") ||
		strings.HasPrefix(s, taskReminderPrefix) ||
		strings.HasPrefix(s, sysNotificationPrefix)
}

// AppendEphemeralSystemMessages moves PreToolUse/PostToolUse hook context
// system messages to the end of the messages array. These are meta-instructions
// from CC's hook system injected between tool_result and the next assistant
// response. Moving them to the end preserves the byte-identical prefix of the
// messages array, which dramatically improves upstream KV cache reuse.
//
// Detection: role=system, string content containing "PreToolUse:".
// Only these messages are moved; all other messages stay at their original
// positions relative to each other. Moved messages retain their original order.
//
// Safety: PreToolUse is a general behavioral instruction, PostToolUse is a
// within-cycle summary. Neither references cross-message history.
// The model sees all prompt tokens during prefill regardless of position.
//
// Logs at debug level when messages are moved.
func AppendEphemeralSystemMessages(body []byte) []byte {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() || len(messages.Array()) == 0 {
		return body
	}

	arr := messages.Array()
	var ephemeralBuf bytes.Buffer
	var stableBuf bytes.Buffer
	ephemeralCount := 0
	firstStable := true
	firstEph := true

	for i := range arr {
		role := arr[i].Get("role").String()
		target := &stableBuf

		if role == "system" {
			content := arr[i].Get("content")
			if content.Type == gjson.String && strings.Contains(content.String(), "PreToolUse:") {
				target = &ephemeralBuf
				ephemeralCount++
			}
		}

		if target == &stableBuf {
			if !firstStable {
				stableBuf.WriteByte(',')
			}
			firstStable = false
		} else {
			if !firstEph {
				ephemeralBuf.WriteByte(',')
			}
			firstEph = false
		}

		target.WriteString(arr[i].Raw)
	}

	if ephemeralCount == 0 {
		return body
	}

	// Build new messages array: [stable..., ephemeral...]
	var out bytes.Buffer
	out.WriteByte('[')
	out.Write(stableBuf.Bytes())
	if stableBuf.Len() > 0 && ephemeralBuf.Len() > 0 {
		out.WriteByte(',')
	}
	out.Write(ephemeralBuf.Bytes())
	out.WriteByte(']')

	result, err := sjson.SetRawBytes(body, "messages", out.Bytes())
	if err != nil {
		log.WithField("module", "system_dedup").Warnf("eph: failed to set messages: %v", err)
		return body
	}

	log.WithFields(log.Fields{
		"module":  "system_dedup",
		"moved":   ephemeralCount,
		"total":   len(arr),
		"new_pos": len(arr) - ephemeralCount,
	}).Debug("system_dedup: moved ephemeral system messages to end [cpa-eph]")

	return result
}

// isSystemInjectedPTU checks if text is a CC system-injected PreToolUse
// reminder. Uses a three-layer guard to minimize false positives:
//  1. Text starts with "<system-reminder>" (after trimming whitespace)
//  2. Text contains "PreToolUse:"
//  3. Text ends with "</system-reminder>" (after trimming whitespace)
func isSystemInjectedPTU(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, "<system-reminder>") &&
		strings.Contains(text, "PreToolUse:") &&
		strings.HasSuffix(text, "</system-reminder>")
}

// isHookInjectedUserString detects CC hook content injected as a raw string
// in a user-role message (without content array). CC uses this format
// when merging multiple hook blocks into a single string.
//
// Detection:
//  1. String starts with "PreToolUse:" (raw format, no <system-reminder>)
//  2. String contains "<system-reminder>" and "PreToolUse:"
func isHookInjectedUserString(text string) bool {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "PreToolUse:") {
		return true
	}
	if strings.Contains(text, "<system-reminder>") && strings.Contains(text, "PreToolUse:") {
		return true
	}
	return false
}

// stripSystemReminder removes the <system-reminder>...</system-reminder>
// wrapper and trims whitespace from the content.
//
// Precondition: text has been validated by isSystemInjectedPTU and is
// guaranteed to contain a <system-reminder> and </system-reminder> pair.
// The function is NOT safe to call on arbitrary input.
func stripSystemReminder(text string) string {
	idx := strings.Index(text, ">")
	if idx >= 0 {
		text = text[idx+1:]
	}
	idx = strings.LastIndex(text, "<")
	if idx >= 0 {
		text = text[:idx]
	}
	return strings.TrimSpace(text)
}

// normalizeHookString extracts the core PTU content from a hook-injected
// user string. If the string is <system-reminder>-wrapped it is stripped;
// otherwise the full text is used as the system PTU content.
func normalizeHookString(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "<system-reminder>") {
		return stripSystemReminder(text)
	}
	return text
}

// classifyContentElements separates content array elements into PreToolUse
// and non-PTU groups. PTU elements are detected via isSystemInjectedPTU.
func classifyContentElements(elems []gjson.Result) (ptuTexts []string, nonPtuRaws []string) {
	for _, elem := range elems {
		if elem.Get("type").String() == "text" && isSystemInjectedPTU(elem.Get("text").String()) {
			ptuTexts = append(ptuTexts, stripSystemReminder(elem.Get("text").String()))
		} else {
			nonPtuRaws = append(nonPtuRaws, elem.Raw)
		}
	}
	return
}

// emitRaw appends a raw JSON message fragment to the buffer.
func emitRaw(buf *bytes.Buffer, first *bool, raw string) {
	if !*first {
		buf.WriteByte(',')
	}
	*first = false
	buf.WriteString(raw)
}

// emitSystemPTU appends a system-role PTU message to the buffer.
func emitSystemPTU(buf *bytes.Buffer, first *bool, content string) {
	if !*first {
		buf.WriteByte(',')
	}
	*first = false
	buf.WriteString(`{"role":"system","content":`)
	quoted, _ := json.Marshal(content)
	buf.Write(quoted)
	buf.WriteByte('}')
}

// emitUserArrayMsg appends a user-role message with array content built
// from raw element fragments to the buffer.
func emitUserArrayMsg(buf *bytes.Buffer, first *bool, elems []string) {
	if !*first {
		buf.WriteByte(',')
	}
	*first = false
	buf.WriteString(`{"role":"user","content":[`)
	for i, e := range elems {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString(e)
	}
	buf.WriteString(`]}`)
}

// emitUserStringAsArray converts a user-role message with plain string
// content into the equivalent array-text format, so that CC's format
// switching between "content":"text" and "content":[{"type":"text","text":"text"}]
// produces byte-stable output for upstream KV cache prefix matching.
func emitUserStringAsArray(buf *bytes.Buffer, first *bool, text string) {
	if !*first {
		buf.WriteByte(',')
	}
	*first = false
	buf.WriteString(`{"role":"user","content":[{"type":"text","text":`)
	quoted, _ := json.Marshal(text)
	buf.Write(quoted)
	buf.WriteString(`}]}`)
}

// NormalizePreToolUseMessages extracts PreToolUse hook context embedded in
// user-role messages and promotes them to standalone system messages. This
// normalizes structural differences between CC's PreToolUse injection
// formats so that consecutive requests produce byte-stable prefixes for
// upstream KV cache reuse.
//
// Three injection formats are handled:
//   - Single-element content array: a user message whose content is a
//     one-element array containing <system-reminder>-wrapped PTU text.
//     The element is extracted and the user message is replaced by a
//     system-role PTU at the same position.
//   - Multi-element content array: a user message whose content array
//     mixes PTU elements with non-PTU elements (e.g. PostToolUse, tool
//     results). PTU elements are extracted into system messages at the
//     deletion position; remaining non-PTU elements are rebuilt as a
//     user message immediately after.
//   - Raw string content: a user message whose content is a plain string
//     starting with "PreToolUse:" (CC's merged-hook format). The entire
//     string is promoted to a system-role message at the same position.
//
// Extracted PTU content is deduplicated globally within the request: if
// two user-role messages contain identical PTU text, only one system
// message is created per unique text.
//
// Logs at debug level when messages are normalized [cpa-norm].
func NormalizePreToolUseMessages(body []byte) []byte {
	msgs := gjson.GetBytes(body, "messages")
	if !msgs.IsArray() || len(msgs.Array()) < 2 {
		return body
	}

	arr := msgs.Array()
	seenPTU := make(map[string]bool)
	changed := false

	var buf bytes.Buffer
	buf.WriteByte('[')
	first := true

	for _, m := range arr {
		if m.Get("role").String() != "user" {
			emitRaw(&buf, &first, m.Raw)
			continue
		}

		content := m.Get("content")

		if content.IsArray() {
			elems := content.Array()
			if len(elems) == 1 {
				elem := elems[0]
				if elem.Get("type").String() == "text" && isSystemInjectedPTU(elem.Get("text").String()) {
					changed = true
					pt := stripSystemReminder(elem.Get("text").String())
					if !seenPTU[pt] {
						seenPTU[pt] = true
						emitSystemPTU(&buf, &first, pt)
					}
					continue
				}
			} else if len(elems) > 1 {
				ptuTexts, nonPtuRaws := classifyContentElements(elems)
				if len(ptuTexts) > 0 {
					changed = true
					for _, pt := range ptuTexts {
						if !seenPTU[pt] {
							seenPTU[pt] = true
							emitSystemPTU(&buf, &first, pt)
						}
					}
					if len(nonPtuRaws) > 0 {
						emitUserArrayMsg(&buf, &first, nonPtuRaws)
					}
					continue
				}
			}
		} else if content.Type == gjson.String {
			text := content.String()
			if isHookInjectedUserString(text) {
				changed = true
				ptuText := normalizeHookString(text)
				if ptuText != "" && !seenPTU[ptuText] {
					seenPTU[ptuText] = true
					emitSystemPTU(&buf, &first, ptuText)
				}
				continue
			}
			// Normalize user string content to array format so that CC's
			// format switching (string ↔ array) between rounds does not
			// break upstream KV cache prefix identity.
			changed = true
			emitUserStringAsArray(&buf, &first, text)
			continue
		}

		emitRaw(&buf, &first, m.Raw)
	}

	buf.WriteByte(']')

	if !changed {
		return body
	}

	result, err := sjson.SetRawBytes(body, "messages", buf.Bytes())
	if err != nil {
		log.WithField("module", "system_dedup").Warnf("norm: failed to set messages: %v", err)
		return body
	}

	log.WithFields(log.Fields{
		"module": "system_dedup",
		"total":  len(arr),
	}).Debug("system_dedup: normalized PreToolUse messages [cpa-norm]")

	return result
}

// hashContent returns a short hex string for content comparison logging.
func hashContent(content string) string {
	if len(content) == 0 {
		return "0"
	}
	h := 0
	for i := 0; i < min(len(content), 256); i++ {
		h = h*31 + int(content[i])
	}
	return fmt.Sprintf("%x", h)
}

// isPTUSystemMessage reports whether a message is a system-role CC hook
// that should be stripped. Detection: role=system, content is string,
// contains "PreToolUse:" or "PostToolUseFailure:".
func isPTUSystemMessage(m gjson.Result) bool {
	if m.Get("role").String() != "system" {
		return false
	}
	content := m.Get("content")
	if content.Type != gjson.String {
		return false
	}
	s := content.String()
	return strings.Contains(s, "PreToolUse:") || strings.Contains(s, "PostToolUseFailure:")
}

// stripPostToolUseSuffix removes the PostToolUse (and PostToolUseFailure)
// suffix from a PTU content string. CC embeds PostToolUse counters in the
// same system message, e.g.:
//
//	PreToolUse:Read hook...\n\nPostToolUse:Read hook...(12 files)
//
// The PostToolUse suffix varies per message (different file counts) which
// defeats PTU deduplication. Stripping it produces a stable dedup key and
// removes model-useless counter noise from the output.
func stripPostToolUseSuffix(ptuText string) string {
	idx := strings.Index(ptuText, "\nPostToolUse")
	if idx < 0 {
		return ptuText
	}
	return strings.TrimRight(ptuText[:idx], "\n\r ")
}

// isPostToolUseUserMessage reports whether a user-role message contains
// PostToolUse or PostToolUseFailure hook text. Checks both string content
// and array content. "PostToolUseFailure:" does not contain "PostToolUse:"
// as a substring, so both patterns are tested independently.
func isPostToolUseUserMessage(m gjson.Result) bool {
	if m.Get("role").String() != "user" {
		return false
	}
	content := m.Get("content")
	if content.Type == gjson.String {
		return strings.Contains(content.String(), "PostToolUse:") ||
			strings.Contains(content.String(), "PostToolUseFailure:")
	}
	if content.IsArray() {
		for _, elem := range content.Array() {
			if elem.Get("type").String() == "text" {
				t := elem.Get("text").String()
				if strings.Contains(t, "PostToolUse:") ||
					strings.Contains(t, "PostToolUseFailure:") {
					return true
				}
			}
		}
	}
	return false
}

// stripPostToolUseElements removes PostToolUse or PostToolUseFailure
// elements from a user-role message's content. For string content that
// is entirely hook text, returns empty string and false (message should
// be deleted). For array content, returns the cleaned JSON with only
// non-hook elements.
func stripPostToolUseElements(m gjson.Result) (string, bool) {
	content := m.Get("content")
	if content.Type == gjson.String {
		return "", false
	}
	if !content.IsArray() {
		return m.Raw, true
	}
	var kept []string
	for _, elem := range content.Array() {
		if elem.Get("type").String() == "text" {
			t := elem.Get("text").String()
			if strings.Contains(t, "PostToolUse:") ||
				strings.Contains(t, "PostToolUseFailure:") {
				continue
			}
		}
		kept = append(kept, elem.Raw)
	}
	if len(kept) == 0 {
		return "", false
	}
	var buf bytes.Buffer
	buf.WriteString(`{"role":"user","content":[`)
	for i, e := range kept {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString(e)
	}
	buf.WriteString(`]}`)
	return buf.String(), true
}

// RelocateHookMessages strips PreToolUse hook messages from inline positions
// and appends them (deduplicated) to the end of the messages array.
// PostToolUse user messages are removed entirely since CC drops them between
// rounds. This keeps the conversation prefix hook-free so that DeepSeek's
// "end of user input" cache unit snaps cleanly at the last real user message,
// enabling cross-round KV cache hits on the full conversation history.
//
// PTU detection: role=system, string content containing "PreToolUse:".
// PostToolUse detection: role=user, content containing "PostToolUse:".
//
// Logs at debug level when messages are relocated [cpa-reloc].
func RelocateHookMessages(body []byte) []byte {
	msgs := gjson.GetBytes(body, "messages")
	if !msgs.IsArray() || len(msgs.Array()) < 2 {
		return body
	}

	arr := msgs.Array()
	removedPTU := 0
	removedPost := 0

	var buf bytes.Buffer
	buf.WriteByte('[')
	first := true

	for _, m := range arr {
		if isPTUSystemMessage(m) {
			removedPTU++
			continue
		}

		if isPostToolUseUserMessage(m) {
			cleaned, hasContent := stripPostToolUseElements(m)
			if hasContent {
				if !first {
					buf.WriteByte(',')
				}
				first = false
				buf.WriteString(cleaned)
			} else {
				removedPost++
			}
			continue
		}

		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.WriteString(m.Raw)
	}

	buf.WriteByte(']')

	if removedPTU == 0 && removedPost == 0 {
		return body
	}

	result, err := sjson.SetRawBytes(body, "messages", buf.Bytes())
	if err != nil {
		log.WithField("module", "system_dedup").Warnf("reloc: failed to set messages: %v", err)
		return body
	}

	log.WithFields(log.Fields{
		"module":       "system_dedup",
		"ptu_removed":  removedPTU,
		"post_removed": removedPost,
	}).Debug("system_dedup: stripped hook messages [cpa-reloc]")

	return result
}

// ReanchorHooks appends hook context (PreToolUse, PostToolUse,
// PostToolUseFailure) into the content of the preceding tool-role message
// instead of leaving them as standalone messages in the array. This
// preserves hook content for the model while reducing per-request message
// count variation, stabilising DeepSeek KV cache prefix units.
//
// Anchor rules:
//   - Hook messages immediately following a tool message are anchored to
//     that tool's content.
//   - Consecutive hooks all anchor to the same preceding tool.
//   - When a normal (non-hook) message sits between a tool and a hook, the
//     look-back up to 3 positions still finds the tool so the hook can
//     anchor. An intervening assistant message (model output) resets the
//     anchor chain because it represents a new reasoning step.
//   - PostToolUseFailure (role=system) may be farther from its tool; the
//     look-back handles this. If still no tool is found the message is
//     kept untouched.
//   - Hook text is extracted from string or array content, stripped of
//     <system-reminder> wrappers. For PreToolUse messages that embed a
//     PostToolUse suffix the suffix is trimmed (stripPostToolUseSuffix);
//     standalone PostToolUse messages are anchored in full — including
//     per-round counters — so the model sees accurate operation scope.
//   - When the hook text cannot be reliably extracted (e.g. array content
//     with no matching text element) the message is skipped and kept as-is
//     in the conversation.
//   - Tool content that is an array has the hook text appended as a new
//     {"type":"text","text":"..."} element rather than being stringified.
//
// Logs at debug level when hooks are anchored [cpa-reanchor].
func ReanchorHooks(body []byte) []byte {
	msgs := gjson.GetBytes(body, "messages")
	if !msgs.IsArray() || len(msgs.Array()) < 2 {
		return body
	}

	arr := msgs.Array()
	// Pass 1: identify which indices are hook messages and build
	// anchor assignments (hook index → target tool index).
	type anchor struct {
		toolIdx int
	}
	anchors := make(map[int]anchor)
	lastToolIdx := -1

	for i := range arr {
		role := arr[i].Get("role").String()

		if role == "tool" {
			lastToolIdx = i
			continue
		}

		if isHookForReanchor(arr[i]) {
			if lastToolIdx >= 0 {
				anchors[i] = anchor{toolIdx: lastToolIdx}
			} else {
				// Look back for a tool message up to 3 positions.
				// An intervening assistant message (model output) breaks
				// the search — hooks belong to the current reasoning step.
				for j := i - 1; j >= 0 && j >= i-3; j-- {
					rj := arr[j].Get("role").String()
					if rj == "assistant" {
						break
					}
					if rj == "tool" {
						lastToolIdx = j
						break
					}
				}
				if lastToolIdx >= 0 {
					anchors[i] = anchor{toolIdx: lastToolIdx}
				}
			}
			continue
		}

		// Only assistant messages (model output) reset the anchor chain.
		// User/system messages that are not hooks are transparent — a hook
		// arriving after them can still anchor to the preceding tool via
		// the look-back window.
		if role == "assistant" {
			lastToolIdx = -1
		}
	}

	if len(anchors) == 0 {
		return body
	}

	// Pass 2: extract hook text and group by anchor tool.
	toolHooks := make(map[int][]string)
	skipped := 0
	for hookIdx, a := range anchors {
		ht, ok := extractHookForReanchor(arr[hookIdx])
		if !ok {
			delete(anchors, hookIdx)
			skipped++
			continue
		}
		toolHooks[a.toolIdx] = append(toolHooks[a.toolIdx], ht)
	}

	if len(anchors) == 0 {
		return body
	}

	// Pass 3: rebuild messages array.
	var buf bytes.Buffer
	buf.WriteByte('[')
	first := true

	for i := range arr {
		if _, isAnchored := anchors[i]; isAnchored {
			continue
		}

		hts, hasHooks := toolHooks[i]
		if !hasHooks {
			if !first {
				buf.WriteByte(',')
			}
			first = false
			buf.WriteString(arr[i].Raw)
			continue
		}

		// This tool message has hooks to anchor — build updated content.
		updated, err := buildAnchoredToolMessage([]byte(arr[i].Raw), hts)
		if err != nil {
			log.WithField("module", "system_dedup").Warnf("reanchor: failed to build anchored tool at %d: %v", i, err)
			updated = []byte(arr[i].Raw)
		}
		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.Write(updated)
	}
	buf.WriteByte(']')

	result, err := sjson.SetRawBytes(body, "messages", buf.Bytes())
	if err != nil {
		log.WithField("module", "system_dedup").Warnf("reanchor: failed to set messages: %v", err)
		return body
	}

	fields := log.Fields{
		"module":   "system_dedup",
		"anchored": len(anchors),
	}
	if skipped > 0 {
		fields["skipped"] = skipped
	}
	log.WithFields(fields).Debug("system_dedup: anchored hook messages into tool content [cpa-reanchor]")

	return result
}

// buildAnchoredToolMessage appends hook texts into the content of a tool
// message. If the original content is a string the hook texts are appended
// with a "\n\n---\n" separator. If it is an array a new text element is
// appended to the array.
func buildAnchoredToolMessage(toolRaw []byte, hooks []string) ([]byte, error) {
	content := gjson.GetBytes(toolRaw, "content")

	var merged string
	for i, h := range hooks {
		if i > 0 {
			merged += "\n"
		}
		merged += h
	}

	if content.IsArray() {
		// Append as a new {"type":"text","text":"..."} element.
		elem := json.RawMessage(`{"type":"text","text":""}`)
		quoted, _ := json.Marshal(merged)
		elem, _ = sjson.SetRawBytes(elem, "text", quoted)
		return sjson.SetRawBytes(toolRaw, "content.-1", elem)
	}

	// String content — append with separator.
	orig := content.String()
	orig += "\n\n---\n" + merged
	return sjson.SetBytes(toolRaw, "content", orig)
}

// isHookForReanchor reports whether a message is a hook that should be
// anchored into the preceding tool's content. Covers:
//   - system-role PreToolUse / PostToolUseFailure
//   - user-role PostToolUse / PostToolUseFailure
func isHookForReanchor(m gjson.Result) bool {
	if isPTUSystemMessage(m) {
		return true
	}
	if isPostToolUseUserMessage(m) {
		return true
	}
	return false
}

// extractHookForReanchor extracts the cleaned hook text from a hook
// message. Returns (text, true) on success; (_, false) when the hook
// content cannot be reliably extracted and the message should be left
// untouched.
//
// For PreToolUse messages (role=system, content starts with "PreToolUse:")
// the embedded PostToolUse suffix is trimmed via stripPostToolUseSuffix.
// For standalone PostToolUse/PostToolUseFailure messages the full text is
// kept, including per-round counters, so the model sees accurate operation
// scope.
func extractHookForReanchor(m gjson.Result) (text string, ok bool) {
	content := m.Get("content")
	var raw string

	if content.Type == gjson.String {
		raw = content.String()
	} else if content.IsArray() {
		for _, elem := range content.Array() {
			if elem.Get("type").String() == "text" {
				t := elem.Get("text").String()
				if strings.Contains(t, "PreToolUse:") ||
					strings.Contains(t, "PostToolUse:") ||
					strings.Contains(t, "PostToolUseFailure:") {
					raw = t
					break
				}
			}
		}
		if raw == "" {
			return "", false // array with no recognisable hook text element
		}
	} else {
		return "", false // unexpected content type
	}

	raw = strings.TrimSpace(raw)

	// Strip <system-reminder> wrapper if present.
	if strings.HasPrefix(raw, "<system-reminder>") {
		idx := strings.Index(raw, ">")
		if idx >= 0 {
			raw = raw[idx+1:]
		}
		idx = strings.LastIndex(raw, "<")
		if idx >= 0 {
			raw = raw[:idx]
		}
		raw = strings.TrimSpace(raw)
	}

	// For PTU messages that embed a PostToolUse suffix: strip the suffix
	// so only the PreToolUse instruction anchors. The standalone PostToolUse
	// messages carry the counter info.
	if strings.HasPrefix(raw, "PreToolUse:") {
		raw = stripPostToolUseSuffix(raw)
	}

	return raw, true
}

// ReorderSystemMessagesToFront moves all system-role messages to the
// beginning of the messages array, leaving user/assistant/tool messages at
// the end.  This ensures DeepSeek's "end of user input" cache prefix unit
// snaps at the end of the conversation (the last non-system message)
// instead of at a trailing block of system hooks or tool definitions.
//
// System messages are kept in their original relative order; conversation
// messages are kept in their original relative order.
//
// Logs at debug level when reordering occurs [cpa-reorder].
func ReorderSystemMessagesToFront(body []byte) []byte {
	msgs := gjson.GetBytes(body, "messages")
	if !msgs.IsArray() || len(msgs.Array()) < 2 {
		return body
	}

	arr := msgs.Array()
	sysCount := 0
	var sysRaws []string
	var convRaws []string

	for _, m := range arr {
		if m.Get("role").String() == "system" {
			sysRaws = append(sysRaws, m.Raw)
			sysCount++
		} else {
			convRaws = append(convRaws, m.Raw)
		}
	}

	if sysCount == 0 {
		return body
	}

	var buf bytes.Buffer
	buf.WriteByte('[')
	first := true
	for _, s := range sysRaws {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.WriteString(s)
	}
	for _, c := range convRaws {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.WriteString(c)
	}
	buf.WriteByte(']')

	result, err := sjson.SetRawBytes(body, "messages", buf.Bytes())
	if err != nil {
		log.WithField("module", "system_dedup").Warnf("reorder: failed to set messages: %v", err)
		return body
	}

	log.WithFields(log.Fields{
		"module":      "system_dedup",
		"system_msgs": sysCount,
		"conv_msgs":   len(convRaws),
	}).Debug("system_dedup: reordered system messages to front [cpa-reorder]")

	return result
}
