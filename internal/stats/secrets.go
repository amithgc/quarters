package stats

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ---- keys and passwords that ended up in a Claude conversation ----

// SecretHit is a credential found in a Claude Code log. Only a masked form
// of it is kept.
type SecretHit struct {
	Kind   string    // "GitHub token"
	Masked string    // "ghp_…a1b2"
	Where  string    // "a tool's output", "a prompt", "Claude's message"
	File   string    // the log
	Cwd    string    // the folder the conversation ran in
	At     time.Time // when it was found
}

// secretRule is a kind of credential: a pattern exact enough that a match is
// almost always the real thing, and a cheap string to look for first.
type secretRule struct {
	kind string
	hint string
	re   *regexp.Regexp
	val  int // the submatch that is the secret (0: all of it)
}

var secretRules = []secretRule{
	{"AWS access key", "AKIA", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`), 0},
	{"AWS access key", "ASIA", regexp.MustCompile(`\bASIA[0-9A-Z]{16}\b`), 0},
	{"GitHub token", "gh", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,255}\b`), 0},
	{"GitHub token", "github_pat_", regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{60,255}\b`), 0},
	{"GitLab token", "glpat-", regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}\b`), 0},
	{"Anthropic API key", "sk-ant-", regexp.MustCompile(`\bsk-ant-(?:api|admin)\d\d-[A-Za-z0-9_-]{80,}`), 0},
	{"OpenAI API key", "sk-", regexp.MustCompile(`\bsk-(?:proj|svcacct|admin)-[A-Za-z0-9_-]{40,}`), 0},
	{"OpenAI API key", "T3BlbkFJ", regexp.MustCompile(`\bsk-[A-Za-z0-9]{20}T3BlbkFJ[A-Za-z0-9]{20}\b`), 0},
	{"Stripe live key", "_live_", regexp.MustCompile(`\b(?:sk|rk)_live_[A-Za-z0-9]{24,}\b`), 0},
	{"Stripe webhook secret", "whsec_", regexp.MustCompile(`\bwhsec_[A-Za-z0-9+/]{32,}`), 0},
	{"Slack token", "xox", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{20,}\b`), 0},
	{"Slack webhook", "hooks.slack.com", regexp.MustCompile(`https://hooks\.slack\.com/services/T[A-Z0-9]+/B[A-Z0-9]+/[A-Za-z0-9]{20,}`), 0},
	{"Google API key", "AIza", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`), 0},
	{"npm token", "npm_", regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}\b`), 0},
	{"PyPI token", "pypi-", regexp.MustCompile(`\bpypi-AgEIcHlwaS5vcmc[A-Za-z0-9_-]{50,}`), 0},
	{"SendGrid key", "SG.", regexp.MustCompile(`\bSG\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}\b`), 0},
	{"Hugging Face token", "hf_", regexp.MustCompile(`\bhf_[A-Za-z]{34}\b`), 0},
	{"DigitalOcean token", "dop_v1_", regexp.MustCompile(`\bdop_v1_[a-f0-9]{64}\b`), 0},
	{"Shopify token", "shp", regexp.MustCompile(`\bshp(?:at|ss|ca|pa)_[a-fA-F0-9]{32}\b`), 0},
	{"Linear API key", "lin_api_", regexp.MustCompile(`\blin_api_[A-Za-z0-9]{40}\b`), 0},
	// a key's text, not the line that names it ("BEGIN … PRIVATE KEY"
	// alone is in plenty of code); in a log, line breaks are written \n
	{"Private key", "PRIVATE KEY", regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH |ENCRYPTED |PGP )?PRIVATE KEY(?: BLOCK)?-----(?:\\[rn]|\s)+([A-Za-z0-9+/=]{40,})`), 1},
	// a database address with its password, unless it's a local one
	{"Database password", "://", regexp.MustCompile(`\b(?:postgres(?:ql)?|mysql|mariadb|mongodb(?:\+srv)?|rediss?|amqps?)://[^\s:/@"'\\]+:([^\s:/@"'\\]{6,})@([^\s/:"'\\]+)`), 1},
}

// localHosts are database hosts whose password is a development one.
var localHosts = map[string]bool{"localhost": true, "127.0.0.1": true, "0.0.0.0": true, "host.docker.internal": true, "db": true, "postgres": true, "mysql": true, "redis": true, "mongo": true}

// placeholders mark an example, not a real secret.
var placeholders = []string{"example", "xxxx", "your", "dummy", "fake", "placeholder", "redacted", "sample", "test", "changeme", "password", "secret", "1234", "abcd", "<", "${", "*"}

// plausible tells a random-looking secret from a placeholder.
func plausible(v string) bool {
	low := strings.ToLower(v)
	for _, p := range placeholders {
		if strings.Contains(low, p) {
			return false
		}
	}
	if len(v) < 16 {
		return true // a password: short, and not judged by randomness
	}
	return entropy(v) >= 3.0
}

// entropy is the Shannon entropy of s in bits per character.
func entropy(s string) float64 {
	n := map[rune]int{}
	for _, r := range s {
		n[r]++
	}
	e := 0.0
	for _, c := range n {
		p := float64(c) / float64(len(s))
		e -= p * math.Log2(p)
	}
	return e
}

// mask keeps enough of a secret to recognise it, and no more.
func mask(rule secretRule, whole, val string) string {
	switch rule.kind {
	case "Private key":
		return "-----BEGIN … PRIVATE KEY-----"
	case "Database password":
		i := strings.Index(whole, val)
		return whole[:i] + "••••" + whole[i+len(val):]
	}
	if len(val) <= 12 {
		return val[:2] + "…"
	}
	head := 4
	for _, p := range []string{"github_pat_", "sk-ant-api03-", "sk-proj-", "glpat-", "xoxb-", "xoxp-", "ghp_", "gho_", "ghs_", "ghu_", "ghr_", "npm_", "hf_", "sk_live_", "rk_live_", "whsec_", "dop_v1_", "lin_api_"} {
		if strings.HasPrefix(val, p) {
			head = len(p)
			break
		}
	}
	if strings.HasPrefix(val, "https://hooks.slack.com/") {
		head = len("https://hooks.slack.com/services/")
	}
	return val[:head] + "…" + val[len(val)-4:]
}

// FindSecrets lists the credentials in a piece of text (a log line): the
// rule's kind, the masked form, and a hash to recognise it again.
func FindSecrets(line []byte) (kinds, masked, hashes []string) {
	for _, rule := range secretRules {
		if !bytes.Contains(line, []byte(rule.hint)) {
			continue
		}
		for _, m := range rule.re.FindAllSubmatch(line, 20) {
			whole, val := string(m[0]), string(m[rule.val])
			if rule.kind == "Database password" && localHosts[strings.ToLower(string(m[2]))] {
				continue
			}
			if !plausible(val) {
				continue
			}
			sum := sha256.Sum256([]byte(val))
			kinds = append(kinds, rule.kind)
			masked = append(masked, mask(rule, whole, val))
			hashes = append(hashes, hex.EncodeToString(sum[:8]))
		}
	}
	return
}

// SecretScanner reads what is added to Claude Code's logs for credentials.
type SecretScanner struct {
	mu      sync.Mutex
	offsets map[string]int64 // log → bytes already read
	seen    map[string]bool  // secrets already reported (hashes)
	primed  bool
}

// NewSecretScanner returns a scanner that has read nothing yet.
func NewSecretScanner() *SecretScanner {
	return &SecretScanner{offsets: map[string]int64{}, seen: map[string]bool{}}
}

// maxHits is how many secrets one scan reports at most.
const maxHits = 20

// Scan reads what was added to the logs under root (Claude Code's projects
// folder) since the last scan, and returns the secrets in it not reported
// before. The first scan only notes where each log ends: secrets from
// before Quarters started aren't reported.
func (s *SecretScanner) Scan(root string, now time.Time) []SecretHit {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []SecretHit
	seen := map[string]bool{}
	dirs, _ := os.ReadDir(root)
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		dir := filepath.Join(root, d.Name())
		_ = filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if e.IsDir() {
				if path != dir && strings.Count(strings.TrimPrefix(path, dir), string(filepath.Separator)) > 2 {
					return filepath.SkipDir // subagents' logs are two levels down
				}
				return nil
			}
			if !strings.HasSuffix(path, ".jsonl") {
				return nil
			}
			info, err := e.Info()
			if err != nil {
				return nil
			}
			seen[path] = true
			off, known := s.offsets[path]
			switch {
			case !s.primed:
				s.offsets[path] = info.Size()
			case info.Size() < off: // rewritten: read it again
				s.offsets[path] = 0
				fallthrough
			case !known || info.Size() > off:
				if len(out) < maxHits {
					out = append(out, s.read(path, now)...)
				}
			}
			return nil
		})
	}
	for path := range s.offsets {
		if !seen[path] {
			delete(s.offsets, path)
		}
	}
	s.primed = true
	if len(out) > maxHits {
		out = out[:maxHits]
	}
	return out
}

// read scans the new lines of one log.
func (s *SecretScanner) read(path string, now time.Time) []SecretHit {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	off := s.offsets[path]
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil
	}
	var out []SecretHit
	br := bufio.NewReaderSize(f, 256*1024)
	for {
		line, err := br.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			rest, err2 := br.ReadBytes('\n')
			if err2 != nil {
				break
			}
			line = append(append([]byte(nil), line...), rest...)
		} else if err != nil {
			break // a partial last line is read again next time
		}
		off += int64(len(line))
		kinds, masked, hashes := FindSecrets(line)
		for i := range kinds {
			if s.seen[hashes[i]] {
				continue
			}
			s.seen[hashes[i]] = true
			where, cwd := lineContext(line)
			out = append(out, SecretHit{Kind: kinds[i], Masked: masked[i], Where: where, File: path, Cwd: cwd, At: now})
		}
	}
	s.offsets[path] = off
	return out
}

// lineContext says where in a conversation a log line is, and the folder
// the conversation ran in.
func lineContext(line []byte) (where, cwd string) {
	var l struct {
		Type    string `json:"type"`
		Cwd     string `json:"cwd"`
		Message *struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &l) != nil {
		return "the conversation", ""
	}
	switch {
	case l.Type == "user" && l.Message != nil && isTypedPrompt(l.Message.Content):
		return "a prompt", l.Cwd
	case l.Type == "user":
		return "a tool's output", l.Cwd
	case l.Type == "assistant":
		return "Claude's message or a command it ran", l.Cwd
	}
	return "the conversation", l.Cwd
}
