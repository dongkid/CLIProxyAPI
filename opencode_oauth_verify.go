//go:build ignore

package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func main() {
	fmt.Println("=== OpenCode OAuth Minimal Verification ===\n")

	debugURL := "ws://127.0.0.1:9222"
	if u := os.Getenv("CHROME_DEBUG_URL"); u != "" {
		debugURL = u
	}

	actx, acancel := chromedp.NewRemoteAllocator(context.Background(), debugURL)
	defer acancel()

	ctx, ccancel := chromedp.NewContext(actx)
	defer ccancel()

	ctx, tcancel := context.WithTimeout(ctx, 60*time.Second)
	defer tcancel()

	// Navigate to opencode so we can access cookies in that domain
	_ = chromedp.Run(ctx, chromedp.Navigate("https://opencode.ai/workspace"))
	chromedp.Run(ctx, chromedp.Sleep(2*time.Second))

	// Extract opencode.ai cookies via CDP
	var sessionCookies []string
	_ = chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		cookies, err := network.GetCookies().Do(ctx)
		if err != nil {
			return err
		}
		for _, c := range cookies {
			if strings.Contains(c.Domain, "opencode.ai") &&
				(strings.Contains(c.Name, "auth") || strings.Contains(c.Name, "session")) {
				fmt.Printf("  ✓ %s: %s... (domain=%s, httpOnly=%v)\n",
					c.Name, mask(c.Value, 20), c.Domain, c.HTTPOnly)
				sessionCookies = append(sessionCookies,
					fmt.Sprintf("%s=%s", c.Name, c.Value))
			}
		}
		return nil
	}))

	if len(sessionCookies) == 0 {
		fmt.Println("  FAILED: No session cookies found. Are you logged into OpenCode?")
		os.Exit(1)
	}

	cookieHeader := strings.Join(sessionCookies, "; ")
	fmt.Println()

	// -- Test with Go HTTP client ----------
	fmt.Println("[Test 1] Calling /auth/session with session cookie...")
	client := &http.Client{}

	req1, _ := http.NewRequest("GET", "https://opencode.ai/auth/session", nil)
	req1.Header.Set("Cookie", cookieHeader)
	req1.Header.Set("Accept", "application/json")
	resp1, err := client.Do(req1)
	if err != nil {
		fmt.Printf("  ERROR: %v\n", err)
	} else {
		body, _ := io.ReadAll(resp1.Body)
		resp1.Body.Close()
		fmt.Printf("  Status: %d | Body: %s\n", resp1.StatusCode, trunc(string(body), 200))
	}

	// Test 2: Go subscription
	fmt.Println("\n[Test 2] Calling Go subscription _server endpoint...")
	args := fmt.Sprintf(`{"t":{"t":9,"i":0,"l":1,"a":[{"t":1,"s":"wrk_01KQHBH445N9GHMZZV7W4A6R1Q"}],"o":0},"f":10,"m":[]}`)
	goURL := fmt.Sprintf("https://opencode.ai/_server?id=test&args=%s", url.QueryEscape(args))

	req2, _ := http.NewRequest("GET", goURL, nil)
	req2.Header.Set("Cookie", cookieHeader)
	resp2, err := client.Do(req2)
	if err != nil {
		fmt.Printf("  ERROR: %v\n", err)
	} else {
		body, _ := io.ReadAll(resp2.Body)
		resp2.Body.Close()
		bodyStr := string(body)
		fmt.Printf("  Status: %d | Len: %d\n", resp2.StatusCode, len(bodyStr))

		markers := []string{"rollingUsage", "weeklyUsage", "monthlyUsage", "useBalance", "mine"}
		success := false
		for _, m := range markers {
			if strings.Contains(bodyStr, m) {
				fmt.Printf("  ✓ Found marker: %s\n", m)
				success = true
			}
		}
		if success {
			fmt.Printf("\n  ★ SUCCESS: Session cookie → API call works!\n")
		}
	}

	// Test 3: List keys
	fmt.Println("\n[Test 3] Listing API keys...")
	req3, _ := http.NewRequest("GET", "https://opencode.ai/workspace/wrk_01KQHBH445N9GHMZZV7W4A6R1Q/keys", nil)
	req3.Header.Set("Cookie", cookieHeader)
	resp3, _ := client.Do(req3)
	body3, _ := io.ReadAll(resp3.Body)
	resp3.Body.Close()
	fmt.Printf("  Status: %d | Contains sk-: %v\n", resp3.StatusCode, strings.Contains(string(body3), "sk-"))

	fmt.Println("\n==============================================")
	fmt.Println("  OAuth Flow Architecture (verified in browser):")
	fmt.Println("==============================================")
	fmt.Println("  ① auth.opencode.ai/authorize?state=xxx → consent page")
	fmt.Println("  ② auth.opencode.ai/github/authorize → GitHub OAuth")
	fmt.Println("     (client_id=Iv23liOTxMmED77mtyGd)")
	fmt.Println("  ③ GitHub callback → auth.opencode.ai/github/callback?code=gh_xxx")
	fmt.Println("  ④ OpenAuth exchanges GitHub code → OpenCode code")
	fmt.Println("  ⑤ 302 → opencode.ai/auth/callback?code=xxx&state=xxx")
	fmt.Println("     → Set-Cookie: auth=... (opencode.ai)")
	fmt.Println("     → 302 → /workspace")
	fmt.Println("")
	fmt.Println("  CPA OAuth integration needs: intercept step ⑤")
	fmt.Println("  and exchange code for session cookie programmatically.")
}

func mask(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
