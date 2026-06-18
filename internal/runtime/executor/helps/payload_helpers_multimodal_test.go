package helps

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/tidwall/gjson"
)

func ptrBool(b bool) *bool { return &b }

func TestApplyPayloadConfig_SupportsMultimodalNil_NoFiltering(t *testing.T) {
	cfg := &config.Config{
		Payload: config.PayloadConfig{
			Default: []config.PayloadRule{{
				Models: []config.PayloadModelRule{{
					Name:     "test-model",
					Protocol: "openai",
				}},
				Params: map[string]any{"temperature": 0.5},
			}},
		},
	}
	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]}]}`)

	out := ApplyPayloadConfigWithRoot(cfg, "test-model", "openai", "", payload, nil, "", "")

	msg := gjson.GetBytes(out, "messages.0.content")
	if !msg.IsArray() {
		t.Fatalf("expected content array (no filtering for nil), got %v", msg.Type)
	}
}

func TestApplyPayloadConfig_SupportsMultimodalTrue_NoFiltering(t *testing.T) {
	cfg := &config.Config{
		Payload: config.PayloadConfig{
			Default: []config.PayloadRule{{
				Models: []config.PayloadModelRule{{
					Name:               "test-model",
					Protocol:           "openai",
					SupportsMultimodal: ptrBool(true),
				}},
				Params: map[string]any{"temperature": 0.5},
			}},
		},
	}
	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]}]}`)

	out := ApplyPayloadConfigWithRoot(cfg, "test-model", "openai", "", payload, nil, "", "")

	msg := gjson.GetBytes(out, "messages.0.content")
	if !msg.IsArray() {
		t.Fatalf("expected content array (no filtering for true), got %v", msg.Type)
	}
}

func TestApplyPayloadConfig_SupportsMultimodalFalse_OpenAI_StripsImages(t *testing.T) {
	falseVal := false
	cfg := &config.Config{
		Payload: config.PayloadConfig{
			Default: []config.PayloadRule{{
				Models: []config.PayloadModelRule{{
					Name:               "test-model",
					Protocol:           "openai",
					SupportsMultimodal: &falseVal,
				}},
				Params: map[string]any{"temperature": 0.5},
			}},
		},
	}
	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]}]}`)

	out := ApplyPayloadConfigWithRoot(cfg, "test-model", "openai", "", payload, nil, "", "")

	content := gjson.GetBytes(out, "messages.0.content")
	if content.Type != gjson.String {
		t.Fatalf("expected content string after stripping, got %v", content.Type)
	}
	if !gjson.GetBytes(out, "messages.0.content").Exists() {
		t.Fatal("expected content to exist")
	}
	str := content.String()
	if str == "" {
		t.Fatal("expected non-empty content")
	}
}

func TestApplyPayloadConfig_SupportsMultimodalFalse_OpenAI_PreservesText(t *testing.T) {
	falseVal := false
	cfg := &config.Config{
		Payload: config.PayloadConfig{
			Default: []config.PayloadRule{{
				Models: []config.PayloadModelRule{{
					Name:               "test-model",
					Protocol:           "openai",
					SupportsMultimodal: &falseVal,
				}},
			}},
		},
	}
	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"What is in this image?"},{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]}]}`)

	out := ApplyPayloadConfigWithRoot(cfg, "test-model", "openai", "", payload, nil, "", "")

	content := gjson.GetBytes(out, "messages.0.content").String()
	if content == "" {
		t.Fatal("expected non-empty content")
	}
}

func TestApplyPayloadConfig_SupportsMultimodalFalse_OpenAI_PureText_Unchanged(t *testing.T) {
	falseVal := false
	cfg := &config.Config{
		Payload: config.PayloadConfig{
			Default: []config.PayloadRule{{
				Models: []config.PayloadModelRule{{
					Name:               "test-model",
					Protocol:           "openai",
					SupportsMultimodal: &falseVal,
				}},
			}},
		},
	}
	payload := []byte(`{"messages":[{"role":"user","content":"Hello, how are you?"}]}`)

	out := ApplyPayloadConfigWithRoot(cfg, "test-model", "openai", "", payload, nil, "", "")

	content := gjson.GetBytes(out, "messages.0.content")
	if content.Type != gjson.String {
		t.Fatalf("expected content string unchanged, got %v", content.Type)
	}
}

func TestApplyPayloadConfig_SupportsMultimodalFalse_Claude_StripsImages(t *testing.T) {
	falseVal := false
	cfg := &config.Config{
		Payload: config.PayloadConfig{
			Default: []config.PayloadRule{{
				Models: []config.PayloadModelRule{{
					Name:               "claude-model",
					Protocol:           "claude",
					SupportsMultimodal: &falseVal,
				}},
			}},
		},
	}
	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"Describe this"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"abc"}}]}]}`)

	out := ApplyPayloadConfigWithRoot(cfg, "claude-model", "claude", "", payload, nil, "", "")

	content := gjson.GetBytes(out, "messages.0.content")
	if content.Type != gjson.String {
		t.Fatalf("expected content string after stripping, got %v", content.Type)
	}
	if content.String() == "" {
		t.Fatal("expected non-empty content")
	}
}

func TestApplyPayloadConfig_SupportsMultimodalFalse_Gemini_StripsInlineData(t *testing.T) {
	falseVal := false
	cfg := &config.Config{
		Payload: config.PayloadConfig{
			Default: []config.PayloadRule{{
				Models: []config.PayloadModelRule{{
					Name:               "gemini-model",
					Protocol:           "gemini",
					SupportsMultimodal: &falseVal,
				}},
			}},
		},
	}
	payload := []byte(`{"contents":[{"role":"user","parts":[{"text":"Describe this image"},{"inlineData":{"mimeType":"image/png","data":"abc"}}]}]}`)

	out := ApplyPayloadConfigWithRoot(cfg, "gemini-model", "gemini", "", payload, nil, "", "")

	parts := gjson.GetBytes(out, "contents.0.parts")
	if !parts.IsArray() {
		t.Fatalf("expected parts array, got %v", parts.Type)
	}
	arr := parts.Array()
	if len(arr) != 1 {
		t.Fatalf("expected 1 part after stripping, got %d", len(arr))
	}
	if arr[0].Get("text").String() == "" {
		t.Fatal("expected text in remaining part")
	}
}

func TestApplyPayloadConfig_SupportsMultimodalFalse_Codex_StripsInputImage(t *testing.T) {
	falseVal := false
	cfg := &config.Config{
		Payload: config.PayloadConfig{
			Default: []config.PayloadRule{{
				Models: []config.PayloadModelRule{{
					Name:               "codex-model",
					Protocol:           "codex",
					SupportsMultimodal: &falseVal,
				}},
			}},
		},
	}
	payload := []byte(`{"input":[{"role":"user","content":[{"type":"input_text","text":"What do you see?"},{"type":"input_image","image_url":"data:image/png;base64,abc"}]}]}`)

	out := ApplyPayloadConfigWithRoot(cfg, "codex-model", "codex", "", payload, nil, "", "")

	content := gjson.GetBytes(out, "input.0.content")
	if content.Type != gjson.String {
		t.Fatalf("expected content string after stripping, got %v", content.Type)
	}
	if content.String() == "" {
		t.Fatal("expected non-empty content")
	}
}

func TestApplyPayloadConfig_SupportsMultimodalFalse_WildcardMatch(t *testing.T) {
	falseVal := false
	cfg := &config.Config{
		Payload: config.PayloadConfig{
			Default: []config.PayloadRule{{
				Models: []config.PayloadModelRule{{
					Name:               "deepseek-*",
					Protocol:           "openai",
					SupportsMultimodal: &falseVal,
				}},
			}},
		},
	}
	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]}]}`)

	out := ApplyPayloadConfigWithRoot(cfg, "deepseek-chat", "openai", "", payload, nil, "", "")

	content := gjson.GetBytes(out, "messages.0.content")
	if content.Type != gjson.String {
		t.Fatalf("expected content string after stripping (wildcard match), got %v", content.Type)
	}
}

func TestApplyPayloadConfig_SupportsMultimodalFalse_WrongProtocol_NoFiltering(t *testing.T) {
	falseVal := false
	cfg := &config.Config{
		Payload: config.PayloadConfig{
			Default: []config.PayloadRule{{
				Models: []config.PayloadModelRule{{
					Name:               "test-model",
					Protocol:           "openai",
					SupportsMultimodal: &falseVal,
				}},
			}},
		},
	}
	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]}]}`)

	out := ApplyPayloadConfigWithRoot(cfg, "test-model", "claude", "", payload, nil, "", "")

	msg := gjson.GetBytes(out, "messages.0.content")
	if !msg.IsArray() {
		t.Fatalf("expected content array (wrong protocol, no filtering), got %v", msg.Type)
	}
}

func TestApplyPayloadConfig_SupportsMultimodalFalse_Claude_ToolResultPreserved(t *testing.T) {
	falseVal := false
	cfg := &config.Config{
		Payload: config.PayloadConfig{
			Default: []config.PayloadRule{{
				Models: []config.PayloadModelRule{{
					Name:               "claude-model",
					Protocol:           "claude",
					SupportsMultimodal: &falseVal,
				}},
			}},
		},
	}
	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"Analyze this"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"abc"}},{"type":"tool_result","tool_use_id":"toolu_123","content":"Tool output text"}]}]}`)

	out := ApplyPayloadConfigWithRoot(cfg, "claude-model", "claude", "", payload, nil, "", "")

	content := gjson.GetBytes(out, "messages.0.content")
	if content.Type != gjson.String {
		t.Fatalf("expected content string after stripping, got %v", content.Type)
	}
	if content.String() == "" {
		t.Fatal("expected non-empty content")
	}
}

func TestApplyPayloadConfig_SupportsMultimodalFalse_Claude_ToolResultImageDetected(t *testing.T) {
	falseVal := false
	cfg := &config.Config{
		Payload: config.PayloadConfig{
			Default: []config.PayloadRule{{
				Models: []config.PayloadModelRule{{
					Name:               "claude-model",
					Protocol:           "claude",
					SupportsMultimodal: &falseVal,
				}},
			}},
		},
	}
	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"Check this"},{"type":"tool_result","tool_use_id":"toolu_456","content":[{"type":"text","text":"Result text"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"xyz"}}]}]}]}`)

	out := ApplyPayloadConfigWithRoot(cfg, "claude-model", "claude", "", payload, nil, "", "")

	content := gjson.GetBytes(out, "messages.0.content")
	if content.Type != gjson.String {
		t.Fatalf("expected content string after stripping nested image, got %v", content.Type)
	}
	if content.String() == "" {
		t.Fatal("expected non-empty content")
	}
}

func TestApplyPayloadConfig_SupportsMultimodalFalse_Claude_DocumentStripped(t *testing.T) {
	falseVal := false
	cfg := &config.Config{
		Payload: config.PayloadConfig{
			Default: []config.PayloadRule{{
				Models: []config.PayloadModelRule{{
					Name:               "claude-model",
					Protocol:           "claude",
					SupportsMultimodal: &falseVal,
				}},
			}},
		},
	}
	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"Read this PDF"},{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"cGRm"}}]}]}`)

	out := ApplyPayloadConfigWithRoot(cfg, "claude-model", "claude", "", payload, nil, "", "")

	content := gjson.GetBytes(out, "messages.0.content")
	if content.Type != gjson.String {
		t.Fatalf("expected content string after document stripping, got %v", content.Type)
	}
}

func TestApplyPayloadConfig_SupportsMultimodalFalse_Antigravity_StripsInlineData(t *testing.T) {
	falseVal := false
	cfg := &config.Config{
		Payload: config.PayloadConfig{
			Default: []config.PayloadRule{{
				Models: []config.PayloadModelRule{{
					Name:               "ag-model",
					Protocol:           "antigravity",
					SupportsMultimodal: &falseVal,
				}},
			}},
		},
	}
	payload := []byte(`{"request":{"contents":[{"role":"user","parts":[{"text":"Describe this"},{"inlineData":{"mimeType":"image/png","data":"abc"}}]}]}}`)

	out := ApplyPayloadConfigWithRoot(cfg, "ag-model", "antigravity", "request", payload, nil, "", "")

	parts := gjson.GetBytes(out, "request.contents.0.parts")
	if !parts.IsArray() {
		t.Fatalf("expected parts array, got %v", parts.Type)
	}
	arr := parts.Array()
	if len(arr) != 1 {
		t.Fatalf("expected 1 part after stripping, got %d", len(arr))
	}
}

func TestApplyPayloadConfig_SupportsMultimodalFalse_OpenAIResponse_StripsInputImage(t *testing.T) {
	falseVal := false
	cfg := &config.Config{
		Payload: config.PayloadConfig{
			Default: []config.PayloadRule{{
				Models: []config.PayloadModelRule{{
					Name:               "my-model",
					Protocol:           "openai-response",
					SupportsMultimodal: &falseVal,
				}},
			}},
		},
	}
	payload := []byte(`{"input":[{"role":"user","content":[{"type":"input_text","text":"What do you see?"},{"type":"input_image","image_url":"data:image/png;base64,abc"}]}]}`)

	out := ApplyPayloadConfigWithRoot(cfg, "my-model", "openai-response", "", payload, nil, "", "")

	content := gjson.GetBytes(out, "input.0.content")
	if content.Type != gjson.String {
		t.Fatalf("expected content string after stripping, got %v", content.Type)
	}
	if content.String() == "" {
		t.Fatal("expected non-empty content")
	}
}

func TestApplyPayloadConfig_SupportsMultimodalFalse_OpenAI_InputAudioStripped(t *testing.T) {
	falseVal := false
	cfg := &config.Config{
		Payload: config.PayloadConfig{
			Default: []config.PayloadRule{{
				Models: []config.PayloadModelRule{{
					Name:               "audio-model",
					Protocol:           "openai",
					SupportsMultimodal: &falseVal,
				}},
			}},
		},
	}
	payload := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"Transcribe this"},{"type":"input_audio","input_audio":{"data":"YXVkaW8=","format":"wav"}}]}]}`)

	out := ApplyPayloadConfigWithRoot(cfg, "audio-model", "openai", "", payload, nil, "", "")

	content := gjson.GetBytes(out, "messages.0.content")
	if content.Type != gjson.String {
		t.Fatalf("expected content string after input_audio stripping, got %v", content.Type)
	}
}
