package remote

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// How long things last.
const (
	pairingTTL    = 10 * time.Minute // a pairing link from the Mac
	ceremonyTTL   = 3 * time.Minute  // between "begin" and "finish" of a passkey prompt
	sessionIdle   = 12 * time.Hour   // signed out after this long unused
	sessionMax    = 7 * 24 * time.Hour
	maxPairErrors = 5 // wrong pairing codes before the link is burnt
)

// Rate limits on the sign-in endpoints.
const (
	perIPRequests = 20 // requests per address per window
	perIPWindow   = 5 * time.Minute

	// An address with this many failures is locked out, for ipLockBase,
	// doubling with every further failure up to ipLockMax. Failures are
	// forgotten after ipFailMemory without one.
	ipLockAfter  = 5
	ipLockBase   = time.Minute
	ipLockMax    = time.Hour
	ipFailMemory = time.Hour

	// A circuit breaker: this many failures from anywhere in lockWindow
	// (a spread-out attack) lock sign-in for everyone for lockFor.
	lockFailures = 100
	lockWindow   = 10 * time.Minute
	lockFor      = 15 * time.Minute

	maxCeremonies = 200 // passkey prompts in progress at once
)

const (
	cookieCeremony = "quarters_ceremony"
	maxAuthBody    = 64 << 10
)

// pairing is a one-time link shown on the Mac that lets one new device
// register a passkey.
type pairing struct {
	code    string
	expires time.Time
	errors  int
}

// ceremony is a passkey prompt in progress.
type ceremony struct {
	kind    string // "login" or "pair"
	data    webauthn.SessionData
	expires time.Time
	code    string // pair: the pairing code it was begun with
	name    string // pair: the device name typed in
}

// session is a signed-in browser. Only the SHA-256 of its cookie is kept.
type session struct {
	device  string
	name    string
	ip      string
	created time.Time
	seen    time.Time
}

type auth struct {
	wa    *webauthn.WebAuthn
	st    *store
	log   *auditLog
	alert func(title, body string)

	mu         sync.Mutex
	pair       *pairing
	ceremonies map[string]*ceremony
	sessions   map[[32]byte]*session
	hits       map[string][]time.Time // ip -> recent requests
	ipFails    map[string]*ipFail
	failures   []time.Time // from anywhere, for the circuit breaker
	lockedTill time.Time
}

// ipFail is an address's recent failures and how long it is locked out.
type ipFail struct {
	n     int
	last  time.Time
	until time.Time
}

func newAuth(rpID, origin string, st *store, log *auditLog, alert func(string, string)) (*auth, error) {
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: "Quarters",
		RPOrigins:     []string{origin},
	})
	if err != nil {
		return nil, err
	}
	return &auth{wa: wa, st: st, log: log, alert: alert,
		ceremonies: map[string]*ceremony{}, sessions: map[[32]byte]*session{}, hits: map[string][]time.Time{}, ipFails: map[string]*ipFail{}}, nil
}

// newPairing replaces any earlier pairing link with a fresh one.
func (a *auth) newPairing() pairing {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pair = &pairing{code: randomToken(24), expires: time.Now().Add(pairingTTL)}
	a.log.add("pairing-link", "valid "+pairingTTL.String())
	return *a.pair
}

func (a *auth) cancelPairing() {
	a.mu.Lock()
	a.pair = nil
	a.mu.Unlock()
}

// checkPairing reports whether code is the current, unexpired pairing code.
// Too many wrong codes burn the link.
func (a *auth) checkPairing(code string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.pair
	if p == nil || time.Now().After(p.expires) {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(p.code)) == 1 {
		return true
	}
	p.errors++
	if p.errors >= maxPairErrors {
		a.pair = nil
		a.log.add("pairing-burnt", "too many wrong codes")
	}
	return false
}

// usePairing consumes the pairing code (once).
func (a *auth) usePairing(code string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.pair
	if p == nil || time.Now().After(p.expires) || subtle.ConstantTimeCompare([]byte(code), []byte(p.code)) != 1 {
		return false
	}
	a.pair = nil
	return true
}

// ---- rate limiting ----

// allow counts a sign-in request from ip; false means slow down.
func (a *auth) allow(ip string) (bool, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	if now.Before(a.lockedTill) {
		return false, "sign-in is locked for a while after too many failed attempts"
	}
	if f := a.ipFails[ip]; f != nil && now.Before(f.until) {
		return false, fmt.Sprintf("too many failed attempts from your address; try again in %s", f.until.Sub(now).Round(time.Second))
	}
	recent := a.hits[ip][:0]
	for _, t := range a.hits[ip] {
		if now.Sub(t) < perIPWindow {
			recent = append(recent, t)
		}
	}
	if len(recent) >= perIPRequests {
		a.hits[ip] = recent
		return false, "too many attempts, try again in a few minutes"
	}
	a.hits[ip] = append(recent, now)
	if len(a.hits) > 10000 { // don't grow without bound under a flood
		for k, v := range a.hits {
			if len(v) == 0 || now.Sub(v[len(v)-1]) > perIPWindow {
				delete(a.hits, k)
			}
		}
	}
	return true, ""
}

// fail records a failed sign-in or pairing. Repeated failures lock out
// their address; very many from everywhere lock sign-in for everyone.
func (a *auth) fail(ip, what string, err error) {
	a.log.add("FAILED "+what, ip, errString(err))
	a.mu.Lock()
	now := time.Now()
	f := a.ipFails[ip]
	if f == nil || now.Sub(f.last) > ipFailMemory {
		f = &ipFail{}
		a.ipFails[ip] = f
	}
	f.n++
	f.last = now
	if f.n >= ipLockAfter {
		wait := ipLockBase << min(f.n-ipLockAfter, 10)
		f.until = now.Add(min(wait, ipLockMax))
	}
	if len(a.ipFails) > 10000 { // don't grow without bound under a flood
		for k, v := range a.ipFails {
			if now.Sub(v.last) > ipFailMemory {
				delete(a.ipFails, k)
			}
		}
	}
	recent := a.failures[:0]
	for _, t := range a.failures {
		if now.Sub(t) < lockWindow {
			recent = append(recent, t)
		}
	}
	a.failures = append(recent, now)
	locked := len(a.failures) >= lockFailures && now.After(a.lockedTill)
	if locked {
		a.lockedTill = now.Add(lockFor)
		a.failures = nil
	}
	a.mu.Unlock()
	if locked {
		a.log.add("LOCKED", fmt.Sprintf("%d failures in %s; sign-in locked for %s", lockFailures, lockWindow, lockFor))
		a.alert("Quarters: remote sign-in locked", fmt.Sprintf("%d failed attempts in %s. Sign-in is locked for %s.", lockFailures, lockWindow, lockFor))
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	var pe *protocol.Error
	if errors.As(err, &pe) && pe.DevInfo != "" {
		return pe.Details + ": " + pe.DevInfo
	}
	return err.Error()
}

// ---- sessions ----

func hashToken(tok string) [32]byte { return sha256.Sum256([]byte(tok)) }

func (a *auth) newSession(dev Device, ip string) string {
	tok := randomToken(32)
	a.mu.Lock()
	a.sessions[hashToken(tok)] = &session{device: dev.ID, name: dev.Name, ip: ip, created: time.Now(), seen: time.Now()}
	a.mu.Unlock()
	return tok
}

// session returns the signed-in session for a cookie value, or nil.
func (a *auth) session(tok string) (*session, [32]byte) {
	if tok == "" || len(tok) > 100 {
		return nil, [32]byte{}
	}
	h := hashToken(tok)
	a.mu.Lock()
	s := a.sessions[h]
	now := time.Now()
	if s != nil && (now.Sub(s.seen) > sessionIdle || now.Sub(s.created) > sessionMax) {
		delete(a.sessions, h)
		s = nil
	}
	if s != nil {
		s.seen = now
	}
	a.mu.Unlock()
	if s == nil {
		return nil, h
	}
	if _, ok := a.st.device(s.device); !ok { // the device was removed
		a.dropSession(h)
		return nil, h
	}
	return s, h
}

func (a *auth) valid(h [32]byte) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.sessions[h]
	return s != nil && time.Since(s.seen) <= sessionIdle && time.Since(s.created) <= sessionMax
}

func (a *auth) touch(h [32]byte) {
	a.mu.Lock()
	if s := a.sessions[h]; s != nil {
		s.seen = time.Now()
	}
	a.mu.Unlock()
}

func (a *auth) dropSession(h [32]byte) {
	a.mu.Lock()
	delete(a.sessions, h)
	a.mu.Unlock()
}

// dropDevice signs out every session of a device ("" = all devices).
func (a *auth) dropDevice(id string) {
	a.mu.Lock()
	for h, s := range a.sessions {
		if id == "" || s.device == id {
			delete(a.sessions, h)
		}
	}
	a.mu.Unlock()
}

// ---- ceremonies ----

func (a *auth) putCeremony(c *ceremony) string {
	id := randomToken(24)
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for k, v := range a.ceremonies {
		if now.After(v.expires) {
			delete(a.ceremonies, k)
		}
	}
	for len(a.ceremonies) >= maxCeremonies { // only a flood gets here: the oldest go
		var oldest string
		for k, v := range a.ceremonies {
			if oldest == "" || v.expires.Before(a.ceremonies[oldest].expires) {
				oldest = k
			}
		}
		delete(a.ceremonies, oldest)
	}
	a.ceremonies[id] = c
	return id
}

// takeCeremony returns and forgets a ceremony (each can finish once).
func (a *auth) takeCeremony(id, kind string) *ceremony {
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.ceremonies[id]
	delete(a.ceremonies, id)
	if c == nil || c.kind != kind || time.Now().After(c.expires) {
		return nil
	}
	return c
}

// ---- HTTP handlers (registered by the server, behind origin checks) ----

type jsonErr struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func setCeremonyCookie(w http.ResponseWriter, secure bool, id string) {
	http.SetCookie(w, &http.Cookie{Name: cookieCeremony, Value: id, Path: "/auth/", MaxAge: int(ceremonyTTL.Seconds()),
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
}

func clearCeremonyCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: cookieCeremony, Value: "", Path: "/auth/", MaxAge: -1,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
}

func ceremonyID(r *http.Request) string {
	if c, err := r.Cookie(cookieCeremony); err == nil {
		return c.Value
	}
	return ""
}

// pairBegin: {code, name} → passkey creation options.
func (s *Server) pairBegin(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	var req struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, jsonErr{"bad request"})
		return
	}
	if !s.auth.checkPairing(req.Code) {
		s.auth.fail(ip, "pairing", errors.New("invalid or expired pairing code"))
		writeJSON(w, http.StatusForbidden, jsonErr{"This pairing link is not valid any more. Make a new one on the Mac: Remote Access → Pair a device."})
		return
	}
	name := cleanName(req.Name)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, jsonErr{"Give this device a name."})
		return
	}
	var exclude []protocol.CredentialDescriptor
	for _, d := range s.auth.st.devices() {
		exclude = append(exclude, d.Credential.Descriptor())
	}
	opts, data, err := s.auth.wa.BeginRegistration(owner{s.auth.st},
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationRequired,
		}),
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation),
		webauthn.WithExclusions(exclude),
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonErr{"could not start: " + err.Error()})
		return
	}
	id := s.auth.putCeremony(&ceremony{kind: "pair", data: *data, expires: time.Now().Add(ceremonyTTL), code: req.Code, name: name})
	setCeremonyCookie(w, s.secure, id)
	writeJSON(w, http.StatusOK, opts)
}

// pairFinish: the new passkey → stored, and the browser is signed in.
func (s *Server) pairFinish(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	c := s.auth.takeCeremony(ceremonyID(r), "pair")
	clearCeremonyCookie(w, s.secure)
	if c == nil {
		writeJSON(w, http.StatusBadRequest, jsonErr{"That took too long. Start again."})
		return
	}
	cred, err := s.auth.wa.FinishRegistration(owner{s.auth.st}, c.data, r)
	if err != nil {
		s.auth.fail(ip, "pairing", err)
		writeJSON(w, http.StatusForbidden, jsonErr{"The passkey could not be verified."})
		return
	}
	if !cred.Flags.UserVerified {
		s.auth.fail(ip, "pairing", errors.New("no user verification"))
		writeJSON(w, http.StatusForbidden, jsonErr{"The passkey must be unlocked with Face ID, Touch ID or a PIN."})
		return
	}
	// The code is used up only now, so a cancelled prompt can be retried.
	if !s.auth.usePairing(c.code) {
		s.auth.fail(ip, "pairing", errors.New("pairing code expired during registration"))
		writeJSON(w, http.StatusForbidden, jsonErr{"This pairing link expired. Make a new one on the Mac."})
		return
	}
	dev := Device{ID: credID(cred.ID), Name: c.name, Created: time.Now(), LastUsed: time.Now(), LastIP: ip, Credential: *cred}
	if err := s.auth.st.add(dev); err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonErr{"could not save: " + err.Error()})
		return
	}
	s.auth.log.add("paired", dev.Name, ip, r.UserAgent())
	s.alert("Quarters: new device paired", fmt.Sprintf("“%s” can now use Quarters remotely (from %s).", dev.Name, ip))
	s.signIn(w, dev, ip)
	writeJSON(w, http.StatusOK, map[string]string{"name": dev.Name})
}

// loginBegin → passkey request options (any paired passkey).
func (s *Server) loginBegin(w http.ResponseWriter, r *http.Request) {
	if len(s.auth.st.devices()) == 0 {
		writeJSON(w, http.StatusConflict, jsonErr{"No device is paired yet. On the Mac: Remote Access → Pair a device."})
		return
	}
	opts, data, err := s.auth.wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonErr{"could not start: " + err.Error()})
		return
	}
	id := s.auth.putCeremony(&ceremony{kind: "login", data: *data, expires: time.Now().Add(ceremonyTTL)})
	setCeremonyCookie(w, s.secure, id)
	writeJSON(w, http.StatusOK, opts)
}

// loginFinish: a signed challenge → signed in.
func (s *Server) loginFinish(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	c := s.auth.takeCeremony(ceremonyID(r), "login")
	clearCeremonyCookie(w, s.secure)
	if c == nil {
		writeJSON(w, http.StatusBadRequest, jsonErr{"That took too long. Try again."})
		return
	}
	var dev Device
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		if subtle.ConstantTimeCompare(userHandle, s.auth.st.UserID) != 1 {
			return nil, errors.New("unknown user")
		}
		d, ok := s.auth.st.device(credID(rawID))
		if !ok {
			return nil, errors.New("unknown passkey (removed device?)")
		}
		dev = d
		return owner{s.auth.st}, nil
	}
	_, cred, err := s.auth.wa.FinishPasskeyLogin(handler, c.data, r)
	if err != nil {
		s.auth.fail(ip, "sign-in", err)
		writeJSON(w, http.StatusForbidden, jsonErr{"Sign-in failed. Is this device still paired?"})
		return
	}
	if !cred.Flags.UserVerified {
		s.auth.fail(ip, "sign-in", errors.New("no user verification"))
		writeJSON(w, http.StatusForbidden, jsonErr{"The passkey must be unlocked with Face ID, Touch ID or a PIN."})
		return
	}
	if cred.Authenticator.CloneWarning {
		s.auth.fail(ip, "sign-in", errors.New("signature counter went backwards: passkey may be cloned"))
		s.alert("Quarters: suspicious sign-in blocked", fmt.Sprintf("The passkey of “%s” may have been copied. Remove the device in Remote Access.", dev.Name))
		writeJSON(w, http.StatusForbidden, jsonErr{"Sign-in blocked."})
		return
	}
	_ = s.auth.st.used(dev.ID, *cred, ip)
	s.auth.log.add("sign-in", dev.Name, ip, r.UserAgent())
	s.alert("Quarters: remote sign-in", fmt.Sprintf("“%s” signed in from %s.", dev.Name, ip))
	s.signIn(w, dev, ip)
	writeJSON(w, http.StatusOK, map[string]string{"name": dev.Name})
}

func (s *Server) signIn(w http.ResponseWriter, dev Device, ip string) {
	tok := s.auth.newSession(dev, ip)
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: tok, Path: "/", MaxAge: int(sessionMax.Seconds()),
		HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(s.cookieName()); err == nil {
		h := hashToken(c.Value)
		s.auth.dropSession(h)
		s.hub.closeSession(h)
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// cleanName keeps a device name short and printable.
func cleanName(n string) string {
	n = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '<' || r == '>' {
			return -1
		}
		return r
	}, strings.TrimSpace(n))
	if r := []rune(n); len(r) > 40 {
		n = string(r[:40])
	}
	return n
}
