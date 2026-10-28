package remote

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// Web Push: a phone gets "Claude is waiting" even with Quarters closed.
// Messages are encrypted for the browser and signed with our VAPID key;
// the push service (Apple, Google, Mozilla) only relays them.

// pushHosts are the push services a subscription may point at, so a
// device cannot make Quarters send requests anywhere else.
var pushHosts = []string{".push.apple.com", "fcm.googleapis.com", ".push.services.mozilla.com", ".notify.windows.com", "android.googleapis.com"}

func validEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || len(endpoint) > 1024 {
		return false
	}
	h := strings.ToLower(u.Hostname())
	for _, p := range pushHosts {
		if h == strings.TrimPrefix(p, ".") || strings.HasPrefix(p, ".") && strings.HasSuffix(h, p) {
			return true
		}
	}
	return false
}

// vapid returns the VAPID keys, making them the first time.
func (st *store) vapid() (pub, priv string, err error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.VAPIDPublic == "" || st.VAPIDPrivate == "" {
		priv, pub, err := webpush.GenerateVAPIDKeys()
		if err != nil {
			return "", "", err
		}
		st.VAPIDPublic, st.VAPIDPrivate = pub, priv
		if err := st.saveLocked(); err != nil {
			return "", "", err
		}
	}
	return st.VAPIDPublic, st.VAPIDPrivate, nil
}

// PushKey is the public key browsers subscribe with.
func (s *Server) PushKey() (string, error) {
	pub, _, err := s.auth.st.vapid()
	return pub, err
}

// Subscribe stores client's push subscription on its device.
func (s *Server) Subscribe(client string, sub PushSub) error {
	dev, ok := s.hub.deviceOf(client)
	if !ok {
		return fmt.Errorf("not connected")
	}
	if !validEndpoint(sub.Endpoint) || sub.P256dh == "" || sub.Auth == "" || len(sub.P256dh) > 200 || len(sub.Auth) > 100 {
		return fmt.Errorf("unsupported push subscription")
	}
	err := s.auth.st.update(dev, func(d *Device) {
		for _, p := range d.Push {
			if p.Endpoint == sub.Endpoint {
				return
			}
		}
		d.Push = append(d.Push, sub)
		if len(d.Push) > 4 { // a device has one browser, maybe a few
			d.Push = d.Push[len(d.Push)-4:]
		}
	})
	if err == nil {
		s.log.add("push-on", s.deviceName(dev))
	}
	return err
}

// Unsubscribe forgets a push subscription of client's device.
func (s *Server) Unsubscribe(client, endpoint string) error {
	dev, ok := s.hub.deviceOf(client)
	if !ok {
		return fmt.Errorf("not connected")
	}
	return s.auth.st.update(dev, func(d *Device) {
		d.Push = dropSub(d.Push, endpoint)
	})
}

func dropSub(list []PushSub, endpoint string) []PushSub {
	out := list[:0]
	for _, p := range list {
		if p.Endpoint != endpoint {
			out = append(out, p)
		}
	}
	return out
}

func (s *Server) deviceName(id string) string {
	d, _ := s.auth.st.device(id)
	return d.Name
}

// PushMessage is what the service worker shows.
type PushMessage struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Key   string `json:"key"` // tab to open
}

// Push sends a notification to every subscribed device (in the
// background). Subscriptions the push service says are gone are dropped.
func (s *Server) Push(m PushMessage) {
	pub, priv, err := s.auth.st.vapid()
	if err != nil {
		return
	}
	payload, _ := json.Marshal(m)
	// the push service's contact for us; the library adds "mailto:" to a
	// non-https one itself
	subscriber := s.origin
	if !s.secure {
		subscriber = "quarters@localhost"
	}
	for _, d := range s.auth.st.devices() {
		for _, p := range d.Push {
			go func(dev string, p PushSub) {
				resp, err := webpush.SendNotification(payload, &webpush.Subscription{Endpoint: p.Endpoint, Keys: webpush.Keys{P256dh: p.P256dh, Auth: p.Auth}},
					&webpush.Options{HTTPClient: pushClient, Subscriber: subscriber, VAPIDPublicKey: pub, VAPIDPrivateKey: priv, TTL: 3600, Urgency: webpush.UrgencyHigh, Topic: topic(m.Key)})
				if err != nil {
					return
				}
				resp.Body.Close()
				if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound {
					_ = s.auth.st.update(dev, func(d *Device) { d.Push = dropSub(d.Push, p.Endpoint) })
				}
			}(d.ID, p)
		}
	}
}

// pushClient gives up on a push service that doesn't answer.
var pushClient = &http.Client{Timeout: 15 * time.Second}

// topic lets a newer message about the same tab replace an undelivered one
// (at most 32 URL-safe characters).
func topic(key string) string {
	var b strings.Builder
	for _, r := range key {
		if b.Len() >= 32 {
			break
		}
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return fmt.Sprint(time.Now().Unix() % 1000)
	}
	return b.String()
}

// ---- what each device may do ----

// Access levels.
const (
	AccessFull = ""
	AccessType = "type"
	AccessView = "view"
)

// AccessOf is what client's device may do ("view" when unknown).
func (s *Server) AccessOf(client string) string {
	dev, ok := s.hub.deviceOf(client)
	if !ok {
		return AccessView
	}
	d, ok := s.auth.st.device(dev)
	if !ok {
		return AccessView
	}
	return d.Access
}

// SetAccess changes what a device may do; its browsers reconnect with it.
func (s *Server) SetAccess(id, access string) error {
	switch access {
	case AccessFull, AccessType, AccessView:
	default:
		return fmt.Errorf("unknown access %q", access)
	}
	if err := s.auth.st.update(id, func(d *Device) { d.Access = access }); err != nil {
		return err
	}
	s.log.add("access", s.deviceName(id), orFull(access))
	s.hub.closeWhere(func(c *client) bool { return c.device == id }, 4000, "access changed")
	return nil
}

func orFull(a string) string {
	if a == "" {
		return "full"
	}
	return a
}
