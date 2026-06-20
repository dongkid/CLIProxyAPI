package helps

import (
	"encoding/json"
	"sort"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

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
