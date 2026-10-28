package remote

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestPasswordSignIn(t *testing.T) {
	s, ts, b := newTestServer(t)
	login := func(user, pw string) *http.Response {
		return post(t, ts, "/auth/password", s.origin, `{"user":"`+user+`","password":"`+pw+`"}`)
	}
	if res := login("amith", "correct horse battery"); res.StatusCode != http.StatusConflict {
		t.Errorf("password sign-in while it is off: %d, want 409", res.StatusCode)
	}
	if err := s.SetPassword("amith", "short"); err == nil {
		t.Error("a 5-character password was accepted")
	}
	if err := s.SetPassword("amith", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mustRead(t, s.auth.st.path)), "correct horse") {
		t.Fatal("remote.json holds the password itself")
	}
	if len(owner{s.auth.st}.WebAuthnCredentials()) != 0 {
		t.Error("the password counts as a passkey")
	}
	if res := login("amith", "wrong password!"); res.StatusCode != http.StatusForbidden {
		t.Errorf("wrong password: %d, want 403", res.StatusCode)
	}
	if res := login("someone", "correct horse battery"); res.StatusCode != http.StatusForbidden {
		t.Errorf("wrong username: %d, want 403", res.StatusCode)
	}
	if res := post(t, ts, "/auth/password", "https://evil.example", `{"user":"amith","password":"correct horse battery"}`); res.StatusCode != http.StatusForbidden {
		t.Errorf("password sign-in from another site: %d, want 403", res.StatusCode)
	}
	res := login("Amith", "correct horse battery")
	if res.StatusCode != http.StatusOK || len(res.Cookies()) == 0 {
		t.Fatalf("right password: %d", res.StatusCode)
	}
	cookie := res.Cookies()[0]
	if r := get(t, ts, "/app.js", func(r *http.Request) { r.AddCookie(cookie) }); r.StatusCode != http.StatusOK {
		t.Errorf("signed in with the password, GET /app.js: %d", r.StatusCode)
	}
	b.mu.Lock()
	alerted := len(b.alerts) > 0
	b.mu.Unlock()
	if !alerted {
		t.Error("no notification on the Mac for a password sign-in")
	}
	// A new password signs out the old one's browsers.
	if err := s.SetPassword("amith", "another long password"); err != nil {
		t.Fatal(err)
	}
	if r := get(t, ts, "/app.js", func(r *http.Request) { r.AddCookie(cookie) }); r.StatusCode == http.StatusOK {
		t.Error("still signed in after the password changed")
	}
	// Removing the device turns password sign-in off.
	if err := s.RemoveDevice(passwordDevice); err != nil {
		t.Fatal(err)
	}
	if res := login("amith", "another long password"); res.StatusCode != http.StatusConflict {
		t.Errorf("password sign-in after removing it: %d, want 409", res.StatusCode)
	}
}

func TestHomeNetwork(t *testing.T) {
	st, err := New(Options{Listen: "127.0.0.1:8421", Dir: t.TempDir(), LAN: true}, &fakeBackend{})
	if err != nil {
		t.Fatalf("home network without a URL: %v", err)
	}
	for h, want := range map[string]bool{
		"192.168.1.20:8421": true, "10.0.0.5:8421": true, "100.101.1.2:8421": true, st.mdns + ":8421": true,
		"192.168.1.20:9999": false, "8.8.8.8:8421": false, "evil.example:8421": false, "127.0.0.1:8421": false,
	} {
		if got := st.lanHost(h); got != want {
			t.Errorf("lanHost(%q) = %v, want %v", h, got, want)
		}
	}
	for addr, want := range map[string]bool{"192.168.1.9:5000": true, "127.0.0.1:5000": true, "8.8.8.8:5000": false, "[2001:db8::1]:5000": false} {
		r := httptest.NewRequest("GET", "/login", nil)
		r.RemoteAddr = addr
		if got := st.nearby(r); got != want {
			t.Errorf("nearby(%s) = %v, want %v", addr, got, want)
		}
	}
	if off, _ := New(Options{Listen: "127.0.0.1:8421", Dir: t.TempDir()}, &fakeBackend{}); off.lanHost("192.168.1.20:8421") {
		t.Error("home-network addresses answered with the option off")
	}

	// End to end: the page and sign-in at a home-network address.
	s, ts, _ := newTestServer(t)
	s.opts.LAN = true
	lan := "192.168.1.20:" + s.port
	req := func(method, path, origin, body string) *http.Response {
		r, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
		r.Host = lan
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		res, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(r)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	if res := req("GET", "/login", "", ""); res.StatusCode != http.StatusOK {
		t.Errorf("GET /login at %s: %d", lan, res.StatusCode)
	}
	if err := s.SetPassword("amith", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	body := `{"user":"amith","password":"correct horse battery"}`
	if res := req("POST", "/auth/password", s.origin, body); res.StatusCode != http.StatusForbidden {
		t.Errorf("sign-in at %s from the localhost page: %d, want 403", lan, res.StatusCode)
	}
	res := req("POST", "/auth/password", "http://"+lan, body)
	if res.StatusCode != http.StatusOK || len(res.Cookies()) == 0 || res.Cookies()[0].Secure {
		t.Errorf("sign-in at %s: %d, cookies %v", lan, res.StatusCode, res.Cookies())
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPhysicalPorts(t *testing.T) {
	for name, want := range map[string]bool{"en0": true, "enp3s0": true, "eth0": true, "wlan0": true, "wlp2s0": true, "bridge100": false, "docker0": false, "veth12ab": false, "virbr0": false, "lo": false, "utun3": false} {
		if physicalPort(name) != want {
			t.Errorf("physicalPort(%q) = %v", name, !want)
		}
	}
}
