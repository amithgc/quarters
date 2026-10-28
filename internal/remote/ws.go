package remote

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// A signed-in browser talks to the app over one WebSocket: calls (with an
// id, answered with the same id) one way, events the other.

const (
	maxMessage   = 4 << 20 // a big paste
	sendQueue    = 1024    // messages waiting for a slow browser before it is dropped
	pingEvery    = 25 * time.Second
	readDeadline = 70 * time.Second
	writeTimeout = 15 * time.Second
	maxBusyCalls = 8 // calls running at once per browser
)

// Calls answered in the order they arrive, without waiting: typing.
var inline = map[string]bool{"Write": true, "Resize": true, "Claim": true}

type client struct {
	id     string
	device string
	name   string
	sess   [32]byte
	conn   *websocket.Conn
	send   chan []byte
	busy   chan struct{}
	once   sync.Once
	closed chan struct{}
}

func (c *client) close(code int, why string) {
	c.once.Do(func() {
		_ = c.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, why), time.Now().Add(time.Second))
		close(c.closed)
		_ = c.conn.Close()
	})
}

// queue sends a message, or drops the browser if it cannot keep up (it
// reconnects and catches up from a snapshot).
func (c *client) queue(msg []byte) {
	select {
	case c.send <- msg:
	case <-c.closed:
	default:
		go c.close(websocket.CloseTryAgainLater, "too slow")
	}
}

type hub struct {
	s        *Server
	upgrader websocket.Upgrader

	mu      sync.Mutex
	clients map[*client]bool
	n       int
	done    chan struct{}
	stopped sync.Once
}

func newHub(s *Server) *hub {
	h := &hub{s: s, clients: map[*client]bool{}, done: make(chan struct{})}
	h.upgrader = websocket.Upgrader{
		ReadBufferSize:    16 << 10,
		WriteBufferSize:   32 << 10,
		EnableCompression: true,
		// Only our own page may open the socket (cookies alone would let
		// another site's page in: cross-site WebSocket hijacking).
		CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == s.origin },
	}
	return h
}

func (h *hub) serve(w http.ResponseWriter, r *http.Request) {
	sess, hash := h.s.sessionOf(r)
	if sess == nil {
		http.Error(w, "sign in first", http.StatusUnauthorized)
		return
	}
	if r.Header.Get("Origin") != h.s.origin {
		h.s.log.add("BLOCKED", "cross-origin websocket", h.s.clientIP(r), r.Header.Get("Origin"))
		http.Error(w, "cross-origin request blocked", http.StatusForbidden)
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(maxMessage)
	h.mu.Lock()
	h.n++
	c := &client{id: fmt.Sprintf("web%d", h.n), device: sess.device, name: sess.name, sess: hash, conn: conn,
		send: make(chan []byte, sendQueue), busy: make(chan struct{}, maxBusyCalls), closed: make(chan struct{})}
	h.clients[c] = true
	h.mu.Unlock()

	access := ""
	if d, ok := h.s.auth.st.device(c.device); ok {
		access = d.Access
	}
	hello, _ := json.Marshal(map[string]any{"ev": "remote:hello", "d": map[string]string{"client": c.id, "device": c.name, "access": orFull(access)}})
	c.queue(hello)
	go h.writer(c)
	h.reader(c)

	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	c.close(websocket.CloseNormalClosure, "")
}

type call struct {
	ID int64             `json:"id"`
	M  string            `json:"m"`
	A  []json.RawMessage `json:"a"`
}

type reply struct {
	ID int64  `json:"id"`
	R  any    `json:"r"`
	E  string `json:"e,omitempty"`
}

func (h *hub) reader(c *client) {
	_ = c.conn.SetReadDeadline(time.Now().Add(readDeadline))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(readDeadline))
	})
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		_ = c.conn.SetReadDeadline(time.Now().Add(readDeadline))
		if !h.s.auth.valid(c.sess) {
			c.close(4401, "signed out")
			return
		}
		h.s.auth.touch(c.sess)
		var m call
		if json.Unmarshal(data, &m) != nil || m.M == "" {
			continue
		}
		if inline[m.M] {
			h.answer(c, m)
			continue
		}
		select {
		case c.busy <- struct{}{}:
		case <-c.closed:
			return
		}
		go func() {
			defer func() { <-c.busy }()
			h.answer(c, m)
		}()
	}
}

func (h *hub) answer(c *client, m call) {
	res, err := h.s.backend.Call(c.id, m.M, m.A)
	if inline[m.M] && err == nil {
		return // nobody waits for keystrokes
	}
	rep := reply{ID: m.ID, R: res}
	if err != nil {
		rep.E = err.Error()
		rep.R = nil
	}
	data, err := json.Marshal(rep)
	if err != nil {
		data, _ = json.Marshal(reply{ID: m.ID, E: "could not encode the answer"})
	}
	c.queue(data)
}

func (h *hub) writer(c *client) {
	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case msg := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				c.close(websocket.CloseGoingAway, "")
				return
			}
		case <-ping.C:
			if err := c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeTimeout)); err != nil {
				c.close(websocket.CloseGoingAway, "")
				return
			}
		case <-c.closed:
			return
		}
	}
}

func (h *hub) broadcast(msg []byte) {
	h.mu.Lock()
	list := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		list = append(list, c)
	}
	h.mu.Unlock()
	for _, c := range list {
		c.queue(msg)
	}
}

func (h *hub) closeWhere(match func(*client) bool, code int, why string) {
	h.mu.Lock()
	var list []*client
	for c := range h.clients {
		if match(c) {
			list = append(list, c)
		}
	}
	h.mu.Unlock()
	for _, c := range list {
		c.close(code, why)
	}
}

func (h *hub) closeSession(hash [32]byte) {
	h.closeWhere(func(c *client) bool { return c.sess == hash }, 4401, "signed out")
}

// deviceOf is the device a connected client signed in with.
func (h *hub) deviceOf(id string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.id == id {
			return c.device, true
		}
	}
	return "", false
}

func (h *hub) closeDevice(id string) {
	h.closeWhere(func(c *client) bool { return c.device == id }, 4401, "device removed")
}

func (h *hub) closeAll() {
	h.closeWhere(func(*client) bool { return true }, 4401, "signed out")
}

func (h *hub) online() map[string]int {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]int{}
	for c := range h.clients {
		out[c.device]++
	}
	return out
}

// sweep closes connections whose session expired.
func (h *hub) sweep() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			h.closeWhere(func(c *client) bool { return !h.s.auth.valid(c.sess) }, 4401, "session expired")
		case <-h.done:
			return
		}
	}
}

func (h *hub) stop() { h.stopped.Do(func() { close(h.done) }) }
