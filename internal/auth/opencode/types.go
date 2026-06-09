package opencode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/misc"
	log "github.com/sirupsen/logrus"
)

// KeyEntry represents a discovered API key from OpenCode's workspace.
type KeyEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Key     string `json:"key"`
	Display string `json:"display"`
}

// TokenStorage stores an imported OpenCode API key on disk.
// It implements the baseauth.TokenStorage interface for integration with FileTokenStore.
type TokenStorage struct {
	Type      string `json:"type"`
	Label     string `json:"label"`
	Key       string `json:"key"`
	Workspace string `json:"workspace,omitempty"`
	Cookie    string `json:"cookie,omitempty"`

	Metadata map[string]any `json:"-"`
}

// SetMetadata allows external callers to inject metadata into the storage before saving.
func (ts *TokenStorage) SetMetadata(meta map[string]any) {
	ts.Metadata = meta
}

// SaveTokenToFile writes the OpenCode API key to a JSON auth file.
func (ts *TokenStorage) SaveTokenToFile(authFilePath string) error {
	misc.LogSavingCredentials(authFilePath)
	ts.Type = "opencode"
	if err := os.MkdirAll(filepath.Dir(authFilePath), 0o700); err != nil {
		return fmt.Errorf("opencode token storage: create directory: %w", err)
	}
	file, err := os.Create(authFilePath)
	if err != nil {
		return fmt.Errorf("opencode token storage: create token file: %w", err)
	}
	defer func() {
		if errClose := file.Close(); errClose != nil {
			log.Errorf("opencode token storage: close token file error: %v", errClose)
		}
	}()

	data, errMerge := misc.MergeMetadata(ts, ts.Metadata)
	if errMerge != nil {
		return fmt.Errorf("opencode token storage: merge metadata: %w", errMerge)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(data); err != nil {
		return fmt.Errorf("opencode token storage: write token file: %w", err)
	}
	return nil
}
