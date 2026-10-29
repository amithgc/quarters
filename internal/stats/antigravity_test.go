package stats

import (
	"encoding/json"
	"testing"
)

func TestAntigravityStatus(t *testing.T) {
	in := `{"userStatus": {"planStatus": {"planInfo": {"planName": "Pro"}},
	  "cascadeModelConfigData": {"clientModelConfigs": [
	    {"label": "Claude Opus 5.5 (Low)", "quotaInfo": {"remainingFraction": 0.9, "resetTime": "2026-10-29T08:57:05Z"}},
	    {"label": "Claude Opus 5.5 (High)", "quotaInfo": {"remainingFraction": 0.4, "resetTime": "2026-10-29T08:57:05Z"}},
	    {"label": "Gemini 3.8 Flash (Medium)", "quotaInfo": {"resetTime": "2026-10-29T08:57:05Z"}},
	    {"label": "No quota here"}
	  ]}}}`
	var st userStatus
	if err := json.Unmarshal([]byte(in), &st); err != nil {
		t.Fatal(err)
	}
	a := st.account()
	if a.Plan != "Pro" || a.Error != "" || len(a.Limits) != 2 {
		t.Fatalf("account = %+v", a)
	}
	// Opus's effort levels are one row, the most used; the most used first
	if l := a.Limits[0]; l.Label != "Claude Opus 5.5" || int(l.Percent+0.5) != 60 || l.ResetsAt != 1793264225000 {
		t.Errorf("opus = %+v", l)
	}
	// no remaining fraction: nothing used
	if l := a.Limits[1]; l.Label != "Gemini 3.8 Flash" || l.Percent != 0 {
		t.Errorf("gemini = %+v", l)
	}
	if a := (userStatus{}).account(); a.Error == "" {
		t.Error("no quotas should say so")
	}
}
