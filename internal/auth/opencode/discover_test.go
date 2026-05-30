package opencode

import (
	"testing"
)

const keysPageFixture = `<!DOCTYPE html>
<html lang="zh-Hans"><head><meta charset="utf-8"><script>
window._$HY={events:[],completed:new WeakSet,r:{},fe(){}};
self.$R=self.$R||{};
self.$R["server-fn:1"]=[];
(()=>{
$R=>$R[0]=[
$R[1]={id:"key_01KQHBH4KGVW0J34VTH2Z6XPEP",name:"Default API Key",key:"sk-mi9vSYILMw84RKHyEUiQBRrqiZOyrXTmha6ZTzCgkdqcboz7HMjpe1erNOwH2m7w",timeUsed:$R[2]=new Date("2026-05-28T03:50:55.000Z"),userID:"usr_01KQHBH445G48E91SNNRGHN2KY",email:"rxie5012@gmail.com",keyDisplay:"sk-mi9v...2m7w"},
$R[3]={id:"key_01KSVRT0EEPPRTKBMJ4ZPJ1WRT",name:"CPA Test Key",key:"sk-CeaYw3GAvckAVl2VKayn1wjTRfAcI3cxTjulUxdRNkt3dgQeyp8V5amSyWS4KhsN",timeUsed:null,userID:"usr_01KQHBH445G48E91SNNRGHN2KY",email:"rxie5012@gmail.com",keyDisplay:"sk-CeaY...KhsN"}
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
	if keys[0].ID != "key_01KQHBH4KGVW0J34VTH2Z6XPEP" {
		t.Errorf("wrong key ID: %s", keys[0].ID)
	}
	if keys[0].Name != "Default API Key" {
		t.Errorf("wrong key name: %s", keys[0].Name)
	}
	if keys[0].Key != "sk-mi9vSYILMw84RKHyEUiQBRrqiZOyrXTmha6ZTzCgkdqcboz7HMjpe1erNOwH2m7w" {
		t.Errorf("wrong key value: %s", keys[0].Key)
	}
	if keys[0].Display != "sk-mi9v...2m7w" {
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
