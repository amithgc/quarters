package stats

import (
	"context"
	"os/exec"
)

// claudeCredentials is the login Claude Code keeps in the macOS Keychain.
func claudeCredentials(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, "security", "find-generic-password", "-s", "Claude Code-credentials", "-w").Output()
}
