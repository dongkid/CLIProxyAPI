package handlers

// CPA PATCH: auto mode classifier redirect — handler level.
// This file is entirely a CPA addition — zero upstream code.
// The handler-level redirect runs BEFORE getRequestDetailsWithOptions,
// so the new model determines the full provider/auth/upstream routing path.
// Only safe Claude-format fields are modified here (model, max_tokens).
// Reasoning effort overrides are done at the executor level after translation.
// Delete this file if upstream adopts its own auto mode routing logic.

import (
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// maybeRedirectAutoMode checks whether the request is a Claude Code auto
// mode classifier. If a redirect is configured, it rewrites the model in
// the request body and returns the new model name. Also injects max_tokens
// from the target model's cc-auto-mode config.
//
// This must run BEFORE getRequestDetailsWithOptions so the new model
// determines the provider/auth/upstream selection path.
//
// NOTE: max_tokens override only happens when a redirect actually occurred.
// For models with cc-auto-mode (no redirect), the executor-level
// ApplyCCAutoMode handles overrides AFTER verifying isAutoModeClassifier.
// This prevents accidental max_tokens override on non-classifier requests.
func (h *BaseAPIHandler) maybeRedirectAutoMode(rawJSON []byte, modelName string) (newModel string, newBody []byte) {
	newModel = modelName
	newBody = rawJSON

	if h.AuthManager == nil {
		return
	}

	// Step 1: Detect classifier and apply redirect.
	redirectModel, ok := h.AuthManager.CheckAutoModeRedirect(rawJSON, modelName)
	if !ok {
		// Not a classifier or no redirect configured.
		// Executor-level ApplyCCAutoMode will handle overrides if needed.
		return
	}
	newModel = redirectModel
	newBody, _ = sjson.SetBytes(rawJSON, "model", redirectModel)

	// Step 2: Inject max_tokens (valid in both Claude and OpenAI formats).
	// Only runs when redirect occurred (implies classifier was detected).
	// reasoning_effort is NOT set here — it's not a Claude format field.
	if maxTokens, _ := h.AuthManager.GetAutoModeOverrides(newModel); maxTokens > 0 {
		cur := int(gjson.GetBytes(newBody, "max_tokens").Int())
		if cur != maxTokens {
			newBody, _ = sjson.SetBytes(newBody, "max_tokens", maxTokens)
		}
	}
	return
}
