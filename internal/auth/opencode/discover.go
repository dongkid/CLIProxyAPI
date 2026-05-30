package opencode

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
)

// NewClient creates a proxy-aware HTTP client for OpenCode API calls.
// Uses util.SetProxy for full SOCKS5/HTTP/HTTPS proxy support (same as xAI/kimi auth packages).
func NewClient(cfg *config.Config) *http.Client {
	client := &http.Client{Timeout: 30 * time.Second}
	if cfg != nil {
		sdkCfg := cfg.SDKConfig
		sdkCfg.ProxyURL = cfg.ProxyURL
		client = util.SetProxy(&sdkCfg, client)
	}
	return client
}

// DiscoverWorkspace discovers the workspace ID.
// OpenCode is a SolidJS SPA — /workspace uses client-side JS redirect, not HTTP 3xx.
// Strategy: 1) follow HTTP redirect, 2) scan HTML body for wrk_ patterns,
// 3) return a clear error telling the user to provide workspace_id manually.
func DiscoverWorkspace(client *http.Client, cookie string) (string, error) {
	req, err := http.NewRequest("GET", "https://opencode.ai/workspace", nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("User-Agent", "CPA/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	// Strategy 1: extract workspace ID from the final request URL (HTTP redirect)
	finalURL := resp.Request.URL.String()
	for _, part := range strings.Split(strings.TrimRight(finalURL, "/"), "/") {
		if strings.HasPrefix(part, "wrk_") {
			return part, nil
		}
	}

	// Strategy 2: scan HTML body for wrk_ patterns (SSR data or links)
	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	wrkRe := regexp.MustCompile(`wrk_[a-zA-Z0-9]+`)
	if m := wrkRe.FindString(html); m != "" {
		return m, nil
	}

	// Strategy 3: neither worked — OpenCode SPA requires JS execution
	return "", fmt.Errorf(
		"auto-discovery failed. OpenCode uses client-side redirect, please provide workspace_id manually. "+
			"You can find it in the browser URL bar when on opencode.ai: /workspace/<b>wrk_xxx</b>")
}

// ExtractKeys extracts API keys from OpenCode's workspace keys page SSR HTML.
func ExtractKeys(client *http.Client, cookie, wspID string) ([]KeyEntry, error) {
	keysURL := fmt.Sprintf("https://opencode.ai/workspace/%s/keys", wspID)
	req, err := http.NewRequest("GET", keysURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("User-Agent", "CPA/1.0")
	req.Header.Set("Accept", "text/html")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode >= 500 {
			return nil, fmt.Errorf("opencode returned status %d — cookie may be expired, please refresh from browser DevTools", resp.StatusCode)
		}
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body failed: %w", err)
	}
	// Three-layer extraction strategy for key data in SSR HTML
	html := string(body)

	// Detect redirect to auth (cookie expired)
	if strings.Contains(html, "auth.opencode.ai/authorize") || strings.Contains(html, "auth.opencode.ai/login") {
		return nil, fmt.Errorf("cookie expired or invalid — redirected to auth")
	}

	return extractFromHTML(html)
}

// extractFromHTML extracts API keys from SSR HTML content (exported for testing).
// OpenCode uses SolidJS _$HY.r serialization — JavaScript object literals.
func extractFromHTML(html string) ([]KeyEntry, error) {
	var entries []KeyEntry

	// Strategy 1: Find key objects with full metadata (id, name, key, keyDisplay)
	// The SSR data uses JS syntax: id:"key_xxx",name:"Name",key:"sk-...",keyDisplay:"sk-..."

	// Find all key objects by locating sk- patterns in object literals
	objPattern := regexp.MustCompile(`\{[^{}]*?\bid\s*:\s*"(key_[a-zA-Z0-9]+)"[^{}]*?\bname\s*:\s*"([^"]*)"[^{}]*?\bkey\s*:\s*"(sk-[a-zA-Z0-9]{40,})"[^{}]*?\bkeyDisplay\s*:\s*"(sk-[^"]*)"`)
	objMatches := objPattern.FindAllStringSubmatch(html, -1)
	for _, m := range objMatches {
		if len(m) >= 5 {
			entries = append(entries, KeyEntry{
				ID: m[1], Name: m[2], Key: m[3], Display: m[4],
			})
		}
	}
	if len(entries) > 0 {
		return entries, nil
	}

	// Strategy 2: Find key fields within object literals (partial match)
	// Match key:"sk-..." followed by keyDisplay:"sk-..." anywhere
	pairRe := regexp.MustCompile(`\bkey\s*:\s*"(sk-[a-zA-Z0-9]{40,})"[^}]*\bkeyDisplay\s*:\s*"(sk-[^"]+)"`)
	pairMatches := pairRe.FindAllStringSubmatch(html, -1)
	for i, m := range pairMatches {
		if len(m) >= 3 {
			entries = append(entries, KeyEntry{
				ID: fmt.Sprintf("key-%d", i), Name: fmt.Sprintf("API Key %d", i+1),
				Key: m[1], Display: m[2],
			})
		}
	}
	if len(entries) > 0 {
		return entries, nil
	}

	// Strategy 3: Just find any sk- key value (last resort)
	anySkRe := regexp.MustCompile(`"(sk-[a-zA-Z0-9]{40,})"`)
	anyMatches := anySkRe.FindAllStringSubmatch(html, -1)
	if len(anyMatches) > 0 {
		seen := make(map[string]bool)
		for i, m := range anyMatches {
			k := m[1]
			if seen[k] { continue }
			seen[k] = true
			entries = append(entries, KeyEntry{
				ID: fmt.Sprintf("key-%d", i), Name: fmt.Sprintf("API Key %d", i+1),
				Key: k, Display: k[:7] + "..." + k[len(k)-4:],
			})
		}
		return entries, nil
	}

	return nil, fmt.Errorf("no API keys found — cookie may be expired or workspace may have no keys (try refreshing cookie from browser DevTools → Application → Cookies → opencode.ai → auth)")
}
