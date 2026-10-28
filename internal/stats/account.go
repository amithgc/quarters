package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// Limit is one plan usage limit, as Claude Code's /usage shows it.
type Limit struct {
	Label    string  `json:"label"`
	Percent  float64 `json:"percent"`
	Severity string  `json:"severity"` // normal, warning, critical…
	ResetsAt int64   `json:"resetsAt"` // unix ms
	Active   bool    `json:"active"`   // the limit currently holding you back
}

// Account is the signed-in Claude account's plan usage.
type Account struct {
	Plan   string  `json:"plan"`
	Limits []Limit `json:"limits"`
	Error  string  `json:"error"`
}

// credentials is the login Claude Code keeps in the macOS Keychain.
type credentials struct {
	OAuth struct {
		AccessToken      string `json:"accessToken"`
		ExpiresAt        int64  `json:"expiresAt"`
		SubscriptionType string `json:"subscriptionType"`
	} `json:"claudeAiOauth"`
}

// FetchAccount reads Claude Code's login from the Keychain and asks
// Anthropic for the plan's usage. The token is never refreshed here (that
// would replace Claude Code's own login); when it has expired, opening
// Claude Code renews it.
func FetchAccount(ctx context.Context) Account {
	out, err := exec.CommandContext(ctx, "security", "find-generic-password", "-s", "Claude Code-credentials", "-w").Output()
	if err != nil {
		return Account{Error: "Not signed in — run claude and /login"}
	}
	var c credentials
	if json.Unmarshal(out, &c) != nil || c.OAuth.AccessToken == "" {
		return Account{Error: "Signed in with an API key — no plan limits"}
	}
	acct := Account{Plan: planName(c.OAuth.SubscriptionType)}
	if c.OAuth.ExpiresAt > 0 && time.Now().UnixMilli() > c.OAuth.ExpiresAt {
		acct.Error = "Login expired — open a Claude tab to renew it"
		return acct
	}
	limits, problem := fetchLimits(ctx, c.OAuth.AccessToken)
	if problem != "" {
		acct.Error = problem
		return acct
	}
	acct.Limits = limits
	return acct
}

func planName(s string) string {
	switch s {
	case "":
		return "Claude"
	case "max":
		return "Max"
	case "pro":
		return "Pro"
	default:
		return strings.ToUpper(s[:1]) + s[1:]
	}
}

// fetchLimits asks for the plan's limits. problem, when not "", says what
// went wrong, in words shown to the user as they are.
func fetchLimits(ctx context.Context, token string) (limits []Limit, problem string) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		return nil, "Usage unavailable (" + err.Error() + ")"
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", "quarters")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "Usage unavailable — offline?"
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, "Login expired — open a Claude tab to renew it"
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Sprintf("Usage unavailable (HTTP %d)", resp.StatusCode)
	}
	var body struct {
		Limits []struct {
			Kind     string  `json:"kind"`
			Percent  float64 `json:"percent"`
			Severity string  `json:"severity"`
			ResetsAt string  `json:"resets_at"`
			IsActive bool    `json:"is_active"`
			Scope    *struct {
				Model *struct {
					DisplayName string `json:"display_name"`
				} `json:"model"`
			} `json:"scope"`
		} `json:"limits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, "Usage unavailable (unexpected reply)"
	}
	var out []Limit
	for _, l := range body.Limits {
		label := map[string]string{"session": "Current session", "weekly_all": "Weekly · all models"}[l.Kind]
		if label == "" {
			label = strings.ReplaceAll(l.Kind, "_", " ")
			if l.Scope != nil && l.Scope.Model != nil && l.Scope.Model.DisplayName != "" {
				label = "Weekly · " + l.Scope.Model.DisplayName
			}
		}
		lim := Limit{Label: label, Percent: l.Percent, Severity: l.Severity, Active: l.IsActive}
		if t, err := time.Parse(time.RFC3339Nano, l.ResetsAt); err == nil {
			lim.ResetsAt = t.UnixMilli()
		}
		out = append(out, lim)
	}
	return out, ""
}
