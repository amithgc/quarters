//go:build !darwin

package stats

import (
	"context"
	"os"
	"path/filepath"
)

// claudeCredentials is the login Claude Code keeps in
// ~/.claude/.credentials.json (or $CLAUDE_CONFIG_DIR) outside macOS.
func claudeCredentials(context.Context) ([]byte, error) {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(home, ".claude")
	}
	return os.ReadFile(filepath.Join(dir, ".credentials.json"))
}
