package helps

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ccGoalHookSystemPrompt is the additional system prompt injected into goal-hook
// evaluator requests to enforce strict JSON output format. The upstream Claude Code
// /goal evaluator prompt already asks for JSON but does not strongly constrain the
// model, causing it to output markdown reports or fenced JSON instead of raw JSON.
// See: https://github.com/anthropics/claude-code/issues/62246
const ccGoalHookSystemPrompt = `CRITICAL OUTPUT FORMAT REQUIREMENT - Your response MUST be a raw JSON object ONLY:

- DO NOT use any tools or functions
- NO markdown code fences (do NOT wrap in json code blocks)
- NO explanatory text before or after the JSON
- NO additional commentary, headings, or formatting
- Respond with EXACTLY a valid JSON object starting with { and ending with }

Your ENTIRE response must be parseable by JSON.parse() directly.
Any text outside the JSON object will cause a validation error.

Required schema:
{"ok": true, "reason": "<evidence from transcript>"}
or
{"ok": false, "reason": "<what is missing or blocking>"}
or
{"ok": false, "impossible": true, "reason": "<explain why>"}`

// ccGoalHookDetectPhrase is the text fragment used to detect goal-hook evaluation requests.
// It appears in the system prompt of Claude Code's /goal stop-condition evaluator.
const ccGoalHookDetectPhrase = "evaluating a stop-condition hook"

// IsCCGoalHookEnabled checks whether the given model has cc-goal-hook enabled
// in the payload configuration rules. It scans PayloadModelRule entries across
// all payload rule lists for CCGoalHook == true with a matching model name pattern.
func IsCCGoalHookEnabled(cfg *config.Config, model string) bool {
	if cfg == nil || model == "" {
		log.Warn("[cc-goal-hook] IsCCGoalHookEnabled: cfg is nil or model is empty")
		return false
	}
	candidates := payloadModelCandidates(model, "")
	rules := collectCCGoalHookRules(cfg)
	log.WithFields(log.Fields{
		"model":       model,
		"candidates":  candidates,
		"rules_found": len(rules),
	}).Info("[cc-goal-hook] IsCCGoalHookEnabled check")
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
			}).Info("[cc-goal-hook] model matched, injection enabled")
			return true
		}
	}
	log.WithFields(log.Fields{
		"model": model,
	}).Info("[cc-goal-hook] no matching rule found")
	return false
}

// collectCCGoalHookRules collects all PayloadModelRule entries where CCGoalHook is true.
func collectCCGoalHookRules(cfg *config.Config) []config.PayloadModelRule {
	var rules []config.PayloadModelRule
	collect := func(ruleList []config.PayloadRule) {
		for _, rule := range ruleList {
			for _, mr := range rule.Models {
				if mr.CCGoalHook != nil && *mr.CCGoalHook {
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

// scanBodyForGoalHookDetectPhrase checks if any system-level content in the body
// contains the goal hook detection phrase. It checks both:
//   - top-level "system" array (standard Claude Messages API format)
//   - messages[0] with role="system" (alternative format)
func scanBodyForGoalHookDetectPhrase(body []byte) (found bool, isTopLevelSystem bool) {
	// Check top-level "system" array first (standard Claude Messages API format).
	system := gjson.GetBytes(body, "system")
	if system.IsArray() {
		system.ForEach(func(_, part gjson.Result) bool {
			if part.Get("type").String() != "text" {
				return true
			}
			text := part.Get("text").String()
			if strings.Contains(text, ccGoalHookDetectPhrase) {
				found = true
				return false
			}
			return true
		})
		if found {
			return true, true
		}
	}

	// Check messages[0] with role="system" (alternative format).
	firstMsg := gjson.GetBytes(body, "messages.0")
	if firstMsg.Get("role").String() == "system" {
		content := firstMsg.Get("content")
		if content.IsArray() {
			content.ForEach(func(_, part gjson.Result) bool {
				if part.Get("type").String() != "text" {
					return true
				}
				text := part.Get("text").String()
				if strings.Contains(text, ccGoalHookDetectPhrase) {
					found = true
					return false
				}
				return true
			})
			if found {
				return true, false
			}
		} else if content.Type == gjson.String {
			if strings.Contains(content.String(), ccGoalHookDetectPhrase) {
				return true, false
			}
		}
	}

	return false, false
}

// InjectGoalHookConstraint detects goal-hook evaluation requests in the body and
// injects a stronger JSON formatting constraint into the system prompt.
// Also sets tool_choice to "none" to prevent the model from calling tools
// instead of outputting raw JSON.
// Returns the (possibly modified) body and a boolean indicating whether injection occurred.
func InjectGoalHookConstraint(body []byte) ([]byte, bool) {
	if !gjson.ValidBytes(body) {
		log.Warn("[cc-goal-hook] InjectGoalHookConstraint: invalid JSON body")
		return body, false
	}

	// Detect goal-hook evaluation request.
	found, isTopLevel := scanBodyForGoalHookDetectPhrase(body)
	if !found {
		log.WithFields(log.Fields{
			"has_system": gjson.GetBytes(body, "system").Exists(),
			"first_role": gjson.GetBytes(body, "messages.0.role").String(),
		}).Info("[cc-goal-hook] goal hook phrase not found in body")
		return body, false
	}

	log.WithFields(log.Fields{
		"is_top_level_system": isTopLevel,
	}).Info("[cc-goal-hook] goal hook detected in body, injecting constraint")

	if isTopLevel {
		// Inject into top-level "system" array (Claude Messages API format).
		textBlockCount := 0
		system := gjson.GetBytes(body, "system")
		system.ForEach(func(_, part gjson.Result) bool {
			if part.Get("type").String() == "text" {
				textBlockCount++
			}
			return true
		})

		var err error
		body, err = sjson.SetBytes(body, "system.-1", map[string]any{
			"type": "text",
			"text": ccGoalHookSystemPrompt,
		})
		if err != nil {
			log.WithError(err).Warn("[cc-goal-hook] failed to inject into system array")
			return body, false
		}
		log.WithFields(log.Fields{
			"text_block_count": textBlockCount,
		}).Info("[cc-goal-hook] injected into top-level system array")
	} else {
		// Inject into messages[0].content array (system role message).
		contentVal := gjson.GetBytes(body, "messages.0.content")
		textBlockCount := 0
		if contentVal.IsArray() {
			contentVal.ForEach(func(_, part gjson.Result) bool {
				if part.Get("type").String() == "text" {
					textBlockCount++
				}
				return true
			})
		}

		var err error
		// sjson -1 append corrupts string values, so convert string content
		// to an array of content blocks first.
		if contentVal.Type == gjson.String {
			textBlockCount = 1
			body, err = sjson.SetBytes(body, "messages.0.content",
				[]any{map[string]any{"type": "text", "text": contentVal.String()}})
			if err != nil {
				log.WithError(err).Warn("[cc-goal-hook] failed to convert messages[0].content to array")
				return body, false
			}
		}

		body, err = sjson.SetBytes(body, "messages.0.content.-1", map[string]any{
			"type": "text",
			"text": ccGoalHookSystemPrompt,
		})
		if err != nil {
			log.WithError(err).Warn("[cc-goal-hook] failed to inject into messages[0].content")
			return body, false
		}
		log.WithFields(log.Fields{
			"text_block_count": textBlockCount,
		}).Info("[cc-goal-hook] injected into messages[0].content")
	}

	// Disable tool calling so the model outputs raw JSON instead of
	// continuing to use tools from conversation context.
	var err error
	body, err = sjson.SetBytes(body, "tool_choice", "none")
	if err != nil {
		log.WithError(err).Warn("[cc-goal-hook] failed to set tool_choice=none")
		return body, false
	}
	log.Info("[cc-goal-hook] set tool_choice=none")

	return body, true
}
