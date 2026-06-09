package opencode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialFileName_Basic(t *testing.T) {
	dir := t.TempDir()
	name, err := CredentialFileName(dir, "Default API Key")
	if err != nil {
		t.Fatal(err)
	}
	if name != "opencode-default-api-key.json" {
		t.Errorf("expected opencode-default-api-key.json, got %s", name)
	}
}

func TestCredentialFileName_Empty(t *testing.T) {
	dir := t.TempDir()
	name, err := CredentialFileName(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if name == "" || name == "opencode-.json" {
		t.Errorf("empty label should produce timestamp-based filename, got %s", name)
	}
}

func TestCredentialFileName_Collision(t *testing.T) {
	dir := t.TempDir()

	first, err := CredentialFileName(dir, "test-key")
	if err != nil {
		t.Fatal(err)
	}

	// Create the first file to simulate collision
	if err := os.WriteFile(filepath.Join(dir, first), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}

	second, err := CredentialFileName(dir, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Errorf("collision not avoided: both returned %s", first)
	}
	if second != "opencode-test-key-2.json" {
		t.Errorf("expected opencode-test-key-2.json, got %s", second)
	}
}

func TestCredentialFileName_SpecialChars(t *testing.T) {
	dir := t.TempDir()
	name, err := CredentialFileName(dir, "中文 Key!@#")
	if err != nil {
		t.Fatal(err)
	}
	// Non-ASCII chars should be replaced with dashes
	if filepath.Ext(name) != ".json" {
		t.Errorf("expected .json extension, got %s", name)
	}
}

func TestCredentialFileName_VeryLong(t *testing.T) {
	dir := t.TempDir()
	longLabel := ""
	for i := 0; i < 500; i++ {
		longLabel += "a"
	}
	name, err := CredentialFileName(dir, longLabel)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(name) != ".json" {
		t.Errorf("expected .json extension, got %s", name)
	}
}

func TestSanitizeFileSegment(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"Default API Key", "default-api-key"},
		{"", ""},
		{"test@email.com", "test-email-com"},
		{"hello world", "hello-world"},
		{"UPPER CASE", "upper-case"},
		{"混合中文key", "key"},
		{"a---b", "a-b"},
		{"  spaces  ", "spaces"},
	}
	for _, tc := range tests {
		got := sanitizeFileSegment(tc.input)
		if got != tc.expected {
			t.Errorf("sanitizeFileSegment(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
