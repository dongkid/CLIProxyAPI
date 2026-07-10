package helps

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ccAutoModeDetectPhrase is the text fragment used to detect auto mode
// classifier requests. It appears in the system prompt of Claude Code's
// auto mode safety monitor.
const ccAutoModeDetectPhrase = "You are a security monitor for autonomous AI coding agents"

// defaultCCAutoModeMaxTokens is the default max_tokens injected when the
// user enables cc-auto-mode but does not specify a custom value.
const defaultCCAutoModeMaxTokens = 8192

// IsCCAutoModeEnabled checks whether the given model has cc-auto-mode
// enabled in the payload configuration rules. Follows the same pattern
// as IsCCGoalHookEnabled.
func IsCCAutoModeEnabled(cfg *config.Config, model string) bool {
	if cfg == nil || model == "" {
		log.Warn("[cc-auto-mode] IsCCAutoModeEnabled: cfg is nil or model is empty")
		return false
	}
	candidates := payloadModelCandidates(model, "")
	rules := collectCCAutoModeRules(cfg)
	log.WithFields(log.Fields{
		"model":       model,
		"candidates":  candidates,
		"rules_found": len(rules),
	}).Info("[cc-auto-mode] IsCCAutoModeEnabled check")
	for _, candidate := range candidates {
		for _, rule := range rules {
			name := strings.TrimSpace(rule.Name)
			if name == "" {
				continue
			}
			if !matchModelPattern(name, candidate) {
				continue
			}
			log.WithFields(log.Fields{
				"model": model,
				"rule":  name,
			}).Info("[cc-auto-mode] model matched, injection enabled")
			return true
		}
	}
	log.WithFields(log.Fields{
		"model": model,
	}).Info("[cc-auto-mode] no matching rule found")
	return false
}

// GetCCAutoModeMaxTokens returns the configured max_tokens value for the
// first matching cc-auto-mode rule, or the default (8192) if not set.
func GetCCAutoModeMaxTokens(cfg *config.Config, model string) int {
	if cfg == nil || model == "" {
		return defaultCCAutoModeMaxTokens
	}
	candidates := payloadModelCandidates(model, "")
	rules := collectCCAutoModeRules(cfg)
	for _, candidate := range candidates {
		for _, rule := range rules {
			name := strings.TrimSpace(rule.Name)
			if name == "" {
				continue
			}
			if !matchModelPattern(name, candidate) {
				continue
			}
			if rule.CCAutoModeMaxTokens != nil && *rule.CCAutoModeMaxTokens > 0 {
				return *rule.CCAutoModeMaxTokens
			}
			return defaultCCAutoModeMaxTokens
		}
	}
	return defaultCCAutoModeMaxTokens
}

// collectCCAutoModeRules collects all PayloadModelRule entries where
// CCAutoMode is true. Follows the same pattern as collectCCGoalHookRules.
func collectCCAutoModeRules(cfg *config.Config) []config.PayloadModelRule {
	var rules []config.PayloadModelRule
	collect := func(ruleList []config.PayloadRule) {
		for _, rule := range ruleList {
			for _, mr := range rule.Models {
				if mr.CCAutoMode != nil && *mr.CCAutoMode {
					rules = append(rules, mr)
				}
			}
		}
	}
	collect(cfg.Payload.Default)
	collect(cfg.Payload.DefaultRaw)
	collect(cfg.Payload.Override)
	collect(cfg.Payload.OverrideRaw)
	return rules
}

// scanBodyForAutoModeDetectPhrase checks if any system-level content in the body
// contains the auto mode classifier detection phrase. It checks:
//   - top-level "system" array (Claude Messages API format)
//   - messages[0] with role="system" (OpenAI format)
//   - messages.#.content blocks (various translated formats where role may be
//     preserved differently across translator versions)
//
// The detection phrase (52 chars) is unique to the Claude Code auto mode
// classifier system prompt, so scanning across all message content blocks
// does not introduce false positive risk.
func scanBodyForAutoModeDetectPhrase(body []byte) bool {
	// 1. Check top-level "system" array (Claude Messages API format).
	for _, part := range gjson.GetBytes(body, "system").Array() {
		if part.Get("type").String() == "text" {
			if strings.Contains(part.Get("text").String(), ccAutoModeDetectPhrase) {
				return true
			}
		}
	}

	// 2. Check messages[0] with role="system" (standard OpenAI format).
	firstMsg := gjson.GetBytes(body, "messages.0")
	if firstMsg.Get("role").String() == "system" {
		content := firstMsg.Get("content")
		if content.IsArray() {
			for _, block := range content.Array() {
				if block.Get("type").String() == "text" && strings.Contains(block.Get("text").String(), ccAutoModeDetectPhrase) {
					return true
				}
			}
		} else if content.Type == gjson.String {
			return strings.Contains(content.String(), ccAutoModeDetectPhrase)
		}
	}

	// 3. Fallback: scan messages without explicit user/assistant role.
	//    Some translator versions may not set role="system", producing
	//    messages with no role field for system content. We skip messages
	//    with explicit user/assistant roles to avoid false positives.
	for _, msg := range gjson.GetBytes(body, "messages").Array() {
		role := msg.Get("role").String()
		if role == "user" || role == "assistant" {
			continue
		}
		content := msg.Get("content")
		if content.IsArray() {
			for _, block := range content.Array() {
				if block.Get("type").String() == "text" && strings.Contains(block.Get("text").String(), ccAutoModeDetectPhrase) {
					return true
				}
			}
		} else if content.Type == gjson.String {
			if strings.Contains(content.String(), ccAutoModeDetectPhrase) {
				return true
			}
		}
	}

	return false
}

// isAutoModeClassifier checks whether the body corresponds to a Claude Code
// auto mode classifier request by looking for the unique system prompt phrase
// in system-level content only.
func isAutoModeClassifier(body []byte) bool {
	if !gjson.ValidBytes(body) {
		return false
	}
	return scanBodyForAutoModeDetectPhrase(body)
}

// ccAutoModeCapEffort caps reasoning effort for classifier requests when
// the user has configured a higher effort (e.g. "max") via ApplyPayloadConfig.
// This runs AFTER ApplyPayloadConfig, so the effort field already exists
// in the body and can be capped to prevent the classifier from consuming
// excessive tokens on deep reasoning.
const ccAutoModeCapEffort = "high"

// capEffortField caps a reasoning effort field to high if it is set to
// an overly aggressive value (max, xhigh, ultra). Expected to run AFTER
// ApplyPayloadConfig has already set the effort from user config.
func capEffortField(body []byte, path string, modified *bool) []byte {
	effort := gjson.GetBytes(body, path)
	if !effort.Exists() {
		return body
	}

	val := strings.ToLower(strings.TrimSpace(effort.String()))
	if val == "" || val == "none" || val == "low" || val == "medium" || val == "high" {
		return body
	}

	body, err := sjson.SetBytes(body, path, ccAutoModeCapEffort)
	if err != nil {
		log.WithError(err).Warn("[cc-auto-mode] failed to cap " + path)
		return body
	}
	log.WithFields(log.Fields{
		"path": path,
		"from": val,
		"to":   ccAutoModeCapEffort,
	}).Info("[cc-auto-mode] reasoning effort capped for classifier")
	*modified = true
	return body
}

// InjectAutoModeOverrides detects auto mode classifier evaluation requests
// and injects overrides to ensure reliable verdict generation:
//
//  1. Increases max_tokens from the Claude Code default (2112) to the
//     configured value (default 8192) so the model has enough budget for
//     complete reasoning + verdict output.
//  2. Caps reasoning_effort at "high" to prevent user-configured "max"
//     effort (set via ApplyPayloadConfig) from consuming tokens
//     unnecessarily on classifier-only analysis. This runs AFTER
//     ApplyPayloadConfig so the effort field already exists in the body.
//
// maxTokens is the target max_tokens value (from config or default 8192).
// Returns the (possibly modified) body and whether injection occurred.
func InjectAutoModeOverrides(body []byte, maxTokens int) ([]byte, bool) {
	if !gjson.ValidBytes(body) {
		return body, false
	}

	if !isAutoModeClassifier(body) {
		return body, false
	}

	log.WithFields(log.Fields{
		"target_max_tokens": maxTokens,
	}).Info("[cc-auto-mode] auto mode classifier detected")

	var modified bool

	// 1. Override max_tokens if insufficient.
	currentMT := gjson.GetBytes(body, "max_tokens")
	curMT := 0
	if currentMT.Exists() {
		curMT = int(currentMT.Int())
	}
	if curMT < maxTokens {
		var err error
		body, err = sjson.SetBytes(body, "max_tokens", maxTokens)
		if err != nil {
			log.WithError(err).Warn("[cc-auto-mode] failed to set max_tokens")
			return body, modified
		}
		log.WithFields(log.Fields{
			"from": curMT,
			"to":   maxTokens,
		}).Info("[cc-auto-mode] max_tokens overridden")
		modified = true
	} else {
		log.WithFields(log.Fields{
			"current": curMT,
			"target":  maxTokens,
		}).Debug("[cc-auto-mode] max_tokens already sufficient, skipping")
	}

	// 2. Cap reasoning_effort if set to max/xhigh/ultra (OpenAI format).
	//    This field was likely set by ApplyPayloadConfig which runs before us.
	body = capEffortField(body, "reasoning_effort", &modified)

	// 3. Cap reasoning.effort if set to max/xhigh/ultra (Codex format).
	body = capEffortField(body, "reasoning.effort", &modified)

	return body, modified
}
