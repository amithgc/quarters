package remote

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Signing in with a username and password: for browsers that can't use a
// passkey, such as one opening Quarters at the Mac's address on the home
// network (passkeys need HTTPS or localhost, and never work on an IP
// address). It is set on the Mac only, and shows up as one device,
// passwordDevice, so its access level, sessions and removal work like a
// passkey's.

const passwordDevice = "password"

// Password rules.
const (
	minPassword = 10
	maxPassword = 256
	maxUser     = 64
)

// argon2id settings (OWASP's recommendation is at least 19 MiB, 2 passes).
const (
	argonTime    = 3
	argonMemory  = 64 << 10 // KiB
	argonThreads = 2
	argonKeyLen  = 32
)

// passwordHash is the stored password: never the password itself.
type passwordHash struct {
	User    string `json:"user"`
	Salt    []byte `json:"salt"`
	Hash    []byte `json:"hash"`
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory"`
	Threads uint8  `json:"threads"`
}

// checking bounds how many passwords are hashed at once (each takes 64 MiB).
var checking = make(chan struct{}, 2)

func hashPassword(user, password string) *passwordHash {
	p := &passwordHash{User: user, Salt: randomBytes(16), Time: argonTime, Memory: argonMemory, Threads: argonThreads}
	p.Hash = argon2.IDKey([]byte(password), p.Salt, p.Time, p.Memory, p.Threads, argonKeyLen)
	return p
}

// matches reports whether user and password are right, in constant time.
func (p *passwordHash) matches(user, password string) bool {
	checking <- struct{}{}
	defer func() { <-checking }()
	got := argon2.IDKey([]byte(password), p.Salt, p.Time, p.Memory, p.Threads, uint32(len(p.Hash)))
	userOK := subtle.ConstantTimeCompare([]byte(strings.ToLower(user)), []byte(strings.ToLower(p.User)))
	passOK := subtle.ConstantTimeCompare(got, p.Hash)
	return userOK&passOK == 1
}

// checkCredentials validates a new username and password.
func checkCredentials(user, password string) error {
	switch {
	case user == "" || cleanName(user) != user || strings.ContainsAny(user, " \t"):
		return errors.New("choose a username (letters, digits, no spaces)")
	case utf8.RuneCountInString(user) > maxUser:
		return fmt.Errorf("the username can have at most %d characters", maxUser)
	case utf8.RuneCountInString(password) < minPassword:
		return fmt.Errorf("the password needs at least %d characters", minPassword)
	case len(password) > maxPassword:
		return fmt.Errorf("the password can have at most %d characters", maxPassword)
	}
	return nil
}

// setPassword stores a new username and password (replacing any earlier
// one) and its device entry, keeping that entry's access level.
func (st *store) setPassword(user, password string) error {
	if err := checkCredentials(user, password); err != nil {
		return err
	}
	h := hashPassword(user, password)
	st.mu.Lock()
	defer st.mu.Unlock()
	st.Password = h
	name := "Password · " + user
	for i := range st.Devices {
		if st.Devices[i].ID == passwordDevice {
			st.Devices[i].Name = name
			return st.saveLocked()
		}
	}
	st.Devices = append(st.Devices, Device{ID: passwordDevice, Name: name, Created: time.Now()})
	return st.saveLocked()
}

func (st *store) password() *passwordHash {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.Password
}

// passwordUser is the username of password sign-in ("" when it is off).
func (st *store) passwordUser() string {
	if p := st.password(); p != nil {
		return p.User
	}
	return ""
}

// SetPassword sets the username and password; browsers signed in with the
// old one are signed out.
func (s *Server) SetPassword(user, password string) error {
	if err := s.auth.st.setPassword(user, password); err != nil {
		return err
	}
	s.auth.dropDevice(passwordDevice)
	s.hub.closeDevice(passwordDevice)
	s.log.add("password-set", user)
	return nil
}

// SetPasswordFile sets the password without a running server.
func SetPasswordFile(dir, user, password string) error {
	st, err := loadStore(dir)
	if err != nil {
		return err
	}
	if err := st.setPassword(user, password); err != nil {
		return err
	}
	(&auditLog{path: filepath.Join(dir, "remote.log")}).add("password-set", user)
	return nil
}

// PasswordUserIn is the username of password sign-in in dir ("" when off).
func PasswordUserIn(dir string) string {
	st, err := loadStore(dir)
	if err != nil {
		return ""
	}
	return st.passwordUser()
}

// passwordLogin: {user, password} → signed in.
func (s *Server) passwordLogin(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	var req struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Password) > maxPassword || len(req.User) > 4*maxUser {
		writeJSON(w, http.StatusBadRequest, jsonErr{"bad request"})
		return
	}
	p := s.auth.st.password()
	if p == nil {
		writeJSON(w, http.StatusConflict, jsonErr{"Password sign-in is off. Set a username and password on the Mac: Remote Access."})
		return
	}
	if !p.matches(strings.TrimSpace(req.User), req.Password) {
		s.auth.fail(ip, "password sign-in", fmt.Errorf("wrong username or password for %q", cleanName(req.User)))
		writeJSON(w, http.StatusForbidden, jsonErr{"Wrong username or password."})
		return
	}
	dev, ok := s.auth.st.device(passwordDevice)
	if !ok {
		writeJSON(w, http.StatusConflict, jsonErr{"Password sign-in is off."})
		return
	}
	_ = s.auth.st.update(dev.ID, func(d *Device) { d.LastUsed, d.LastIP = time.Now(), ip })
	s.auth.log.add("sign-in", dev.Name, ip, r.UserAgent())
	s.alert("Quarters: remote sign-in", fmt.Sprintf("“%s” signed in from %s.", p.User, ip))
	s.signIn(w, r, dev, ip)
	writeJSON(w, http.StatusOK, map[string]string{"name": dev.Name})
}
