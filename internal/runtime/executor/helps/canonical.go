package helps

import (
	"bytes"
	"encoding/json"
	"sort"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// reorderKeys lists the top-level JSON keys written first, in order, by
// ReorderJSONForCache. Stable fields (model, tools) come before variable
// fields (messages), so that message-array growth never shifts the byte
// offset of preceding content. Any top-level keys not in this list are
// appended in alphabetical order after messages.
var reorderKeys = []string{
	"model",
	"reasoning_effort",
	"stream",
	"stream_options",
	"tools",
	"messages",
}

// CanonicalizeJSON re-serializes a JSON body to produce a byte-stable
// deterministic representation. Identical semantic content always produces
// byte-for-byte identical output regardless of key ordering, whitespace, or
// number formatting in the input.
//
// Go's encoding/json sorts map keys alphabetically when marshaling and uses
// consistent formatting, so consecutive requests that differ only in sjson
// serialization artifacts will converge to the same byte stream. This
// maximizes upstream KV prefix cache hit rates.
//
// On parse failure the original body is returned unchanged (safe fallback).
func CanonicalizeJSON(body []byte) []byte {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		log.WithField("module", "canonical").Warnf("canonicalize: failed to unmarshal body: %v", err)
		return body
	}
	out, err := json.Marshal(v)
	if err != nil {
		log.WithField("module", "canonical").Warnf("canonicalize: failed to marshal body: %v", err)
		return body
	}
	return out
}

// SortToolsByName sorts the top-level "tools" array alphabetically by
// function.name so that CC round-boundary tool reordering does not break
// the DeepSeek KV cache prefix.  Returns body unchanged if there is no
// tools array or on parse failure.
//
// Logs at debug level when tools are sorted [cpa-toolsort].
func SortToolsByName(body []byte) []byte {
	tools := gjson.GetBytes(body, "tools")
	if !tools.IsArray() || len(tools.Array()) < 2 {
		return body
	}

	arr := tools.Array()
	indices := make([]int, len(arr))
	for i := range indices {
		indices[i] = i
	}

	sort.SliceStable(indices, func(i, j int) bool {
		ni := arr[indices[i]].Get("function.name").String()
		nj := arr[indices[j]].Get("function.name").String()
		return ni < nj
	})

	changed := false
	for i, origIdx := range indices {
		if i != origIdx {
			changed = true
			break
		}
	}
	if !changed {
		return body
	}

	var sorted []json.RawMessage
	for _, idx := range indices {
		sorted = append(sorted, json.RawMessage(arr[idx].Raw))
	}

	sortedBytes, err := json.Marshal(sorted)
	if err != nil {
		log.WithField("module", "canonical").Warnf("toolsort: failed to marshal sorted tools: %v", err)
		return body
	}

	result, err := sjson.SetRawBytes(body, "tools", sortedBytes)
	if err != nil {
		log.WithField("module", "canonical").Warnf("toolsort: failed to set sorted tools: %v", err)
		return body
	}

	log.WithField("module", "canonical").Debug("canonical: sorted tools by name [cpa-toolsort]")
	return result
}

// ReorderJSONForCache serializes the JSON body with top-level keys in a
// cache-friendly order: model, reasoning_effort, stream, stream_options,
// tools, then messages last. Any keys not in that list are appended
// alphabetically after messages.
//
// Rationale: DeepSeek's KV cache is a byte-level prefix match. When the
// "messages" array grows between turns, every key that follows "messages"
// in the JSON shifts to a different byte offset — breaking the prefix.
// Placing "messages" last isolates growth to the tail of the body so the
// entire stable prefix (model + tools, ~105 KB in practice) stays cached.
//
// On parse failure the original body is returned unchanged (safe fallback).
// Call this as the final step in the CPA pipeline, replacing CanonicalizeJSON.
//
// Logs at debug level when reordering occurs [cpa-reorder-json].
func ReorderJSONForCache(body []byte) []byte {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		log.WithField("module", "canonical").Warnf("reorder-json: failed to unmarshal body: %v", err)
		return body
	}
	if len(root) == 0 {
		return body
	}

	// Set of known keys for O(1) lookup in Phase 2.
	isKnown := make(map[string]bool, len(reorderKeys))
	for _, k := range reorderKeys {
		isKnown[k] = true
	}

	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true

	// Phase 1: write known keys in fixed order.
	for _, k := range reorderKeys {
		v, ok := root[k]
		if !ok {
			continue
		}
		if !first {
			buf.WriteByte(',')
		}
		first = false
		writeKeyValue(&buf, k, v)
	}

	// Phase 2: collect unknown keys and write them alphabetically.
	var extra []string
	for k := range root {
		if isKnown[k] {
			continue
		}
		extra = append(extra, k)
	}
	sort.Strings(extra)
	for _, k := range extra {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		writeKeyValue(&buf, k, root[k])
	}

	buf.WriteByte('}')

	log.WithField("module", "canonical").Debug("canonical: reordered JSON keys for cache [cpa-reorder-json]")
	return buf.Bytes()
}

// writeKeyValue writes `"key":value` into buf, where value is a raw JSON
// fragment that is re-serialised via json.Marshal for deterministic output.
func writeKeyValue(buf *bytes.Buffer, key string, raw json.RawMessage) {
	k, _ := json.Marshal(key)
	buf.Write(k)
	buf.WriteByte(':')

	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		buf.Write(raw)
		return
	}
	marshaled, err := json.Marshal(v)
	if err != nil {
		buf.Write(raw)
		return
	}
	buf.Write(marshaled)
}
