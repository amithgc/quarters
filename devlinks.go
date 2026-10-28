package main

import (
	"bytes"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/amithgc/quarters/internal/session"
)

// ---- dev server links ----

type sessionLinks struct {
	tail []byte // end of the last chunk: a link split across chunks
	urls []string
}

type portCheck struct {
	up bool
	at time.Time
}

var (
	ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[()][0-9A-Za-z]`)
	linkRe = regexp.MustCompile(`https?://(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1?\]):(\d{2,5})(/[^\s"'<>)\]]*)?`)
)

// watchOutput finds links to local servers in a session's output.
func (a *App) watchOutput(id int, data []byte) {
	on := a.current().On("dev_links")
	a.fmu.Lock()
	defer a.fmu.Unlock()
	if !on {
		delete(a.f.links, id)
		return
	}
	l := a.f.links[id]
	if l == nil {
		l = &sessionLinks{}
		a.f.links[id] = l
	}
	text := append(append([]byte{}, l.tail...), data...)
	l.tail = lastBytes(data, 120)
	if !bytes.Contains(text, []byte("://")) {
		return
	}
	text = ansiRe.ReplaceAll(text, nil)
	added := false
	for _, m := range linkRe.FindAllSubmatch(text, -1) {
		port, _ := strconv.Atoi(string(m[1]))
		if port < 80 || port > 65535 {
			continue
		}
		u := "http://localhost:" + string(m[1]) + strings.TrimRight(string(m[2]), ".,;:")
		u = strings.TrimSuffix(u, "/")
		if !slices.Contains(l.urls, u) {
			l.urls = append(l.urls, u)
			if len(l.urls) > 6 {
				l.urls = l.urls[1:]
			}
			added = true
		}
	}
	if added {
		go a.emit("links", nil)
	}
}

func lastBytes(b []byte, n int) []byte {
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return append([]byte{}, b...)
}

// LinkView is a link to a local server a terminal printed.
type LinkView struct {
	ID    int    `json:"id"` // session
	Path  string `json:"path"`
	Title string `json:"title"`
	URL   string `json:"url"`
	Up    bool   `json:"up"`
}

// DevLinks lists the local servers the open terminals mentioned, and
// whether each is answering.
func (a *App) DevLinks() []LinkView {
	out := []LinkView{}
	if !a.config().On("dev_links") {
		return out
	}
	live := map[int]session.Info{}
	for _, info := range a.sessions.List() {
		live[info.ID] = info
	}
	a.fmu.Lock()
	type item struct {
		id  int
		url string
	}
	var items []item
	for id, l := range a.f.links {
		if info, ok := live[id]; !ok || info.Exited {
			delete(a.f.links, id)
			continue
		}
		for _, u := range l.urls {
			items = append(items, item{id, u})
		}
	}
	a.fmu.Unlock()
	slices.SortFunc(items, func(x, y item) int { return x.id - y.id })
	for _, it := range items {
		m := metaOf(live[it.id])
		title := m.Title
		if title == "" {
			title = m.Kind
		}
		out = append(out, LinkView{ID: it.id, Path: m.Path, Title: title, URL: it.url, Up: a.portUp(it.url)})
	}
	return out
}

func (a *App) portUp(u string) bool {
	pu, err := url.Parse(u)
	if err != nil {
		return false
	}
	addr := "127.0.0.1:" + pu.Port()
	a.fmu.Lock()
	c, ok := a.f.ports[addr]
	a.fmu.Unlock()
	if ok && time.Since(c.at) < 3*time.Second {
		return c.up
	}
	conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
	if err != nil {
		if conn6, err6 := net.DialTimeout("tcp", "[::1]:"+pu.Port(), 200*time.Millisecond); err6 == nil {
			conn, err = conn6, nil
		}
	}
	up := err == nil
	if up {
		conn.Close()
	}
	a.fmu.Lock()
	a.f.ports[addr] = portCheck{up, time.Now()}
	a.fmu.Unlock()
	return up
}

// OpenOnMac opens a local server's link in the Mac's browser.
func (a *App) OpenOnMac(link string) error {
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.User != nil {
		return fmt.Errorf("not a link")
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
	default:
		return fmt.Errorf("only links to this Mac's own servers")
	}
	return exec.Command("open", u.String()).Run()
}
