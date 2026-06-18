package opencode

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
)

// ModelEntry represents a model returned by the OpenCode Go API.
type ModelEntry struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// GoUsage holds Go subscription usage data extracted from SSR HTML.
type GoUsage struct {
	RollingUsagePercent int  `json:"rolling_usage_percent"`
	RollingResetInSec   int  `json:"rolling_reset_in_sec"`
	WeeklyUsagePercent  int  `json:"weekly_usage_percent"`
	WeeklyResetInSec    int  `json:"weekly_reset_in_sec"`
	MonthlyUsagePercent int  `json:"monthly_usage_percent"`
	MonthlyResetInSec   int  `json:"monthly_reset_in_sec"`
	UseBalance          bool `json:"use_balance"`
	Mine                bool `json:"mine"`
}

// FetchModels retrieves the available Go models using the API key.
// The client should be proxy-aware (use NewClient).
func FetchModels(client *http.Client, apiKey string) ([]ModelEntry, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequest("GET", "https://opencode.ai/zen/go/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("opencode models: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("User-Agent", "CPA/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opencode models: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("opencode models: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result struct {
		Data []ModelEntry `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("opencode models: parse response: %w", err)
	}
	return result.Data, nil
}

// FetchGoUsage extracts Go subscription usage data from the SSR HTML using the auth cookie.
func FetchGoUsage(client *http.Client, cookie, wspID string) (*GoUsage, error) {
	goURL := fmt.Sprintf("https://opencode.ai/workspace/%s/go", wspID)
	req, err := http.NewRequest("GET", goURL, nil)
	if err != nil {
		return nil, fmt.Errorf("opencode usage: create request: %w", err)
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("User-Agent", "CPA/1.0")
	req.Header.Set("Accept", "text/html")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opencode usage: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opencode usage: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("opencode usage: read body: %w", err)
	}
	return parseGoUsageHTML(string(body))
}

// parseGoUsageHTML extracts Go plan usage from SolidJS _$HY.r SSR serialization.
func parseGoUsageHTML(html string) (*GoUsage, error) {
	var u GoUsage

	re := regexp.MustCompile(`rollingUsage:\$R\[[^\]]+\]=\{status:"ok",resetInSec:(\d+),usagePercent:(\d+)\}`)
	if m := re.FindStringSubmatch(html); len(m) >= 3 {
		u.RollingResetInSec = atoi(m[1])
		u.RollingUsagePercent = atoi(m[2])
	}

	re = regexp.MustCompile(`weeklyUsage:\$R\[[^\]]+\]=\{status:"ok",resetInSec:(\d+),usagePercent:(\d+)\}`)
	if m := re.FindStringSubmatch(html); len(m) >= 3 {
		u.WeeklyResetInSec = atoi(m[1])
		u.WeeklyUsagePercent = atoi(m[2])
	}

	re = regexp.MustCompile(`monthlyUsage:\$R\[[^\]]+\]=\{status:"ok",resetInSec:(\d+),usagePercent:(\d+)\}`)
	if m := re.FindStringSubmatch(html); len(m) >= 3 {
		u.MonthlyResetInSec = atoi(m[1])
		u.MonthlyUsagePercent = atoi(m[2])
	}

	u.Mine = !strings.Contains(html, `mine:!1`)
	u.UseBalance = strings.Contains(html, `useBalance:!0`)

	return &u, nil
}

func atoi(s string) int {
	var n int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

// VerifyKey checks whether an OpenCode API key is valid by calling the models endpoint.
// The client should be proxy-aware (use NewClient).
// A non-2xx response means the key is invalid, revoked, or the network is unreachable.
func VerifyKey(client *http.Client, apiKey string) error {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequest("GET", "https://opencode.ai/zen/go/v1/models", nil)
	if err != nil {
		return fmt.Errorf("opencode verify: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("User-Agent", "CPA/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("opencode verify: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("opencode verify: key invalid (status %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// WorkspaceEntry represents a workspace from OpenCode's SSR HTML.
type WorkspaceEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ExtractWorkspaces extracts the list of workspaces from OpenCode SSR HTML.
// Tries multiple pages: first the product page (/go) to get any workspace ID,
// then the workspace keys page to get the full list with names.
func ExtractWorkspaces(client *http.Client, cookie string) ([]WorkspaceEntry, error) {
	// Strategy 1: Try /go page — may have workspace IDs in SSR (but no names)
	entries, _ := fetchAndParseWorkspaces(client, cookie, "https://opencode.ai/go")
	if len(entries) > 0 {
		return entries, nil
	}

	// Strategy 2: Extract any wrk_ ID from /go, then use it to fetch full list
	wrkID := extractAnyWorkspaceID(client, cookie)
	if wrkID == "" {
		return nil, fmt.Errorf("no workspaces found — cookie may be invalid")
	}

	keysURL := fmt.Sprintf("https://opencode.ai/workspace/%s/keys", wrkID)
	entries, err := fetchAndParseWorkspaces(client, cookie, keysURL)
	if err == nil && len(entries) > 0 {
		return entries, nil
	}

	// Fallback: return the single workspace we found
	return []WorkspaceEntry{{ID: wrkID, Name: "Default"}}, nil
}

func extractAnyWorkspaceID(client *http.Client, cookie string) string {
	req, _ := http.NewRequest("GET", "https://opencode.ai/go", nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("User-Agent", "CPA/1.0")
	req.Header.Set("Accept", "text/html")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	re := regexp.MustCompile(`(wrk_[a-zA-Z0-9]{10,40})`)
	m := re.FindStringSubmatch(string(body))
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}

func fetchAndParseWorkspaces(client *http.Client, cookie, url string) ([]WorkspaceEntry, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("User-Agent", "CPA/1.0")
	req.Header.Set("Accept", "text/html")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return extractWorkspacesFromHTML(string(body))
}

func extractWorkspacesFromHTML(html string) ([]WorkspaceEntry, error) {
	// SolidJS SSR serialization: {id:"wrk_xxx",name:"Name",slug:null}
	re := regexp.MustCompile(`\{id:"(wrk_[a-zA-Z0-9]+)",name:"([^"]*)",slug:\w+\}`)
	matches := re.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		// Fallback: {id:"wrk_xxx",name:"Name"}
		re2 := regexp.MustCompile(`\{id:"(wrk_[a-zA-Z0-9]+)",name:"([^"]*)"`)
		matches = re2.FindAllStringSubmatch(html, -1)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no workspaces found in SSR HTML — cookie may be invalid")
	}

	seen := make(map[string]bool)
	var entries []WorkspaceEntry
	for _, m := range matches {
		if len(m) >= 3 && !seen[m[1]] {
			seen[m[1]] = true
			entries = append(entries, WorkspaceEntry{ID: m[1], Name: m[2]})
		}
	}
	return entries, nil
}

// IsAuthError reports whether the error from VerifyKey indicates an authentication
// failure (401/403) as opposed to a transient network issue.
func IsAuthError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "key invalid")
}

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
	html := string(body)

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
			if seen[k] {
				continue
			}
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
