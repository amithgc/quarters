// Package remote serves Quarters to a browser (a phone, another computer)
// over the internet, safely: devices sign in with a passkey (Face ID, Touch
// ID or a PIN), new devices can only be paired from the Mac app, and every
// request is checked for origin, host and a live session.
package remote

import (
	"compress/gzip"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"rsc.io/qr"
)

//go:embed web
var webFiles embed.FS

// Backend is the app behind the web page.
type Backend interface {
	// Call runs one of the app's functions for a browser (client).
	Call(client, method string, args []json.RawMessage) (any, error)
	// Alert tells the person at the Mac about a security event.
	Alert(title, body string)
}

// Options configure the server.
type Options struct {
	URL     string // public address, e.g. https://quarters.example.com
	Listen  string // local address, e.g. 127.0.0.1:8421
	TLSCert string // needed when Listen is not loopback
	TLSKey  string
	Dir     string // where remote.json and remote.log are kept
	App     fs.FS  // the web app (frontend/dist)
}

// Server is the remote-access web server.
type Server struct {
	opts    Options
	backend Backend
	origin  string          // scheme://host[:port] of URL
	hosts   map[string]bool // Host headers accepted
	secure  bool            // URL is https
	auth    *auth
	hub     *hub
	log     *auditLog
	web     fs.FS

	mu      sync.Mutex
	srv     *http.Server
	running bool
}

// New checks the options and loads the paired devices.
func New(opts Options, b Backend) (*Server, error) {
	u, err := url.Parse(strings.TrimSpace(opts.URL))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("set the public URL, e.g. https://quarters.example.com")
	}
	if u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.User != nil {
		return nil, fmt.Errorf("the URL must be just the address (no path), e.g. https://quarters.example.com")
	}
	host := u.Hostname()
	if net.ParseIP(strings.Trim(host, "[]")) != nil {
		return nil, fmt.Errorf("the URL needs a name, not an IP address (passkeys are tied to a name), e.g. https://quarters.example.com")
	}
	if u.Scheme == "http" && !isLoopbackName(host) {
		return nil, fmt.Errorf("the URL must start with https:// (plain http is only allowed for localhost)")
	}
	if opts.Listen == "" {
		return nil, fmt.Errorf("no listen address")
	}
	lh, _, err := net.SplitHostPort(opts.Listen)
	if err != nil {
		return nil, fmt.Errorf("listen address %q: %v", opts.Listen, err)
	}
	if !isLoopbackName(lh) && (opts.TLSCert == "" || opts.TLSKey == "") {
		return nil, fmt.Errorf("listening on %s (not this Mac only) needs tls_cert and tls_key; or keep 127.0.0.1 and put a tunnel in front", opts.Listen)
	}
	log := &auditLog{path: filepath.Join(opts.Dir, "remote.log")}
	st, err := loadStore(opts.Dir)
	if err != nil {
		return nil, err
	}
	s := &Server{opts: opts, backend: b, origin: u.Scheme + "://" + u.Host, secure: u.Scheme == "https", log: log,
		hosts: map[string]bool{strings.ToLower(u.Host): true}}
	// The tunnel may pass the local address as Host. Only exact loopback
	// names are accepted, which still stops DNS-rebinding attacks.
	_, port, _ := net.SplitHostPort(opts.Listen)
	for _, h := range []string{"127.0.0.1", "localhost", "[::1]"} {
		s.hosts[h+":"+port] = true
	}
	s.auth, err = newAuth(host, s.origin, st, log, b.Alert)
	if err != nil {
		return nil, err
	}
	s.hub = newHub(s)
	s.web, _ = fs.Sub(webFiles, "web")
	return s, nil
}

func isLoopbackName(h string) bool {
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && ip.IsLoopback()
}

// Start listens in the background.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.opts.Listen)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %v", s.opts.Listen, err)
	}
	srv := &http.Server{
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	s.mu.Lock()
	s.srv, s.running = srv, true
	s.mu.Unlock()
	s.log.add("started", s.origin, "listening on "+s.opts.Listen)
	go s.hub.sweep()
	go func() {
		var err error
		if s.opts.TLSCert != "" {
			err = srv.ServeTLS(ln, s.opts.TLSCert, s.opts.TLSKey)
		} else {
			err = srv.Serve(ln)
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.add("stopped", err.Error())
		}
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()
	return nil
}

// Stop closes the server and every browser connection.
func (s *Server) Stop() {
	s.mu.Lock()
	srv := s.srv
	s.srv, s.running = nil, false
	s.mu.Unlock()
	s.hub.closeAll()
	s.auth.dropDevice("")
	s.auth.cancelPairing()
	if srv != nil {
		_ = srv.Close()
		s.log.add("stopped")
	}
	s.hub.stop()
}

// Running reports whether the server is up.
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Origin is the public address.
func (s *Server) Origin() string { return s.origin }

// Broadcast sends an event to every signed-in browser.
func (s *Server) Broadcast(event string, data any) {
	msg, err := json.Marshal(struct {
		Ev string `json:"ev"`
		D  any    `json:"d"`
	}{event, data})
	if err == nil {
		s.hub.broadcast(msg)
	}
}

// Pairing is a one-time link (and its QR code) for pairing a new device.
type Pairing struct {
	URL     string `json:"url"`
	QR      string `json:"qr"` // data: URL of a PNG
	Expires int64  `json:"expires"`
}

// NewPairing makes a pairing link, valid for 10 minutes and one device.
func (s *Server) NewPairing() (Pairing, error) {
	p := s.auth.newPairing()
	link := s.origin + "/pair#" + p.code // after "#": never sent to a server or logged
	code, err := qr.Encode(link, qr.M)
	if err != nil {
		return Pairing{}, err
	}
	code.Scale = 6
	return Pairing{URL: link, QR: "data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG()), Expires: p.expires.UnixMilli()}, nil
}

// CancelPairing makes the current pairing link useless.
func (s *Server) CancelPairing() { s.auth.cancelPairing() }

// DeviceView is a paired device as the Mac shows it.
type DeviceView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Created  int64  `json:"created"`
	LastUsed int64  `json:"lastUsed"`
	LastIP   string `json:"lastIP"`
	Online   int    `json:"online"` // open connections
	Access   string `json:"access"` // full, type or view
	Push     bool   `json:"push"`   // gets phone alerts
}

// Devices lists the paired devices.
func (s *Server) Devices() []DeviceView {
	online := s.hub.online()
	var out []DeviceView
	for _, d := range s.auth.st.devices() {
		out = append(out, DeviceView{ID: d.ID, Name: d.Name, Created: d.Created.UnixMilli(),
			LastUsed: d.LastUsed.UnixMilli(), LastIP: d.LastIP, Online: online[d.ID], Access: orFull(d.Access), Push: len(d.Push) > 0})
	}
	return out
}

// RemoveDevice unpairs a device and disconnects it at once.
func (s *Server) RemoveDevice(id string) error {
	d, err := s.auth.st.remove(id)
	if err != nil {
		return err
	}
	s.auth.dropDevice(id)
	s.hub.closeDevice(id)
	s.log.add("removed", d.Name)
	return nil
}

// SignOutAll ends every browser session; devices stay paired.
func (s *Server) SignOutAll() {
	s.auth.dropDevice("")
	s.hub.closeAll()
	s.log.add("signed-out-all")
}

// Log returns the newest lines of the security log.
func (s *Server) Log(n int) []string { return s.log.tail(n) }

// DevicesIn lists the devices paired in dir, without a running server.
func DevicesIn(dir string) []DeviceView {
	st, err := loadStore(dir)
	if err != nil {
		return nil
	}
	var out []DeviceView
	for _, d := range st.devices() {
		out = append(out, DeviceView{ID: d.ID, Name: d.Name, Created: d.Created.UnixMilli(), LastUsed: d.LastUsed.UnixMilli(), LastIP: d.LastIP,
			Access: orFull(d.Access), Push: len(d.Push) > 0})
	}
	return out
}

// LogIn returns the newest lines of the security log in dir.
func LogIn(dir string, n int) []string {
	return (&auditLog{path: filepath.Join(dir, "remote.log")}).tail(n)
}

// SetAccessFile changes a device's access without a running server.
func SetAccessFile(dir, id, access string) error {
	switch access {
	case AccessFull, AccessType, AccessView:
	default:
		return fmt.Errorf("unknown access %q", access)
	}
	st, err := loadStore(dir)
	if err != nil {
		return err
	}
	return st.update(id, func(d *Device) { d.Access = access })
}

// RemoveDeviceFile unpairs a device without a running server.
func RemoveDeviceFile(dir, id string) error {
	st, err := loadStore(dir)
	if err != nil {
		return err
	}
	d, err := st.remove(id)
	if err == nil {
		(&auditLog{path: filepath.Join(dir, "remote.log")}).add("removed", d.Name)
	}
	return err
}

func (s *Server) alert(title, body string) { s.backend.Alert(title, body) }

func (s *Server) cookieName() string {
	if s.secure {
		return "__Host-quarters" // only ever sent to this exact host, over https
	}
	return "quarters"
}

// clientIP is the browser's address. A request from a local tunnel
// (cloudflared, tailscale, ngrok) carries it as the last X-Forwarded-For
// entry, the one the tunnel itself appends; earlier entries, and headers
// like CF-Connecting-IP, can be set by anyone and are not trusted.
func (s *Server) clientIP(r *http.Request) string {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if v := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(v) != nil {
				return v
			}
		}
	}
	return host
}

// ---- requests ----

const csp = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; " +
	"font-src 'self'; connect-src 'self'; worker-src 'self'; manifest-src 'self'; frame-ancestors 'none'; " +
	"base-uri 'none'; form-action 'self'; object-src 'none'"

// public paths need no sign-in: the sign-in pages and what they show.
func public(p string) bool {
	switch p {
	case "/login", "/pair", "/manifest.webmanifest", "/sw.js", "/favicon.ico":
		return true
	}
	for _, pre := range []string{"/auth-ui/", "/icons/", "/vendor/fonts/", "/mascot/"} {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Security-Policy", csp)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	if s.secure {
		h.Set("Strict-Transport-Security", "max-age=31536000")
	}
	if !s.hosts[strings.ToLower(r.Host)] {
		http.Error(w, "unknown host", http.StatusMisdirectedRequest)
		return
	}
	p := path.Clean("/" + r.URL.Path)

	if strings.HasPrefix(p, "/auth/") {
		h.Set("Cache-Control", "no-store")
		s.serveAuth(w, r, p)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if p == "/api/ws" {
		s.hub.serve(w, r)
		return
	}
	if !public(p) {
		if sess, _ := s.sessionOf(r); sess == nil {
			if p == "/" || strings.HasSuffix(p, ".html") {
				http.Redirect(w, r, "/login", http.StatusFound)
			} else {
				http.Error(w, "sign in first", http.StatusUnauthorized)
			}
			return
		}
	}
	switch {
	case p == "/login" || p == "/pair":
		h.Set("Cache-Control", "no-store")
		s.serveFile(w, r, s.web, strings.TrimPrefix(p, "/")+".html")
	case strings.HasPrefix(p, "/auth-ui/"):
		h.Set("Cache-Control", "no-cache")
		s.serveFile(w, r, s.web, strings.TrimPrefix(p, "/auth-ui/"))
	case p == "/" || p == "/index.html":
		h.Set("Cache-Control", "no-store")
		s.serveFile(w, r, s.opts.App, "index.html")
	case p == "/sw.js":
		h.Set("Cache-Control", "no-cache")
		s.serveFile(w, r, s.opts.App, "sw.js")
	default:
		if strings.HasPrefix(p, "/vendor/") || strings.HasPrefix(p, "/mascot/") || strings.HasPrefix(p, "/icons/") {
			h.Set("Cache-Control", "private, max-age=86400")
		} else {
			h.Set("Cache-Control", "no-cache")
		}
		s.serveFile(w, r, s.opts.App, strings.TrimPrefix(p, "/"))
	}
}

func (s *Server) sessionOf(r *http.Request) (*session, [32]byte) {
	c, err := r.Cookie(s.cookieName())
	if err != nil {
		return nil, [32]byte{}
	}
	return s.auth.session(c.Value)
}

// sameOrigin is the CSRF check: the request must come from our own page.
func (s *Server) sameOrigin(r *http.Request) bool {
	if r.Header.Get("Origin") != s.origin {
		return false
	}
	site := r.Header.Get("Sec-Fetch-Site")
	return site == "" || site == "same-origin"
}

func (s *Server) serveAuth(w http.ResponseWriter, r *http.Request, p string) {
	if p == "/auth/me" && r.Method == http.MethodGet {
		sess, _ := s.sessionOf(r)
		if sess == nil {
			writeJSON(w, http.StatusUnauthorized, jsonErr{"signed out"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"name": sess.name})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.sameOrigin(r) {
		s.log.add("BLOCKED", "cross-origin "+p, s.clientIP(r), r.Header.Get("Origin"))
		writeJSON(w, http.StatusForbidden, jsonErr{"cross-origin request blocked"})
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType, jsonErr{"expected JSON"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBody)
	if p == "/auth/logout" {
		s.logout(w, r)
		return
	}
	if ok, why := s.auth.allow(s.clientIP(r)); !ok {
		writeJSON(w, http.StatusTooManyRequests, jsonErr{why})
		return
	}
	switch p {
	case "/auth/pair/begin":
		s.pairBegin(w, r)
	case "/auth/pair/finish":
		s.pairFinish(w, r)
	case "/auth/login/begin":
		s.loginBegin(w, r)
	case "/auth/login/finish":
		s.loginFinish(w, r)
	default:
		http.NotFound(w, r)
	}
}

var compressible = map[string]bool{".js": true, ".css": true, ".html": true, ".json": true, ".svg": true, ".webmanifest": true}

// serveFile serves name from fsys, gzipped when the browser accepts it.
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	f, err := fsys.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	ext := path.Ext(name)
	if ext == ".webmanifest" {
		w.Header().Set("Content-Type", "application/manifest+json")
	}
	if compressible[ext] && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", mimeOf(ext))
		}
		if r.Method == http.MethodHead {
			return
		}
		gz := gzip.NewWriter(w)
		defer gz.Close()
		_, _ = io.Copy(gz, f)
		return
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "unreadable", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, name, time.Time{}, rs)
}

func mimeOf(ext string) string {
	switch ext {
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".html":
		return "text/html; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".webmanifest":
		return "application/manifest+json"
	}
	return "application/json"
}
