package auth

// CPA PATCH: auto mode classifier routing-layer redirect + override.
// This file is entirely a CPA addition — zero upstream code.
// It provides routing-level redirect of Claude Code auto mode classifier
// requests to a different model, ensuring the target model determines
// the full provider/auth/upstream selection path.
// Delete this file if upstream adopts its own auto mode routing logic.

import (
	"strings"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/tidwall/gjson"
)

// ccAutoModeDetectPhrase is the unique phrase in Claude Code auto mode
// classifier system prompts used to identify classifier requests.
const ccAutoModeDetectPhrase = "You are a security monitor for autonomous AI coding agents"

// autoModeModelMatch checks whether a model name matches a glob pattern
// supporting '*' as a wildcard. Implements the same iterative algorithm
// as matchModelPattern in internal/runtime/executor/helps/payload_helpers.go.
func autoModeModelMatch(pattern, model string) bool {
	pattern = strings.TrimSpace(pattern)
	model = strings.TrimSpace(model)
	if pattern == "" || model == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	pi, si := 0, 0
	starIdx := -1
	matchIdx := 0
	for si < len(model) {
		if pi < len(pattern) && pattern[pi] == model[si] {
			pi++
			si++
			continue
		}
		if pi < len(pattern) && pattern[pi] == '*' {
			starIdx = pi
			matchIdx = si
			pi++
			continue
		}
		if starIdx != -1 {
			pi = starIdx + 1
			matchIdx++
			si = matchIdx
			continue
		}
		return false
	}
	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi == len(pattern)
}

// findMatchingRule scans all four rule lists for the first rule matching
// the given baseModel. Returns nil if no match found.
func findMatchingRule(cfg *internalconfig.Config, baseModel string, pred func(mr internalconfig.PayloadModelRule) bool) *internalconfig.PayloadModelRule {
	if cfg == nil || baseModel == "" {
		return nil
	}
	scan := func(rules []internalconfig.PayloadRule) *internalconfig.PayloadModelRule {
		for _, rule := range rules {
			for _, mr := range rule.Models {
				if !pred(mr) {
					continue
				}
				if autoModeModelMatch(mr.Name, baseModel) {
					return &mr
				}
			}
		}
		return nil
	}
	for _, list := range [][]internalconfig.PayloadRule{
		cfg.Payload.Default,
		cfg.Payload.DefaultRaw,
		cfg.Payload.Override,
		cfg.Payload.OverrideRaw,
	} {
		if mr := scan(list); mr != nil {
			return mr
		}
	}
	return nil
}

// CheckAutoModeRedirect detects whether the request is a Claude Code auto
// mode classifier. If so, and if a redirect is configured for the source
// model, it returns the target model name. The caller should use this
// model name for routing and update it in the request body.
//
// This is called at the API handler level, BEFORE provider/auth/executor
// selection, so the new model determines the entire routing path.
func (m *Manager) CheckAutoModeRedirect(rawJSON []byte, modelName string) (redirectModel string, ok bool) {
	if m == nil || len(rawJSON) == 0 || modelName == "" {
		return "", false
	}

	// 1. Detect auto mode classifier by the system prompt phrase.
	system := gjson.GetBytes(rawJSON, "system")
	if !system.IsArray() {
		return "", false
	}
	found := false
	system.ForEach(func(_, part gjson.Result) bool {
		if part.Get("type").String() == "text" && strings.Contains(part.Get("text").String(), ccAutoModeDetectPhrase) {
			found = true
			return false
		}
		return true
	})
	if !found {
		return "", false
	}

	// 2. Look up redirect in config.
	cfg, _ := m.runtimeConfig.Load().(*internalconfig.Config)
	if cfg == nil {
		return "", false
	}
	baseModel := modelName
	if idx := strings.IndexByte(modelName, '('); idx >= 0 {
		baseModel = modelName[:idx]
	}
	mr := findMatchingRule(cfg, baseModel, func(mr internalconfig.PayloadModelRule) bool {
		return mr.CCAutoMode != nil && *mr.CCAutoMode &&
			mr.CCAutoModeRedirect != nil && *mr.CCAutoModeRedirect != ""
	})
	if mr == nil {
		return "", false
	}
	return *mr.CCAutoModeRedirect, true
}

// GetAutoModeOverrides returns max_tokens and reasoning_effort values
// configured for auto mode classifier requests for the given model.
// Returns 0 and "" when not configured (caller should not override).
func (m *Manager) GetAutoModeOverrides(modelName string) (maxTokens int, reasoningEffort string) {
	cfg, _ := m.runtimeConfig.Load().(*internalconfig.Config)
	if cfg == nil || modelName == "" {
		return 0, ""
	}
	baseModel := modelName
	if idx := strings.IndexByte(modelName, '('); idx >= 0 {
		baseModel = modelName[:idx]
	}
	mr := findMatchingRule(cfg, baseModel, func(mr internalconfig.PayloadModelRule) bool {
		return mr.CCAutoMode != nil && *mr.CCAutoMode
	})
	if mr == nil {
		return 0, ""
	}
	if mr.CCAutoModeMaxTokens != nil && *mr.CCAutoModeMaxTokens > 0 {
		maxTokens = *mr.CCAutoModeMaxTokens
	}
	if mr.CCAutoModeReasoningEffort != nil && *mr.CCAutoModeReasoningEffort != "" {
		reasoningEffort = *mr.CCAutoModeReasoningEffort
	}
	return
}
