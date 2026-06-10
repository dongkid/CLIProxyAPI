package opencode

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CredentialFileName generates a collision-free filename for an OpenCode key.
// The wspSuffix is an optional short workspace identifier to disambiguate files
// from different workspaces (e.g., "wrk01"). Pass "" to skip.
// Returns "opencode-<sanitized_label>[-<wspSuffix>].json" or "opencode-<timestamp>.json" if empty.
// If the target file already exists, appends a counter: "opencode-<label>-2.json".
func CredentialFileName(baseDir, label, wspSuffix string) (string, error) {
	name := sanitizeFileSegment(label)
	if name == "" {
		name = fmt.Sprintf("%d", time.Now().UnixMilli())
	}
	wspSuffix = sanitizeFileSegment(wspSuffix)
	var fileName string
	if wspSuffix != "" {
		fileName = fmt.Sprintf("opencode-%s-%s.json", name, wspSuffix)
	} else {
		fileName = fmt.Sprintf("opencode-%s.json", name)
	}
	fullPath := filepath.Join(baseDir, fileName)

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return fileName, nil
	}
	for i := 2; i <= 99; i++ {
		var candidate string
		if wspSuffix != "" {
			candidate = fmt.Sprintf("opencode-%s-%s-%d.json", name, wspSuffix, i)
		} else {
			candidate = fmt.Sprintf("opencode-%s-%d.json", name, i)
		}
		if _, err := os.Stat(filepath.Join(baseDir, candidate)); os.IsNotExist(err) {
			return candidate, nil
		}
	}
	return fmt.Sprintf("opencode-%d.json", time.Now().UnixMilli()), nil
}

func sanitizeFileSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	result := strings.Trim(b.String(), "-")
	// Collapse consecutive dashes
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	return result
}
