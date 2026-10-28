package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Claude Code's own Remote Control (claude.ai/code and the Claude app):
// which Claude tabs are connected. Claude Code lists the processes it has
// connected in ~/.claude.json (replBridgePlaceholders), whether Quarters
// started them with --remote-control or Claude turned it on by itself.

type rcCache struct {
	mu   sync.Mutex
	mod  time.Time
	size int64
	pids map[int]bool
}

var rc rcCache

// remotePids is the set of process ids Claude Code has connected.
func remotePids() map[int]bool {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".claude.json")
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if rc.pids != nil && st.ModTime().Equal(rc.mod) && st.Size() == rc.size {
		return rc.pids
	}
	var cfg struct {
		Bridges map[string]struct {
			Pid int `json:"pid"`
		} `json:"replBridgePlaceholders"`
	}
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &cfg) != nil {
		return rc.pids
	}
	pids := map[int]bool{}
	for _, b := range cfg.Bridges {
		if b.Pid > 0 {
			pids[b.Pid] = true
		}
	}
	rc.mod, rc.size, rc.pids = st.ModTime(), st.Size(), pids
	return pids
}

// RemoteControlled lists the session ids of Claude tabs connected to
// Claude Code's Remote Control.
func (a *App) RemoteControlled() []int {
	pids := remotePids()
	out := []int{}
	if len(pids) == 0 {
		return out
	}
	for _, info := range a.sessions.List() {
		if info.Exited {
			continue
		}
		if pid := a.sessions.Pid(info.ID); pid > 0 && pids[pid] {
			out = append(out, info.ID)
		}
	}
	return out
}
