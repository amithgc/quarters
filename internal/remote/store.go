package remote

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// Device is a phone or computer paired with quarters: one passkey.
type Device struct {
	ID         string              `json:"id"` // the passkey's credential id, base64url
	Name       string              `json:"name"`
	Created    time.Time           `json:"created"`
	LastUsed   time.Time           `json:"last_used"`
	LastIP     string              `json:"last_ip"`
	Credential webauthn.Credential `json:"credential"`
	// Access is what the device may do: "" (full), "type" (type into
	// running sessions only) or "view" (watch only).
	Access string    `json:"access,omitempty"`
	Push   []PushSub `json:"push,omitempty"`
}

// PushSub is a browser's Web Push subscription.
type PushSub struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"`
	Auth     string `json:"auth"`
}

// store is remote.json: the owner's random WebAuthn user id and the paired
// devices. It holds only public keys, but is still written 0600.
type store struct {
	path string
	mu   sync.Mutex

	UserID  []byte   `json:"user_id"`
	Devices []Device `json:"devices"`
	// VAPID keys sign Web Push messages (made the first time).
	VAPIDPublic  string `json:"vapid_public,omitempty"`
	VAPIDPrivate string `json:"vapid_private,omitempty"`
	// Password is the username and password sign-in (password.go).
	Password *passwordHash `json:"password,omitempty"`
}

func loadStore(dir string) (*store, error) {
	st := &store{path: filepath.Join(dir, "remote.json")}
	data, err := os.ReadFile(st.path)
	switch {
	case os.IsNotExist(err):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(data, st); err != nil {
			return nil, fmt.Errorf("remote.json: %w", err)
		}
	}
	if len(st.UserID) == 0 {
		st.UserID = randomBytes(32)
		if err := st.saveLocked(); err != nil {
			return nil, err
		}
	}
	return st, nil
}

// saveLocked writes the file atomically; the caller holds mu (or owns st).
func (st *store) saveLocked() error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(st.path), 0o700); err != nil {
		return err
	}
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, st.path)
}

func (st *store) devices() []Device {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]Device(nil), st.Devices...)
}

func (st *store) device(id string) (Device, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, d := range st.Devices {
		if d.ID == id {
			return d, true
		}
	}
	return Device{}, false
}

func (st *store) add(d Device) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.Devices = append(st.Devices, d)
	return st.saveLocked()
}

// used records a sign-in: the new signature counter, time and address.
func (st *store) used(id string, cred webauthn.Credential, ip string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := range st.Devices {
		if st.Devices[i].ID == id {
			st.Devices[i].Credential = cred
			st.Devices[i].LastUsed = time.Now()
			st.Devices[i].LastIP = ip
			return st.saveLocked()
		}
	}
	return fmt.Errorf("device not found")
}

// update changes a device and saves.
func (st *store) update(id string, f func(*Device)) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i := range st.Devices {
		if st.Devices[i].ID == id {
			f(&st.Devices[i])
			return st.saveLocked()
		}
	}
	return fmt.Errorf("device not found")
}

func (st *store) remove(id string) (Device, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i, d := range st.Devices {
		if d.ID == id {
			st.Devices = append(st.Devices[:i], st.Devices[i+1:]...)
			if id == passwordDevice {
				st.Password = nil
			}
			return d, st.saveLocked()
		}
	}
	return Device{}, fmt.Errorf("device not found")
}

// owner is the one Quarters user, as the WebAuthn library sees it.
type owner struct{ st *store }

func (o owner) WebAuthnID() []byte          { return o.st.UserID }
func (o owner) WebAuthnName() string        { return "quarters" }
func (o owner) WebAuthnDisplayName() string { return "Quarters" }
func (o owner) WebAuthnCredentials() []webauthn.Credential {
	var out []webauthn.Credential
	for _, d := range o.st.devices() {
		if len(d.Credential.ID) > 0 { // not the password
			out = append(out, d.Credential)
		}
	}
	return out
}

func credID(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // the system's random source failing is not recoverable
	}
	return b
}

func randomToken(n int) string { return base64.RawURLEncoding.EncodeToString(randomBytes(n)) }

// ---- security log ----

// maxLogBytes caps remote.log; older half is dropped when it grows past it.
const maxLogBytes = 512 << 10

type auditLog struct {
	path string
	mu   sync.Mutex
}

// add appends one line: time, event and details.
func (l *auditLog) add(event string, details ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	clean := make([]string, 0, len(details))
	for _, d := range details {
		d = strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return ' '
			}
			return r
		}, d)
		if len(d) > 160 {
			d = d[:160] + "…"
		}
		clean = append(clean, d)
	}
	line := fmt.Sprintf("%s  %-14s %s\n", time.Now().Format("2006-01-02 15:04:05"), event, strings.Join(clean, "  "))
	if st, err := os.Stat(l.path); err == nil && st.Size() > maxLogBytes {
		if data, err := os.ReadFile(l.path); err == nil {
			data = data[len(data)/2:]
			if i := bytes.IndexByte(data, '\n'); i >= 0 {
				data = data[i+1:]
			}
			_ = os.WriteFile(l.path, data, 0o600)
		}
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

// tail returns the last n lines, newest first.
func (l *auditLog) tail(n int) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	data, err := os.ReadFile(l.path)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var out []string
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		if lines[i] != "" {
			out = append(out, lines[i])
		}
	}
	return out
}
