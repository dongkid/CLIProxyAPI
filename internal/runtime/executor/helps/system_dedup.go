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
//     that tool's content with a "[hook:...]" label prefix.
//   - Consecutive hook messages all anchor to the same preceding tool.
//   - PostToolUseFailure (role=system) may appear farther from its tool;
//     a look-back of up to 3 messages finds the nearest tool to anchor to.
//     If no tool is found within that window the message is kept as-is.
//   - User-role PostToolUse messages have their content extracted from
//     string or array format, stripped of <system-reminder> wrappers,
//     and the PostToolUse suffix noise removed via stripPostToolUseSuffix.
//
// Hooks already anchored by this function are not re-detected by
// isPTUSystemMessage / isPostToolUseUserMessage in downstream steps
// because they become part of the tool message's content string.
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
				for j := i - 1; j >= 0 && j >= i-3; j-- {
					if arr[j].Get("role").String() == "tool" {
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

		// Non-tool, non-hook message resets the anchor chain.
		// PTU/PostToolUse only anchor to the immediately preceding tool;
		// an intervening assistant/user breaks the chain.
		if !isHookForReanchor(arr[i]) {
			lastToolIdx = -1
		}
	}

	if len(anchors) == 0 {
		return body
	}

	// Pass 2: build anchored tool content strings.
	// Group hook indices by their anchor tool index.
	toolHooks := make(map[int][]int)
	for hookIdx, a := range anchors {
		toolHooks[a.toolIdx] = append(toolHooks[a.toolIdx], hookIdx)
	}

	// Build updated tool content for each anchored tool.
	toolReplacements := make(map[int]string)
	for toolIdx, hookIdxs := range toolHooks {
		origContent := arr[toolIdx].Get("content").String()
		var sb strings.Builder
		sb.WriteString(origContent)
		for _, hi := range hookIdxs {
			hookLabel, hookText := extractHookForReanchor(arr[hi])
			sb.WriteString("\n\n[hook:")
			sb.WriteString(hookLabel)
			sb.WriteString("] ")
			sb.WriteString(hookText)
		}
		toolReplacements[toolIdx] = sb.String()
	}

	// Pass 3: rebuild messages array, applying tool content replacements
	// and skipping anchored hook messages.
	var buf bytes.Buffer
	buf.WriteByte('[')
	first := true

	for i := range arr {
		if _, isAnchored := anchors[i]; isAnchored {
			continue
		}

		if repl, ok := toolReplacements[i]; ok {
			// Replace this tool message's content string.
			updated, err := sjson.SetBytes([]byte(arr[i].Raw), "content", repl)
			if err != nil {
				log.WithField("module", "system_dedup").Warnf("reanchor: failed to set tool content at %d: %v", i, err)
				updated = []byte(arr[i].Raw)
			}
			if !first {
				buf.WriteByte(',')
			}
			first = false
			buf.Write(updated)
			continue
		}

		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.WriteString(arr[i].Raw)
	}
	buf.WriteByte(']')

	result, err := sjson.SetRawBytes(body, "messages", buf.Bytes())
	if err != nil {
		log.WithField("module", "system_dedup").Warnf("reanchor: failed to set messages: %v", err)
		return body
	}

	log.WithFields(log.Fields{
		"module":   "system_dedup",
		"anchored": len(anchors),
	}).Debug("system_dedup: anchored hook messages into tool content [cpa-reanchor]")

	return result
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

// extractHookForReanchor extracts a human-readable label and the cleaned
// hook text from a hook message. The label is the hook type prefix
// (e.g. "PreToolUse:Read", "PostToolUseFailure:mcp__..."). The text is
// stripped of <system-reminder> wrappers and PostToolUse suffix noise.
func extractHookForReanchor(m gjson.Result) (label, text string) {
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
			raw = content.Raw
		}
	} else {
		raw = content.Raw
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

	// Extract label from the hook prefix.
	label = extractHookLabel(raw)

	// Remove PostToolUse suffix noise for stable dedup anchors.
	cleaned := stripPostToolUseSuffix(raw)
	return label, cleaned
}

// extractHookLabel extracts the hook type identifier from the hook text
// prefix, e.g. "PreToolUse:Bash" or "PostToolUseFailure:mcp__chrome".
func extractHookLabel(text string) string {
	text = strings.TrimSpace(text)
	for _, prefix := range []string{"PreToolUse:", "PostToolUseFailure:", "PostToolUse:"} {
		if strings.HasPrefix(text, prefix) {
			rest := text[len(prefix):]
			// Take up to the first space or colon for the tool name.
			end := strings.IndexAny(rest, " \t\n:(")
			if end < 0 {
				end = len(rest)
			}
			toolName := rest[:end]
			if toolName == "" {
				return prefix[:len(prefix)-1] // "PreToolUse", "PostToolUse", etc.
			}
			return prefix + toolName
		}
	}
	// Fallback: first 40 chars.
	if len(text) > 40 {
		return text[:40]
	}
	return text
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
