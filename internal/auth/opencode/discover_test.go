package opencode

import (
	"testing"
)

const keysPageFixture = `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><script>
window._$HY={events:[],completed:new WeakSet,r:{},fe(){}};
self.$R=self.$R||{};
self.$R["server-fn:1"]=[];
(()=>{
$R=>$R[0]=[
$R[1]={id:"key_test00000000000000000000001",name:"Default API Key",key:"sk-test0000000000000000000000000000000000000000000000000000000000000001",timeUsed:$R[2]=new Date("2026-05-28T03:50:55.000Z"),userID:"usr_test00000000000000000000001",email:"test@example.com",keyDisplay:"sk-test...0001"},
$R[3]={id:"key_test00000000000000000000002",name:"CPA Test Key",key:"sk-test0000000000000000000000000000000000000000000000000000000000000002",timeUsed:null,userID:"usr_test00000000000000000000002",email:"test@example.com",keyDisplay:"sk-test...0002"}
])($R["server-fn:1"]);
})();
</script></head><body><div>API Keys page content</div></body></html>`

func TestExtractKeys_Success(t *testing.T) {
	keys, err := extractFromHTML(keysPageFixture)
	if err != nil {
		t.Fatalf("extractFromHTML: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
	if keys[0].ID != "key_test00000000000000000000001" {
		t.Errorf("wrong key ID: %s", keys[0].ID)
	}
	if keys[0].Name != "Default API Key" {
		t.Errorf("wrong key name: %s", keys[0].Name)
	}
	if keys[0].Key != "sk-test0000000000000000000000000000000000000000000000000000000000000001" {
		t.Errorf("wrong key value: %s", keys[0].Key)
	}
	if keys[0].Display != "sk-test...0001" {
		t.Errorf("wrong key display: %s", keys[0].Display)
	}
}

func TestExtractKeys_NoKeysInPage(t *testing.T) {
	_, err := extractFromHTML("<html><body>No keys here</body></html>")
	if err == nil {
		t.Fatal("expected error for page with no keys")
	}
}

func TestExtractKeys_FallbackPattern(t *testing.T) {
	html := `<html><head><script>{"data":[{"key":"sk-abcdefghijklmnopqrstuvwxyz1234567890ABCDEFGHIJKLMNOPQRSTUVWXYZ12"}]}</script></head><body></body></html>`
	keys, err := extractFromHTML(html)
	if err != nil {
		t.Fatalf("extractFromHTML fallback: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key from fallback, got %d", len(keys))
	}
	expectedDisplay := keys[0].Key[:7] + "..." + keys[0].Key[len(keys[0].Key)-4:]
	if keys[0].Display != expectedDisplay {
		t.Errorf("wrong display from fallback: got %s, want %s", keys[0].Display, expectedDisplay)
	}
}

const goPageFixture = `<script>_$HY.r["lite.subscription.get[\"wrk_01KQ\"]"]=$R[23]=$R[2]($R[24]={p:0,s:0,f:0});
$R[16]($R[24],$R[33]={mine:!0,useBalance:!1,rollingUsage:$R[34]={status:"ok",resetInSec:9490,usagePercent:17},weeklyUsage:$R[35]={status:"ok",resetInSec:121010,usagePercent:58},monthlyUsage:$R[36]={status:"ok",resetInSec:153407,usagePercent:86}});
</script>`

func TestParseGoUsageHTML(t *testing.T) {
	usage, err := parseGoUsageHTML(goPageFixture)
	if err != nil {
		t.Fatalf("parseGoUsageHTML: %v", err)
	}
	if usage.RollingUsagePercent != 17 {
		t.Errorf("expected 17 for rolling, got %d", usage.RollingUsagePercent)
	}
	if usage.RollingResetInSec != 9490 {
		t.Errorf("expected 9490 for rolling reset, got %d", usage.RollingResetInSec)
	}
	if usage.WeeklyUsagePercent != 58 {
		t.Errorf("expected 58 for weekly, got %d", usage.WeeklyUsagePercent)
	}
	if usage.WeeklyResetInSec != 121010 {
		t.Errorf("expected 121010 for weekly reset, got %d", usage.WeeklyResetInSec)
	}
	if usage.MonthlyUsagePercent != 86 {
		t.Errorf("expected 86 for monthly, got %d", usage.MonthlyUsagePercent)
	}
	if usage.MonthlyResetInSec != 153407 {
		t.Errorf("expected 153407 for monthly reset, got %d", usage.MonthlyResetInSec)
	}
	if !usage.Mine {
		t.Error("expected Mine to be true")
	}
	if usage.UseBalance {
		t.Error("expected UseBalance to be false")
	}
}

const goPageFixtureWithBalance = `<script>_$HY.r["lite.subscription.get[\"wrk_01KQ\"]"]=$R[23]=$R[2]($R[24]={p:0,s:0,f:0});
$R[16]($R[24],$R[33]={mine:!0,useBalance:!0,rollingUsage:$R[34]={status:"ok",resetInSec:100,usagePercent:99},weeklyUsage:$R[35]={status:"ok",resetInSec:200,usagePercent:99},monthlyUsage:$R[36]={status:"ok",resetInSec:300,usagePercent:99}});
</script>`

func TestParseGoUsageHTML_UseBalance(t *testing.T) {
	usage, err := parseGoUsageHTML(goPageFixtureWithBalance)
	if err != nil {
		t.Fatalf("parseGoUsageHTML with balance: %v", err)
	}
	if !usage.UseBalance {
		t.Error("expected UseBalance to be true")
	}
	if usage.RollingUsagePercent != 99 {
		t.Errorf("expected 99 for rolling, got %d", usage.RollingUsagePercent)
	}
}

func TestAtoi(t *testing.T) {
	tests := []struct {
		in  string
		out int
	}{
		{"0", 0},
		{"42", 42},
		{"007", 7},
		{"9490", 9490},
		{"abc", 0},
		{"123abc456", 123456},
		{"", 0},
	}
	for _, tc := range tests {
		got := atoi(tc.in)
		if got != tc.out {
			t.Errorf("atoi(%q) = %d, want %d", tc.in, got, tc.out)
		}
	}
}
