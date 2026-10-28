package stats

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// maxImageLogs is how many session logs are searched for a pasted image.
const maxImageLogs = 8

// FindImage returns the image pasted as "[Image #n]" in a session log in dir,
// as a data: URL. Logs written since `since` (the tab's start) are searched
// first, newest first, so a tab finds its own conversation's image.
func FindImage(dir string, n int, since time.Time) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	type log struct {
		path string
		mod  time.Time
	}
	var logs []log
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		if info, err := e.Info(); err == nil {
			logs = append(logs, log{filepath.Join(dir, e.Name()), info.ModTime()})
		}
	}
	sort.Slice(logs, func(i, j int) bool {
		ri, rj := !logs[i].mod.Before(since), !logs[j].mod.Before(since)
		if ri != rj {
			return ri
		}
		return logs[i].mod.After(logs[j].mod)
	})
	for i, l := range logs {
		if i == maxImageLogs {
			break
		}
		if url := imageInLog(l.path, n); url != "" {
			return url, nil
		}
	}
	return "", fmt.Errorf("image #%d is not in this conversation's log", n)
}

var pasteKey = []byte(`"imagePasteIds"`)

// imageInLog finds the last message in a log that pasted image #n. Each
// entry of imagePasteIds belongs to the image block at the same position.
func imageInLog(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	var found string
	for {
		line, err := r.ReadBytes('\n')
		if bytes.Contains(line, pasteKey) {
			if url := imageInLine(line, n); url != "" {
				found = url
			}
		}
		if err != nil {
			return found
		}
	}
}

func imageInLine(line []byte, n int) string {
	var e struct {
		IDs     []int `json:"imagePasteIds"`
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &e) != nil {
		return ""
	}
	var blocks []struct {
		Type   string `json:"type"`
		Source struct {
			Type      string `json:"type"`
			MediaType string `json:"media_type"`
			Data      string `json:"data"`
		} `json:"source"`
	}
	if json.Unmarshal(e.Message.Content, &blocks) != nil {
		return ""
	}
	k := 0
	for _, b := range blocks {
		if b.Type != "image" {
			continue
		}
		if k < len(e.IDs) && e.IDs[k] == n && b.Source.Type == "base64" && strings.HasPrefix(b.Source.MediaType, "image/") {
			return "data:" + b.Source.MediaType + ";base64," + b.Source.Data
		}
		k++
	}
	return ""
}
