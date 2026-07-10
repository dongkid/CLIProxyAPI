package helps

import (
	"sort"
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

// GetCCAutoModeRedirect returns the redirect model name for the first matching
// cc-auto-mode rule, or "" if not configured. When non-empty, the classifier
// request's "model" field should be overridden to this value.
func GetCCAutoModeRedirect(cfg *config.Config, model string) string {
	if cfg == nil || model == "" {
		return ""
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
			if rule.CCAutoModeRedirect != nil && *rule.CCAutoModeRedirect != "" {
				return *rule.CCAutoModeRedirect
			}
			return ""
		}
	}
	return ""
}

// GetCCAutoModeMaxTokens returns the configured max_tokens value for the
// first matching cc-auto-mode rule, or 0 if not configured.
// Caller should only override max_tokens when the return value is > 0.
func GetCCAutoModeMaxTokens(cfg *config.Config, model string) int {
	if cfg == nil || model == "" {
		return 0
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
			return 0
		}
	}
	return 0
}

// GetCCAutoModeReasoningEffort returns the configured reasoning effort for
// the first matching cc-auto-mode rule, or "" if not configured.
// When non-empty, both "reasoning_effort" and "reasoning.effort" should
// be overridden to this value.
func GetCCAutoModeReasoningEffort(cfg *config.Config, model string) string {
	if cfg == nil || model == "" {
		return ""
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
			if rule.CCAutoModeReasoningEffort != nil && *rule.CCAutoModeReasoningEffort != "" {
				return *rule.CCAutoModeReasoningEffort
			}
			return ""
		}
	}
	return ""
}

// collectCCAutoModeRules collects all PayloadModelRule entries where
// CCAutoMode is true, sorted by Name length descending so that more
// specific (longer) patterns match before wildcard patterns.
// Follows the same collection pattern as collectCCGoalHookRules.
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

	sort.SliceStable(rules, func(i, j int) bool {
		return len(rules[i].Name) > len(rules[j].Name)
	})
	return rules
}

// scanBodyForAutoModeDetectPhrase checks if any system-level content in the body
// contains the auto mode classifier detection phrase. It checks:
//   - top-level "system" array (Claude Messages API format)
//   - messages[0] with role="system" (OpenAI format)
//   - messages content blocks without explicit user/assistant role (translated format)
func scanBodyForAutoModeDetectPhrase(body []byte) bool {
	// 1. Check top-level "system" array (Claude Messages API format).
	for _, part := range gjson.GetBytes(body, "system").Array() {
		if part.Get("type").String() == "text" {
			if strings.Contains(part.Get("text").String(), ccAutoModeDetectPhrase) {
				return true
			}
		}
	}

	// 2. Check messages[0] with role="system" (OpenAI format).
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

	// 3. Fallback: messages without explicit user/assistant role.
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

// InjectAutoModeOverrides detects auto mode classifier evaluation requests
// and applies the configured overrides:
//
//  1. maxTokens > 0: overrides max_tokens in the request body.
//     When 0, leaves max_tokens unchanged.
//  2. reasoningEffort != "": overrides both "reasoning_effort" and
//     "reasoning.effort" to this value. When "", leaves effort unchanged.
//
// Returns the (possibly modified) body and whether any modification occurred.
func InjectAutoModeOverrides(body []byte, maxTokens int, reasoningEffort string) ([]byte, bool) {
	if !isAutoModeClassifier(body) {
		return body, false
	}

	log.WithFields(log.Fields{
		"max_tokens":       maxTokens,
		"reasoning_effort": reasoningEffort,
	}).Info("[cc-auto-mode] auto mode classifier detected, applying overrides")

	var modified bool

	// 1. Override max_tokens if a positive value is configured.
	if maxTokens > 0 {
		currentMT := gjson.GetBytes(body, "max_tokens")
		curMT := 0
		if currentMT.Exists() {
			curMT = int(currentMT.Int())
		}
		if curMT != maxTokens {
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
			}).Debug("[cc-auto-mode] max_tokens already matches target, skipping")
		}
	} else {
		log.Debug("[cc-auto-mode] max_tokens not configured, skipping")
	}

	// 2. Override reasoning_effort if a value is configured.
	if reasoningEffort != "" {
		body = setEffortField(body, "reasoning_effort", reasoningEffort, &modified)
		body = setEffortField(body, "reasoning.effort", reasoningEffort, &modified)
	} else {
		log.Debug("[cc-auto-mode] reasoning_effort not configured, skipping")
	}

	return body, modified
}

// ApplyCCAutoMode applies cc-auto-mode overrides (max_tokens + reasoning_effort)
// to the translated request body at the executor level.
//
// The handler-level redirect (handlers.go:maybeRedirectAutoMode) handles model
// routing and max_tokens in Claude format. This function is a belt-and-suspenders
// layer that ALSO checks isAutoModeClassifier before applying — it only acts
// on actual classifier requests, not on normal chat requests that happen to
// use a model with cc-auto-mode enabled.
//
// It reads the client-facing model name from originalBody (the handler-modified
// raw JSON, containing the alias) for config lookup, since baseModel at this
// point is the upstream name which would not match cc-auto-mode rules.
//
// Returns the (possibly modified) body.
func ApplyCCAutoMode(body []byte, cfg *config.Config, originalBody []byte) []byte {
	// CRITICAL: Only apply overrides to actual classifier requests.
	// Without this check, ALL requests for a model with cc-auto-mode
	// configured would get their max_tokens and reasoning_effort overridden.
	if !isAutoModeClassifier(body) {
		return body
	}

	// Extract client-facing model name from original body for config lookup.
	lookupModel := ""
	if len(originalBody) > 0 {
		lookupModel = gjson.GetBytes(originalBody, "model").String()
	}
	if lookupModel == "" {
		return body
	}
	if !IsCCAutoModeEnabled(cfg, lookupModel) {
		return body
	}

	var modified bool

	// 1. Override max_tokens as a safety net (handler already set it in Claude
	// format, but translation might have altered it).
	if mt := GetCCAutoModeMaxTokens(cfg, lookupModel); mt > 0 {
		cur := int(gjson.GetBytes(body, "max_tokens").Int())
		if cur != mt {
			body, _ = sjson.SetBytes(body, "max_tokens", mt)
			log.WithFields(log.Fields{
				"model": lookupModel,
				"from":  cur,
				"to":    mt,
			}).Info("[cc-auto-mode] max_tokens override at executor level")
			modified = true
		}
	}

	// 2. Override reasoning_effort (only possible in OpenAI format,
	// not in the Claude format that the handler works with).
	if effort := GetCCAutoModeReasoningEffort(cfg, lookupModel); effort != "" {
		body, _ = sjson.SetBytes(body, "reasoning_effort", effort)
		body, _ = sjson.SetBytes(body, "reasoning.effort", effort)
		log.WithFields(log.Fields{
			"model":  lookupModel,
			"effort": effort,
		}).Info("[cc-auto-mode] reasoning_effort override at executor level")
		modified = true
	}

	if modified {
		log.WithFields(log.Fields{
			"model": lookupModel,
		}).Debug("[cc-auto-mode] overrides applied at executor level")
	}
	return body
}

// setEffortField sets a reasoning effort field to the specified value.
func setEffortField(body []byte, path string, value string, modified *bool) []byte {
	current := gjson.GetBytes(body, path).String()
	if current == value {
		return body
	}

	body, err := sjson.SetBytes(body, path, value)
	if err != nil {
		log.WithError(err).Warn("[cc-auto-mode] failed to set " + path)
		return body
	}
	log.WithFields(log.Fields{
		"path": path,
		"from": current,
		"to":   value,
	}).Info("[cc-auto-mode] reasoning effort overridden")
	*modified = true
	return body
}
