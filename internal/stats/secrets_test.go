package stats

import (
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Fake credentials are made up when the tests run, so no token-shaped
// string sits in the source for a scanner (this one included) to find.

const (
	alnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	upper = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	hexs  = "0123456789abcdef"
)

func fake(r *rand.Rand, n int, alphabet string) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[r.Intn(len(alphabet))]
	}
	return string(b)
}

func TestFindSecrets(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	gh := "gh" + "p_" + fake(r, 36, alnum)
	aws := "AK" + "IA" + fake(r, 16, upper)
	stripe := "sk" + "_live_" + fake(r, 30, alnum)
	do := "dop" + "_v1_" + fake(r, 64, hexs)
	key := "-----BEGIN RSA " + "PRIVATE KEY-----\\n" + fake(r, 64, alnum)
	db := "postgres://app:" + fake(r, 18, alnum) + "@db.internal.example.net:5432/app"

	for _, c := range []struct {
		line, kind, masked string
	}{
		{`{"content":"token ` + gh + `"}`, "GitHub token", gh[:4] + "…" + gh[len(gh)-4:]},
		{`{"content":"` + aws + `"}`, "AWS access key", aws[:4] + "…" + aws[len(aws)-4:]},
		{`{"content":"` + stripe + `"}`, "Stripe live key", stripe[:8] + "…" + stripe[len(stripe)-4:]},
		{`{"content":"` + do + `"}`, "DigitalOcean token", do[:7] + "…" + do[len(do)-4:]},
		{`{"content":"` + key + `"}`, "Private key", "-----BEGIN … PRIVATE KEY-----"},
		{`{"content":"DATABASE_URL=` + db + `"}`, "Database password", "postgres://app:••••@db.internal.example.net"},
	} {
		kinds, masked, hashes := FindSecrets([]byte(c.line))
		if len(kinds) != 1 || kinds[0] != c.kind || masked[0] != c.masked || len(hashes[0]) != 16 {
			t.Errorf("%s: got %v %v", c.kind, kinds, masked)
		}
	}

	for _, line := range []string{
		`the docs' example key ` + "AK" + "IA" + "IOSFODNN7" + "EXAMPLE",
		"gh" + "p_" + strings.Repeat("x", 36), // a placeholder
		"gh" + "p_" + strings.Repeat("a", 36), // not random
		`if strings.Contains(s, "-----BEGIN ` + `PRIVATE KEY-----") {`,
		"postgres://postgres:" + fake(r, 12, alnum) + "@localhost:5432/dev", // a local database
		"postgres://app:" + "${DB_PASSWORD}" + "@db.prod/app",
		"sk" + "_test_" + fake(r, 30, alnum), // a Stripe test key
	} {
		if kinds, masked, _ := FindSecrets([]byte(line)); len(kinds) != 0 {
			t.Errorf("%q: found %v %v", line, kinds, masked)
		}
	}
}

func TestSecretScanner(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	root := t.TempDir()
	dir := filepath.Join(root, "-Users-me-code-shop")
	if err := os.MkdirAll(filepath.Join(dir, "conv", "subagents"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := "gh" + "p_" + fake(r, 36, alnum)
	logPath := filepath.Join(dir, "conv.jsonl")
	line := func(typ, content, token string) string {
		return `{"type":"` + typ + `","cwd":"/Users/me/code/shop","message":{"role":"user","content":` + content + `},"x":"` + token + `"}` + "\n"
	}
	write := func(path, s string) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.WriteString(s); err != nil {
			t.Fatal(err)
		}
	}
	write(logPath, line("user", `"hi"`, old))

	s := NewSecretScanner()
	now := time.Now()
	if hits := s.Scan(root, now); len(hits) != 0 {
		t.Fatalf("what was there before the first scan was reported: %+v", hits)
	}

	tok := "gh" + "p_" + fake(r, 36, alnum)
	write(logPath, line("user", `[{"type":"tool_result","content":"x"}]`, tok))
	write(logPath, line("user", `[{"type":"tool_result","content":"x"}]`, tok)) // the same secret again
	sub := filepath.Join(dir, "conv", "subagents", "agent-1.jsonl")
	aws := "AS" + "IA" + fake(r, 16, upper)
	write(sub, line("user", `"use `+aws+`"`, ""))

	hits := s.Scan(root, now)
	if len(hits) != 2 {
		t.Fatalf("hits = %+v, want 2", hits)
	}
	if hits[0].Kind != "GitHub token" { // the order logs are walked in doesn't matter
		hits[0], hits[1] = hits[1], hits[0]
	}
	if hits[0].Kind != "GitHub token" || hits[0].Where != "a tool's output" || hits[0].Cwd != "/Users/me/code/shop" || hits[0].File != logPath {
		t.Errorf("first hit = %+v", hits[0])
	}
	if hits[1].Kind != "AWS access key" || hits[1].Where != "a prompt" {
		t.Errorf("second hit = %+v", hits[1])
	}
	if strings.Contains(hits[0].Masked, tok[8:30]) {
		t.Errorf("masked form %q shows too much", hits[0].Masked)
	}
	if hits := s.Scan(root, now); len(hits) != 0 {
		t.Errorf("nothing new, yet %+v", hits)
	}
	// a partial last line is read once it's complete
	tok2 := "gh" + "p_" + fake(r, 36, alnum)
	full := line("assistant", `"ok"`, tok2)
	write(logPath, full[:40])
	if hits := s.Scan(root, now); len(hits) != 0 {
		t.Errorf("a partial line was read: %+v", hits)
	}
	write(logPath, full[40:])
	if hits := s.Scan(root, now); len(hits) != 1 || hits[0].Where != "Claude's message or a command it ran" {
		t.Errorf("hits = %+v", hits)
	}
}
