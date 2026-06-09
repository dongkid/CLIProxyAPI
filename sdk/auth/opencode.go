package auth

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/opencode"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// OpenCodeAuthenticator implements cookie-based API key discovery for OpenCode Go.
// OpenCode does not support OAuth redirect_uri — login uses the browser auth cookie
// to discover API keys from the workspace's SSR HTML.
type OpenCodeAuthenticator struct{}

// NewOpenCodeAuthenticator constructs a new OpenCode authenticator.
func NewOpenCodeAuthenticator() Authenticator {
	return &OpenCodeAuthenticator{}
}

// Provider returns the provider key for opencode.
func (OpenCodeAuthenticator) Provider() string {
	return "opencode"
}

// RefreshLead returns nil — OpenCode API keys have no known expiry schedule,
// so the auto-refresh loop does not apply.
func (OpenCodeAuthenticator) RefreshLead() *time.Duration {
	return nil
}

// Login discovers API keys using the browser auth cookie and saves the selected
// key to the auth store. The cookie and workspace ID may be supplied via
// opts.Metadata["cookie"] / opts.Metadata["workspace_id"] or prompted
// interactively.
func (OpenCodeAuthenticator) Login(ctx context.Context, cfg *config.Config, opts *LoginOptions) (*coreauth.Auth, error) {
	if cfg == nil {
		return nil, fmt.Errorf("cliproxy auth: configuration is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if opts == nil {
		opts = &LoginOptions{}
	}

	cookie := strings.TrimSpace(metaOrEmpty(opts.Metadata, "cookie"))
	if cookie == "" && opts.Prompt != nil {
		val, err := opts.Prompt("Enter OpenCode auth cookie (F12 → Application → Cookies → opencode.ai → auth): ")
		if err != nil {
			return nil, fmt.Errorf("opencode: failed to read cookie: %w", err)
		}
		cookie = strings.TrimSpace(val)
	}
	if cookie != "" && !strings.HasPrefix(cookie, "auth=") {
		cookie = "auth=" + cookie
	}
	if cookie == "" {
		return nil, fmt.Errorf("opencode: auth cookie is required (get it from opencode.ai browser DevTools)")
	}

	wspID := strings.TrimSpace(metaOrEmpty(opts.Metadata, "workspace_id"))
	if wspID == "" && opts.Prompt != nil {
		val, err := opts.Prompt("Enter OpenCode workspace ID: ")
		if err != nil {
			return nil, fmt.Errorf("opencode: failed to read workspace ID: %w", err)
		}
		wspID = strings.TrimSpace(val)
	}
	if wspID == "" {
		return nil, fmt.Errorf("opencode: workspace ID is required")
	}

	client := opencode.NewClient(cfg)
	keys, err := opencode.ExtractKeys(client, cookie, wspID)
	if err != nil {
		return nil, fmt.Errorf("opencode: key discovery failed: %w", err)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("opencode: no API keys found in workspace %s", wspID)
	}

	selectedKey := pickKey(keys, opts)
	if selectedKey == nil {
		return nil, fmt.Errorf("opencode: no key selected")
	}

	label := strings.TrimSpace(metaOrEmpty(opts.Metadata, "key_name"))
	if label == "" {
		label = selectedKey.Name
	}
	if label == "" {
		label = "OpenCode Go"
	}

	fileName, err := opencode.CredentialFileName(cfg.AuthDir, label)
	if err != nil {
		return nil, fmt.Errorf("opencode: filename generation failed: %w", err)
	}

	now := time.Now()
	metadata := map[string]any{
		"type":        "opencode",
		"api_key":     selectedKey.Key,
		"key_display": selectedKey.Display,
		"workspace":   wspID,
		"key_id":      selectedKey.ID,
		"key_name":    selectedKey.Name,
		"timestamp":   now.UnixMilli(),
	}

	if errVerify := opencode.VerifyKey(client, selectedKey.Key); errVerify != nil {
		if opencode.IsAuthError(errVerify) {
			return nil, fmt.Errorf("opencode: key verification failed — key may be invalid or revoked: %v", errVerify)
		}
		log.Warnf("opencode: key verification returned warning: %v", errVerify)
	} else {
		log.Info("opencode: API key verified successfully")
	}

	tokenStore := &opencode.TokenStorage{
		Type:      "opencode",
		Label:     label,
		Key:       selectedKey.Key,
		Workspace: wspID,
	}

	fmt.Printf("\nOpenCode authentication successful!\n")
	fmt.Printf("  Workspace: %s\n", wspID)
	fmt.Printf("  Key:       %s\n", selectedKey.Display)

	return &coreauth.Auth{
		ID:       fileName,
		Provider: "opencode",
		FileName: fileName,
		Label:    label,
		Storage:  tokenStore,
		Metadata: metadata,
		Attributes: map[string]string{
			"api_key":     selectedKey.Key,
			"key_display": selectedKey.Display,
			"workspace":   wspID,
		},
		CreatedAt: now,
		UpdatedAt: now,
		Status:    coreauth.StatusActive,
	}, nil
}

func pickKey(keys []opencode.KeyEntry, opts *LoginOptions) *opencode.KeyEntry {
	if len(keys) == 1 {
		return &keys[0]
	}
	if opts.Prompt != nil {
		fmt.Println("\nMultiple API keys found:")
		for i, k := range keys {
			fmt.Printf("  %d: %s (%s)\n", i+1, k.Name, k.Display)
		}
		raw, err := opts.Prompt(fmt.Sprintf("Select key (1-%d): ", len(keys)))
		if err != nil {
			return &keys[0]
		}
		idx, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || idx < 1 || idx > len(keys) {
			return &keys[0]
		}
		return &keys[idx-1]
	}
	return &keys[0]
}

func metaOrEmpty(m map[string]string, key string) string {
	if m == nil {
		return ""
	}
	return m[key]
}
