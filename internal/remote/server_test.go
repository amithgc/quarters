package remote

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gorilla/websocket"
)

type fakeBackend struct {
	mu     sync.Mutex
	calls  []string
	alerts []string
}

func (f *fakeBackend) Call(client, method string, args []json.RawMessage) (any, error) {
	f.mu.Lock()
	f.calls = append(f.calls, client+":"+method)
	f.mu.Unlock()
	return "ok:" + method, nil
}

func (f *fakeBackend) Alert(title, body string) {
	f.mu.Lock()
	f.alerts = append(f.alerts, title)
	f.mu.Unlock()
}

// newTestServer serves on an httptest server whose address is the public URL.
func newTestServer(t *testing.T) (*Server, *httptest.Server, *fakeBackend) {
	t.Helper()
	b := &fakeBackend{}
	app := fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html>app")},
		"app.js":               {Data: []byte("console.log('app')")},
		"manifest.webmanifest": {Data: []byte("{}")},
		"sw.js":                {Data: []byte("// sw")},
	}
	var s *Server
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.ServeHTTP(w, r) }))
	ts.Start()
	t.Cleanup(ts.Close)
	var err error
	// WebAuthn needs a name, not an IP address: the page is "localhost".
	s, err = New(Options{URL: strings.Replace(ts.URL, "127.0.0.1", "localhost", 1), Listen: ts.Listener.Addr().String(), Dir: t.TempDir(), App: app}, b)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.hub.stop)
	return s, ts, b
}

// signedIn pairs a fake device and returns a session cookie for it.
func signedIn(t *testing.T, s *Server) *http.Cookie {
	t.Helper()
	dev := Device{ID: "dev1", Name: "test phone", Created: time.Now()}
	if err := s.auth.st.add(dev); err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: s.cookieName(), Value: s.auth.newSession(dev, "127.0.0.1")}
}

func get(t *testing.T, ts *httptest.Server, path string, mod func(*http.Request)) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", ts.URL+path, nil)
	if mod != nil {
		mod(req)
	}
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res
}

func post(t *testing.T, ts *httptest.Server, path, origin, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res
}

func TestNewRejectsUnsafeSettings(t *testing.T) {
	for _, o := range []Options{
		{URL: "http://quarters.example.com", Listen: "127.0.0.1:8421"},          // plain http on the internet
		{URL: "https://quarters.example.com/x", Listen: "127.0.0.1:8421"},       // a path
		{URL: "https://quarters.example.com", Listen: "0.0.0.0:8421"},           // open to the network without TLS
		{URL: "quarters.example.com", Listen: "127.0.0.1:8421"},                 // no scheme
		{URL: "https://user:pw@quarters.example.com", Listen: "127.0.0.1:8421"}, // credentials
		{Listen: "0.0.0.0:8421", TLSCert: "c", TLSKey: "k"},                     // no URL beyond this Mac
	} {
		o.Dir = t.TempDir()
		if _, err := New(o, &fakeBackend{}); err == nil {
			t.Errorf("New(%q, %q) accepted unsafe settings", o.URL, o.Listen)
		}
	}
	if _, err := New(Options{URL: "https://quarters.example.com", Listen: "127.0.0.1:8421", Dir: t.TempDir()}, &fakeBackend{}); err != nil {
		t.Errorf("a tunnel setup was refused: %v", err)
	}
	s, err := New(Options{Listen: "127.0.0.1:8421", Dir: t.TempDir()}, &fakeBackend{})
	if err != nil || s.Origin() != "http://localhost:8421" {
		t.Errorf("no public URL: %v, want http://localhost:8421", err)
	}
}

func TestSignedOutSeesOnlySignIn(t *testing.T) {
	_, ts, _ := newTestServer(t)
	if res := get(t, ts, "/", nil); res.StatusCode != http.StatusFound || res.Header.Get("Location") != "/login" {
		t.Errorf("GET / signed out: %d %s, want redirect to /login", res.StatusCode, res.Header.Get("Location"))
	}
	for _, p := range []string{"/app.js", "/index.html", "/api/ws", "/../app.js"} {
		if res := get(t, ts, p, nil); res.StatusCode != http.StatusUnauthorized && res.StatusCode != http.StatusFound {
			t.Errorf("GET %s signed out: %d", p, res.StatusCode)
		}
	}
	for _, p := range []string{"/login", "/pair", "/auth-ui/auth.js", "/manifest.webmanifest", "/sw.js"} {
		res := get(t, ts, p, nil)
		if res.StatusCode != http.StatusOK {
			t.Errorf("GET %s: %d, want 200", p, res.StatusCode)
		}
		if !strings.Contains(res.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") || res.Header.Get("X-Frame-Options") != "DENY" {
			t.Errorf("GET %s: missing security headers", p)
		}
	}
	if res := get(t, ts, "/auth/me", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("/auth/me signed out: %d", res.StatusCode)
	}
}

func TestSignedInGetsApp(t *testing.T) {
	s, ts, _ := newTestServer(t)
	c := signedIn(t, s)
	res := get(t, ts, "/app.js", func(r *http.Request) { r.AddCookie(c) })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /app.js signed in: %d", res.StatusCode)
	}
	// a made-up cookie gets nothing
	res = get(t, ts, "/app.js", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: s.cookieName(), Value: "forged"}) })
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("forged cookie: %d", res.StatusCode)
	}
}

// A page on another site resolving to this server (DNS rebinding) is refused.
func TestUnknownHostRefused(t *testing.T) {
	s, ts, _ := newTestServer(t)
	c := signedIn(t, s)
	res := get(t, ts, "/app.js", func(r *http.Request) {
		r.Host = "evil.example.com"
		r.AddCookie(c)
	})
	if res.StatusCode != http.StatusMisdirectedRequest {
		t.Errorf("foreign Host: %d, want 421", res.StatusCode)
	}
}

func TestAuthNeedsSameOrigin(t *testing.T) {
	s, ts, _ := newTestServer(t)
	for _, origin := range []string{"", "https://evil.example.com", "null"} {
		if res := post(t, ts, "/auth/login/begin", origin, "{}"); res.StatusCode != http.StatusForbidden {
			t.Errorf("login/begin with Origin %q: %d, want 403", origin, res.StatusCode)
		}
	}
	// same origin, but no device paired yet
	if res := post(t, ts, "/auth/login/begin", s.origin, "{}"); res.StatusCode != http.StatusConflict {
		t.Errorf("login/begin with nothing paired: %d, want 409", res.StatusCode)
	}
}

func TestPairingCode(t *testing.T) {
	s, ts, b := newTestServer(t)
	body := func(code string) string { return `{"code":"` + code + `","name":"phone"}` }
	if res := post(t, ts, "/auth/pair/begin", s.origin, body("guess")); res.StatusCode != http.StatusForbidden {
		t.Errorf("pairing without a link: %d", res.StatusCode)
	}
	p := s.auth.newPairing()
	if res := post(t, ts, "/auth/pair/begin", s.origin, body(p.code)); res.StatusCode != http.StatusOK {
		t.Fatalf("pairing with the right code: %d", res.StatusCode)
	}
	// a guessing address is locked out…
	for i := 0; i < ipLockAfter; i++ {
		post(t, ts, "/auth/pair/begin", s.origin, body("wrong"))
	}
	if res := post(t, ts, "/auth/pair/begin", s.origin, body(p.code)); res.StatusCode != http.StatusTooManyRequests {
		t.Errorf("the guessing address after %d wrong codes: %d, want 429", ipLockAfter, res.StatusCode)
	}
	// …and wrong guesses from anywhere burn the link, even for the right code afterwards
	for i := 0; i < maxPairErrors; i++ {
		s.auth.checkPairing("wrong")
	}
	if s.auth.checkPairing(p.code) {
		t.Errorf("the link still works after %d wrong codes", maxPairErrors)
	}
	s.auth.mu.Lock()
	clear(s.auth.ipFails) // a different, well-behaved address from here on
	s.auth.mu.Unlock()
	// finishing without a begun ceremony fails
	if res := post(t, ts, "/auth/pair/finish", s.origin, "{}"); res.StatusCode != http.StatusBadRequest {
		t.Errorf("pair/finish without begin: %d", res.StatusCode)
	}
	if len(s.auth.st.devices()) != 0 {
		t.Error("a device was paired")
	}
	_ = b
}

func TestPairingExpires(t *testing.T) {
	s, _, _ := newTestServer(t)
	p := s.auth.newPairing()
	s.auth.mu.Lock()
	s.auth.pair.expires = time.Now().Add(-time.Second)
	s.auth.mu.Unlock()
	if s.auth.checkPairing(p.code) {
		t.Error("an expired pairing code was accepted")
	}
}

func TestRateLimitAndLockout(t *testing.T) {
	s, ts, b := newTestServer(t)
	var last int
	for i := 0; i <= perIPRequests; i++ {
		last = post(t, ts, "/auth/pair/begin", s.origin, `{"code":"x","name":"y"}`).StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("request %d from one address: %d, want 429", perIPRequests+1, last)
	}
	// an address that keeps failing is locked out, without locking anyone else out
	pairing := s.auth.newPairing()
	for i := 0; i < ipLockAfter; i++ {
		s.auth.fail("198.51.100.7", "pairing", nil)
	}
	if ok, _ := s.auth.allow("198.51.100.7"); ok {
		t.Error("an address with many failures was not locked out")
	}
	if ok, _ := s.auth.allow("192.0.2.1"); !ok {
		t.Error("one address's failures locked out another")
	}
	if !s.auth.checkPairing(pairing.code) {
		t.Error("an attacker's failures cancelled the pairing link")
	}
	s.auth.mu.Lock()
	first := s.auth.ipFails["198.51.100.7"].until
	s.auth.mu.Unlock()
	s.auth.fail("198.51.100.7", "pairing", nil)
	s.auth.mu.Lock()
	if !s.auth.ipFails["198.51.100.7"].until.After(first.Add(30 * time.Second)) {
		t.Error("the lockout did not grow with more failures")
	}
	s.auth.mu.Unlock()
	// failures from very many addresses lock sign-in for everyone and tell the Mac
	for i := 0; i < lockFailures; i++ {
		s.auth.fail(fmt.Sprintf("10.0.%d.%d", i/250, i%250), "sign-in", nil)
	}
	if ok, _ := s.auth.allow("192.0.2.1"); ok {
		t.Error("sign-in was not locked after many failures")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.alerts) == 0 {
		t.Error("the Mac was not told about the lockout")
	}
}

func TestSessionExpiry(t *testing.T) {
	s, _, _ := newTestServer(t)
	c := signedIn(t, s)
	sess, h := s.auth.session(c.Value)
	if sess == nil {
		t.Fatal("fresh session not valid")
	}
	s.auth.mu.Lock()
	s.auth.sessions[h].seen = time.Now().Add(-sessionIdle - time.Minute)
	s.auth.mu.Unlock()
	if sess, _ := s.auth.session(c.Value); sess != nil {
		t.Error("idle session still valid")
	}
}

func dialWS(ts *httptest.Server, origin string, c *http.Cookie) (*websocket.Conn, *http.Response, error) {
	h := http.Header{}
	if origin != "" {
		h.Set("Origin", origin)
	}
	if c != nil {
		h.Set("Cookie", c.String())
	}
	return websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/api/ws", h)
}

func TestWebSocket(t *testing.T) {
	s, ts, b := newTestServer(t)
	if _, res, err := dialWS(ts, s.origin, nil); err == nil || res.StatusCode != http.StatusUnauthorized {
		t.Errorf("websocket signed out: err=%v", err)
	}
	c := signedIn(t, s)
	// cross-site WebSocket hijacking: right cookie, wrong page
	if _, res, err := dialWS(ts, "https://evil.example.com", c); err == nil || res.StatusCode != http.StatusForbidden {
		t.Errorf("websocket from another origin: err=%v", err)
	}
	conn, _, err := dialWS(ts, s.origin, c)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var hello struct {
		Ev string            `json:"ev"`
		D  map[string]string `json:"d"`
	}
	if err := conn.ReadJSON(&hello); err != nil || hello.Ev != "remote:hello" || hello.D["device"] != "test phone" {
		t.Fatalf("hello: %+v %v", hello, err)
	}
	if err := conn.WriteJSON(map[string]any{"id": 7, "m": "Projects", "a": []any{}}); err != nil {
		t.Fatal(err)
	}
	var rep struct {
		ID int    `json:"id"`
		R  string `json:"r"`
	}
	if err := conn.ReadJSON(&rep); err != nil || rep.ID != 7 || rep.R != "ok:Projects" {
		t.Fatalf("reply: %+v %v", rep, err)
	}
	s.Broadcast("pty:data", map[string]int{"id": 1})
	var ev struct{ Ev string }
	if err := conn.ReadJSON(&ev); err != nil || ev.Ev != "pty:data" {
		t.Fatalf("event: %+v %v", ev, err)
	}
	// removing the device cuts the connection at once
	if err := s.RemoveDevice("dev1"); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = conn.ReadMessage()
	if ce, ok := err.(*websocket.CloseError); !ok || ce.Code != 4401 {
		t.Errorf("after removing the device: %v, want close 4401", err)
	}
	if sess, _ := s.auth.session(c.Value); sess != nil {
		t.Error("session survived removing its device")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.calls) != 1 || !strings.HasPrefix(b.calls[0], "web") {
		t.Errorf("calls: %v", b.calls)
	}
}

func TestPushEndpointsOnlyKnownServices(t *testing.T) {
	for _, ok := range []string{"https://web.push.apple.com/abc", "https://fcm.googleapis.com/fcm/send/x", "https://updates.push.services.mozilla.com/wpush/v2/x"} {
		if !validEndpoint(ok) {
			t.Errorf("%s refused", ok)
		}
	}
	for _, bad := range []string{"http://web.push.apple.com/x", "https://evil.com/push.apple.com", "https://push.apple.com.evil.com/x",
		"https://127.0.0.1/x", "https://user@fcm.googleapis.com/x", "https://evilfcm.googleapis.com.attacker.io/"} {
		if validEndpoint(bad) {
			t.Errorf("%s accepted", bad)
		}
	}
}
