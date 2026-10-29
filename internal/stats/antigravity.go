package stats

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Antigravity keeps no plan or quota on disk: it asks Google each time. The
// Antigravity IDE runs a local helper (its "language server") that answers
// with the plan and each model's quota, so Quarters asks that, on this Mac
// only. The helper wants the token it was started with, which is on its
// command line; it is sent nowhere but to the helper on 127.0.0.1. This is
// not a public interface, so an Antigravity update may change it.

// AntigravityClosed is the error while the Antigravity IDE isn't running.
const AntigravityClosed = "Open the Antigravity IDE to see its plan and quotas here."

var (
	csrfArg   = regexp.MustCompile(`--csrf_token[= ]([A-Za-z0-9-]+)`)
	listenRow = regexp.MustCompile(`:(\d+) \(LISTEN\)`)
	// a model's effort level, e.g. " (Medium)"
	effortSuffix = regexp.MustCompile(`\s*\((Low|Medium|High|Max|Thinking)\)$`)
)

// FetchAntigravity reads the plan and quotas from the running Antigravity IDE.
func FetchAntigravity(ctx context.Context) Account {
	pid, token := antigravityHelper(ctx)
	if pid == "" {
		return Account{Error: AntigravityClosed}
	}
	out, _ := exec.CommandContext(ctx, "lsof", "-nP", "-a", "-p", pid, "-iTCP", "-sTCP:LISTEN").Output()
	for _, m := range listenRow.FindAllStringSubmatch(string(out), -1) {
		if a, err := antigravityStatus(ctx, m[1], token); err == nil {
			return a
		}
	}
	return Account{Error: "Antigravity is open but didn't answer; its plan shows in Antigravity."}
}

// antigravityHelper finds the IDE's language server and its token.
func antigravityHelper(ctx context.Context) (pid, token string) {
	out, err := exec.CommandContext(ctx, "ps", "-axww", "-o", "pid=,args=").Output()
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "language_server") || !strings.Contains(strings.ToLower(line), "antigravity") {
			continue
		}
		if m := csrfArg.FindStringSubmatch(line); m != nil {
			pid, _, _ = strings.Cut(line, " ")
			return pid, m[1]
		}
	}
	return "", ""
}

// antigravityStatus asks the helper on port for the signed-in user's status.
func antigravityStatus(ctx context.Context, port, token string) (Account, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	body := []byte(`{"metadata":{"ideName":"antigravity","extensionName":"antigravity","locale":"en"}}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://127.0.0.1:"+port+"/exa.language_server_pb.LanguageServerService/GetUserStatus", bytes.NewReader(body))
	if err != nil {
		return Account{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("X-Codeium-Csrf-Token", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Account{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Account{}, fmt.Errorf("status %d", resp.StatusCode)
	}
	var st userStatus
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return Account{}, err
	}
	return st.account(), nil
}

// userStatus is the part of GetUserStatus's answer Quarters shows.
type userStatus struct {
	UserStatus struct {
		PlanStatus struct {
			PlanInfo struct {
				PlanName string `json:"planName"`
			} `json:"planInfo"`
		} `json:"planStatus"`
		CascadeModelConfigData struct {
			ClientModelConfigs []struct {
				Label     string `json:"label"`
				QuotaInfo *struct {
					RemainingFraction *float64 `json:"remainingFraction"`
					ResetTime         string   `json:"resetTime"`
				} `json:"quotaInfo"`
			} `json:"clientModelConfigs"`
		} `json:"cascadeModelConfigData"`
	} `json:"userStatus"`
}

// account turns the status into the same shape as Claude's plan: each
// model's quota as a limit, percent used.
func (s userStatus) account() Account {
	a := Account{Plan: s.UserStatus.PlanStatus.PlanInfo.PlanName, Limits: []Limit{}}
	seen := map[string]int{}
	for _, m := range s.UserStatus.CascadeModelConfigData.ClientModelConfigs {
		q := m.QuotaInfo
		if q == nil || m.Label == "" {
			continue
		}
		left := 1.0 // no fraction: nothing used
		if q.RemainingFraction != nil {
			left = *q.RemainingFraction
		}
		// one row per model: "Opus (Low)", "(Medium)" and "(High)" are one
		label := effortSuffix.ReplaceAllString(m.Label, "")
		l := Limit{Label: label, Percent: max(0, min(100, 100*(1-left))), Severity: "normal"}
		if t, err := time.Parse(time.RFC3339, q.ResetTime); err == nil {
			l.ResetsAt = t.UnixMilli()
		}
		if i, ok := seen[label]; ok {
			if l.Percent > a.Limits[i].Percent {
				a.Limits[i] = l
			}
			continue
		}
		seen[label] = len(a.Limits)
		a.Limits = append(a.Limits, l)
	}
	// the most used first
	sort.SliceStable(a.Limits, func(i, j int) bool { return a.Limits[i].Percent > a.Limits[j].Percent })
	if len(a.Limits) == 0 {
		a.Error = "Antigravity didn't report any quotas."
	}
	return a
}
