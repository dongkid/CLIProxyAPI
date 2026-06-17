package helps

import (
	"bytes"
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
