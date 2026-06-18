package helps

import (
	"encoding/json"

	log "github.com/sirupsen/logrus"
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
