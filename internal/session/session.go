// Package session runs programs (Claude Code or a shell) in pseudo
// terminals and streams their output to the window, where xterm.js draws it.
package session

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// Output is a chunk of a session's output, base64-encoded. Off is how many
// bytes the session had written before this chunk, so a window that joined
// late (with a Snapshot) can skip what it already has.
type Output struct {
	ID   int    `json:"id"`
	Data string `json:"data"`
	Off  int64  `json:"off"`
}

// Info describes a running session to a window that did not start it.
// Meta is what the starting window attached (project, kind, title, …).
type Info struct {
	ID     int             `json:"id"`
	Meta   json.RawMessage `json:"meta"`
	Exited bool            `json:"exited"`
	Code   int             `json:"code"`
	Owner  string          `json:"owner"`
	Cols   int             `json:"cols"` // the terminal's size, set by the owner
	Rows   int             `json:"rows"`
}

// Snap is the recent output of a session, for a window that joins late.
type Snap struct {
	Data string `json:"data"` // base64
	Off  int64  `json:"off"`  // total bytes written; later chunks start here
}

// Owner says which window now drives a session's terminal size.
// Other windows draw the terminal at its size, so output that moves the
// cursor lands where the program meant it to.
type Owner struct {
	ID    int    `json:"id"`
	Owner string `json:"owner"`
	Cols  int    `json:"cols"`
	Rows  int    `json:"rows"`
}

// keepBytes is how much recent output each session keeps for late joiners.
const keepBytes = 1 << 20

// Exit tells the window a session's program ended.
type Exit struct {
	ID   int `json:"id"`
	Code int `json:"code"`
}

// Emitter sends events to the window.
type Emitter func(event string, data any)

// Manager owns all sessions.
type Manager struct {
	emit Emitter

	mu       sync.Mutex
	sessions map[int]*session
	nextID   int

	envOnce sync.Once
	env     []string

	// onOutput (optional) sees every chunk of output.
	onOutput func(id int, data []byte)
}

type session struct {
	id     int
	cmd    *exec.Cmd
	pty    *os.File
	exited chan struct{}
	meta   json.RawMessage

	flushMu sync.Mutex // keeps chunks in order
	mu      sync.Mutex
	pending bytes.Buffer
	recent  []byte // the last keepBytes of output
	total   int64
	code    int
	owner   string // window (client) whose size the terminal has
	cols    int
	rows    int

	// "Claude finished" tracking: output that is not the echo of typing
	// counts as work.
	lastInput time.Time
	workStart time.Time
	workLast  time.Time
}

// NewManager starts the output pump. Output is batched and sent at most
// every 8 ms per session so a busy program does not flood the window.
// onOutput, when not nil, also sees every chunk of output (the app looks
// for links to dev servers in it).
func NewManager(emit Emitter, onOutput func(id int, data []byte)) *Manager {
	m := &Manager{emit: emit, onOutput: onOutput, sessions: map[int]*session{}}
	go m.pump()
	go m.loginEnv() // warm up: reading the login shell's environment takes a moment
	return m
}

func (m *Manager) pump() {
	t := time.NewTicker(8 * time.Millisecond)
	defer t.Stop()
	for range t.C {
		m.mu.Lock()
		list := make([]*session, 0, len(m.sessions))
		for _, s := range m.sessions {
			list = append(list, s)
		}
		m.mu.Unlock()
		for _, s := range list {
			s.flush(m.emit, m.onOutput)
		}
	}
}

func (s *session) flush(emit Emitter, watch func(int, []byte)) {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	s.mu.Lock()
	if s.pending.Len() == 0 {
		s.mu.Unlock()
		return
	}
	chunk := s.pending.Bytes()
	data := base64.StdEncoding.EncodeToString(chunk)
	off := s.total
	s.total += int64(len(chunk))
	s.recent = append(s.recent, chunk...)
	if over := len(s.recent) - keepBytes; over > 0 {
		cut := over
		// start the kept output on a fresh line, not mid escape sequence
		if i := bytes.IndexByte(s.recent[over:], '\n'); i >= 0 && i < 4096 {
			cut = over + i + 1
		}
		s.recent = append([]byte(nil), s.recent[cut:]...)
	}
	var copied []byte
	if watch != nil {
		copied = append(copied, chunk...)
	}
	s.pending.Reset()
	if now := time.Now(); now.Sub(s.lastInput) > 250*time.Millisecond {
		if s.workStart.IsZero() || now.Sub(s.workLast) > quietFor {
			s.workStart = now
		}
		s.workLast = now
	}
	s.mu.Unlock()
	emit("pty:data", Output{ID: s.id, Data: data, Off: off})
	if watch != nil {
		watch(s.id, copied)
	}
}

// typeAfterQuiet is how long a new shell must stay quiet (its prompt drawn)
// before a start-up command is typed into it; typeDeadline is the longest wait.
const (
	typeAfterQuiet = 400 * time.Millisecond
	typeDeadline   = 6 * time.Second
)

// Start runs argv in dir on a cols×rows terminal and returns the session id.
// When input is set it is typed in (followed by Enter) once the program has
// started up and gone quiet, as if the user typed it at the prompt.
// meta is passed on to other windows; owner is the window starting it.
func (m *Manager) Start(dir string, argv []string, cols, rows int, input string, meta json.RawMessage, owner string) (int, error) {
	if len(argv) == 0 {
		return 0, errors.New("no command")
	}
	env := m.loginEnv()
	path := argv[0]
	if !strings.Contains(path, "/") {
		found, err := lookPath(path, env)
		if err != nil {
			return 0, fmt.Errorf("%s was not found on your PATH", path)
		}
		path = found
	}
	cmd := exec.Command(path, argv[1:]...)
	cmd.Dir = dir
	cmd.Env = env
	cols, rows = max(cols, 10), max(rows, 3)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return 0, err
	}

	m.mu.Lock()
	m.nextID++
	s := &session{id: m.nextID, cmd: cmd, pty: f, exited: make(chan struct{}), meta: meta, owner: owner, cols: cols, rows: rows}
	m.sessions[s.id] = s
	m.mu.Unlock()
	m.emit("session:open", s.info())

	var typeOnce sync.Once
	var quiet *time.Timer
	typeIt := func() {
		typeOnce.Do(func() {
			if !s.done() {
				_, _ = f.Write([]byte(input + "\r"))
			}
		})
	}
	if input != "" {
		time.AfterFunc(typeDeadline, typeIt)
	}

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 64*1024)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				s.mu.Lock()
				s.pending.Write(buf[:n])
				s.mu.Unlock()
				if input != "" {
					if quiet == nil {
						quiet = time.AfterFunc(typeAfterQuiet, typeIt)
					} else {
						quiet.Reset(typeAfterQuiet)
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		err := cmd.Wait()
		close(s.exited)
		code := 0
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}
		s.mu.Lock()
		s.code = code
		s.mu.Unlock()
		select { // let the last output arrive before saying it ended
		case <-readDone:
		case <-time.After(300 * time.Millisecond):
		}
		s.flush(m.emit, m.onOutput)
		m.emit("pty:exit", Exit{ID: s.id, Code: code})
	}()
	return s.id, nil
}

// Pid is the process id of a session's program (0 when it has ended).
func (m *Manager) Pid(id int) int {
	s := m.get(id)
	if s == nil || s.done() || s.cmd.Process == nil {
		return 0
	}
	return s.cmd.Process.Pid
}

func (m *Manager) get(id int) *session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

func (s *session) info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Info{ID: s.id, Meta: s.meta, Exited: s.done(), Code: s.code, Owner: s.owner, Cols: s.cols, Rows: s.rows}
}

// Write sends text typed in window client to a session. Typing makes that
// window the session's owner: the terminal takes its size.
func (m *Manager) Write(client string, id int, data string) {
	if s := m.get(id); s != nil {
		m.claim(s, client)
		s.mu.Lock()
		s.lastInput = time.Now()
		s.mu.Unlock()
		_, _ = s.pty.Write([]byte(data))
	}
}

// Resize changes a session's terminal size, if client owns the session.
// Windows of different sizes share a session; the one in use sets the size.
func (m *Manager) Resize(client string, id, cols, rows int) {
	s := m.get(id)
	if s == nil || cols <= 0 || rows <= 0 {
		return
	}
	s.mu.Lock()
	mine := s.owner == "" || s.owner == client
	changed := mine && (s.cols != cols || s.rows != rows)
	if changed {
		s.cols, s.rows = cols, rows
	}
	owner := s.owner
	s.mu.Unlock()
	if mine {
		_ = pty.Setsize(s.pty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	}
	if changed {
		m.emit("pty:owner", Owner{ID: s.id, Owner: owner, Cols: cols, Rows: rows})
	}
}

// Claim makes client the owner of a session and gives it client's size.
// The program always redraws (the new owner clears its screen for that):
// at an unchanged size, it is nudged with a one-row-smaller size first.
func (m *Manager) Claim(client string, id, cols, rows int) {
	s := m.get(id)
	if s == nil || cols <= 0 || rows <= 0 {
		return
	}
	m.claim(s, client)
	if cur, err := pty.GetsizeFull(s.pty); err == nil && int(cur.Cols) == cols && int(cur.Rows) == rows && rows > 3 {
		_ = pty.Setsize(s.pty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows - 1)})
	}
	m.Resize(client, id, cols, rows)
}

func (m *Manager) claim(s *session, client string) {
	s.mu.Lock()
	changed := s.owner != client
	s.owner = client
	cols, rows := s.cols, s.rows
	s.mu.Unlock()
	if changed {
		m.emit("pty:owner", Owner{ID: s.id, Owner: client, Cols: cols, Rows: rows})
	}
}

// Snapshot returns a session's recent output.
func (m *Manager) Snapshot(id int) (Snap, error) {
	s := m.get(id)
	if s == nil {
		return Snap{}, fmt.Errorf("session %d is not running", id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return Snap{Data: base64.StdEncoding.EncodeToString(s.recent), Off: s.total}, nil
}

// List describes every session, oldest first.
func (m *Manager) List() []Info {
	m.mu.Lock()
	list := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	m.mu.Unlock()
	out := make([]Info, 0, len(list))
	for _, s := range list {
		out = append(out, s.info())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Close ends a session.
func (m *Manager) Close(id int) {
	m.mu.Lock()
	s := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if s == nil {
		return
	}
	m.emit("session:close", Exit{ID: id})
	if p := s.cmd.Process; p != nil && !s.done() {
		_ = p.Signal(syscall.SIGHUP)
		go func() {
			select {
			case <-s.exited:
			case <-time.After(2 * time.Second):
				_ = p.Kill()
			}
		}()
	}
	_ = s.pty.Close()
}

func (s *session) done() bool {
	select {
	case <-s.exited:
		return true
	default:
		return false
	}
}

// quietFor is how long a session must be quiet after working before it
// counts as finished (waiting for you).
const quietFor = 3 * time.Second

// Finished returns the sessions that worked for at least minWork and have
// now been quiet for a while — each one once per stretch of work.
func (m *Manager) Finished(minWork time.Duration) []Info {
	m.mu.Lock()
	list := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	m.mu.Unlock()
	var out []Info
	now := time.Now()
	for _, s := range list {
		s.mu.Lock()
		hit := !s.workStart.IsZero() && now.Sub(s.workLast) >= quietFor
		worked := hit && s.workLast.Sub(s.workStart) >= minWork
		if hit {
			s.workStart = time.Time{}
		}
		s.mu.Unlock()
		if worked && !s.done() {
			out = append(out, s.info())
		}
	}
	return out
}

// Busy reports whether any session wrote output within d.
func (m *Manager) Busy(d time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		s.mu.Lock()
		last := s.workLast
		s.mu.Unlock()
		if !s.done() && time.Since(last) < d {
			return true
		}
	}
	return false
}

// Running counts sessions whose program has not ended.
func (m *Manager) Running() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, s := range m.sessions {
		if !s.done() {
			n++
		}
	}
	return n
}

// CloseAll ends every session.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	ids := make([]int, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Close(id)
	}
}

// loginEnv is the environment of a login shell. An app started from the
// Dock does not get the PATH set up in ~/.zshrc, so we ask the shell for it.
func (m *Manager) loginEnv() []string {
	m.envOnce.Do(func() {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/zsh"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		const marker = "__QUARTERS_ENV__"
		cmd := exec.CommandContext(ctx, shell, "-l", "-i", "-c", "printf "+marker+"; env -0")
		cmd.Stdin = nil
		out, _ := cmd.Output()
		var env []string
		if i := bytes.Index(out, []byte(marker)); i >= 0 {
			for _, kv := range bytes.Split(out[i+len(marker):], []byte{0}) {
				if len(kv) > 0 && bytes.IndexByte(kv, '=') > 0 {
					env = append(env, string(kv))
				}
			}
		}
		if len(env) == 0 {
			env = os.Environ()
			home, _ := os.UserHomeDir()
			env = append(env, "PATH="+strings.Join([]string{
				filepath.Join(home, ".local", "bin"), "/opt/homebrew/bin", "/usr/local/bin",
				"/usr/bin", "/bin", "/usr/sbin", "/sbin"}, ":"))
		}
		m.env = cleanEnv(env)
	})
	return m.env
}

// cleanEnv sets the terminal type and drops variables that belong to the
// terminal (or Claude Code session) Quarters itself was started from.
func cleanEnv(env []string) []string {
	drop := []string{"TERM=", "COLORTERM=", "TERM_PROGRAM=", "TERM_PROGRAM_VERSION=",
		"LC_TERMINAL=", "LC_TERMINAL_VERSION=", "ITERM_", "TERM_SESSION_ID=", "SHLVL=", "_=",
		"CLAUDECODE=", "CLAUDE_CODE_ENTRYPOINT=", "CLAUDE_CODE_SESSION_", "CLAUDE_CODE_CHILD_SESSION=",
		"CLAUDE_CODE_MESSAGING_", "CLAUDE_CODE_BRIDGE_SESSION_ID=", "CLAUDE_CODE_EXECPATH=", "CLAUDE_PID=",
		"CLAUDE_EFFORT="}
	var out []string
	seen := map[string]int{}
next:
	for _, kv := range env {
		for _, d := range drop {
			if strings.HasPrefix(kv, d) {
				continue next
			}
		}
		key, _, _ := strings.Cut(kv, "=")
		if i, ok := seen[key]; ok {
			out[i] = kv // later wins
			continue
		}
		seen[key] = len(out)
		out = append(out, kv)
	}
	if _, ok := seen["LANG"]; !ok {
		out = append(out, "LANG=en_US.UTF-8")
	}
	return append(out, "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=quarters")
}

func lookPath(name string, env []string) (string, error) {
	for _, kv := range env {
		if dirs, ok := strings.CutPrefix(kv, "PATH="); ok {
			for _, dir := range filepath.SplitList(dirs) {
				p := filepath.Join(dir, name)
				if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
					return p, nil
				}
			}
		}
	}
	return exec.LookPath(name)
}

// Installed reports whether a program is on the login shell's PATH.
func (m *Manager) Installed(name string) bool {
	_, err := lookPath(name, m.loginEnv())
	return err == nil
}

// escapes are the terminal control sequences in a program's output: CSI
// (colours, cursor moves), OSC (titles, links) and the short ones.
var escapes = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)

// Printed reports whether session id's recent output shows text (no
// spaces in it): colours, cursor moves and line breaks are ignored, so text
// a program wrapped across lines still counts.
func (m *Manager) Printed(id int, text string) bool {
	s := m.get(id)
	if s == nil || text == "" || strings.ContainsAny(text, " \t\r\n") {
		return false
	}
	s.mu.Lock()
	out := escapes.ReplaceAll(s.recent, nil)
	s.mu.Unlock()
	out = bytes.Join(bytes.Fields(out), nil)
	return bytes.Contains(out, []byte(text))
}
