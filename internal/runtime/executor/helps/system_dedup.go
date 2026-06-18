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

// isPTUSystemMessage reports whether a message is a system-role PTU hook.
// Detection: role=system, content is string, contains "PreToolUse:".
func isPTUSystemMessage(m gjson.Result) bool {
	if m.Get("role").String() != "system" {
		return false
	}
	content := m.Get("content")
	return content.Type == gjson.String && strings.Contains(content.String(), "PreToolUse:")
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
	var ptuTexts []string
	seenPTU := make(map[string]bool)
	removedPTU := 0
	removedPost := 0

	var buf bytes.Buffer
	buf.WriteByte('[')
	first := true

	for _, m := range arr {
		if isPTUSystemMessage(m) {
			ptu := m.Get("content").String()
			if !seenPTU[ptu] {
				seenPTU[ptu] = true
				ptuTexts = append(ptuTexts, ptu)
			}
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

	// Append deduplicated PTU messages at the end
	for _, pt := range ptuTexts {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.WriteString(`{"role":"system","content":`)
		quoted, _ := json.Marshal(pt)
		buf.Write(quoted)
		buf.WriteByte('}')
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
		"ptu_appended": len(ptuTexts),
		"post_removed": removedPost,
	}).Debug("system_dedup: relocated hook messages to end [cpa-reloc]")

	return result
}
