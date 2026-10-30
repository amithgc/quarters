package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ---- ports: what a project's tabs listen on, and stopping it ----

// PortView is a TCP port a process of yours listens on.
type PortView struct {
	Port    int    `json:"port"`
	PID     int    `json:"pid"`
	Process string `json:"process"` // its command name: node, vite, go…
	Tab     int    `json:"tab"`     // the session it runs in (0: not started from Quarters)
	Title   string `json:"title"`   // that tab's title
}

type listener struct {
	port int
	pid  int
	name string
}

var portScan = struct {
	sync.Mutex
	at   time.Time
	list []listener
}{}

// listeners lists the TCP ports your processes listen on (1024 and up),
// read with lsof (or ss, on Linux without lsof) at most every 2 s.
func listeners(fresh bool) []listener {
	portScan.Lock()
	defer portScan.Unlock()
	if !fresh && time.Since(portScan.at) < 2*time.Second {
		return portScan.list
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var list []listener
	if out, err := exec.CommandContext(ctx, "lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-a", "-u", strconv.Itoa(os.Getuid()), "-Fpcn").Output(); err == nil || len(out) > 0 {
		list = parseLsof(string(out))
	} else if out, err := exec.CommandContext(ctx, "ss", "-ltnpH").Output(); err == nil {
		list = parseSS(string(out))
	}
	portScan.at, portScan.list = time.Now(), list
	return list
}

// parseLsof reads lsof -F pcn: a p (pid) line, a c (command) line, then an
// n (address) line per socket.
func parseLsof(out string) []listener {
	var list []listener
	seen := map[[2]int]bool{}
	pid, name := 0, ""
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
		case 'c':
			name = line[1:]
		case 'n':
			i := strings.LastIndex(line, ":")
			port, err := strconv.Atoi(line[i+1:])
			if err != nil || port < 1024 || seen[[2]int{pid, port}] {
				continue
			}
			seen[[2]int{pid, port}] = true
			list = append(list, listener{port, pid, name})
		}
	}
	return list
}

var ssUser = regexp.MustCompile(`\("([^"]+)",pid=(\d+)`)

// parseSS reads ss -ltnpH: the local address is the 4th column, the
// process in users:(("name",pid=…)) (only your own processes show one).
func parseSS(out string) []listener {
	var list []listener
	seen := map[[2]int]bool{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		m := ssUser.FindStringSubmatch(line)
		if len(f) < 4 || m == nil {
			continue
		}
		i := strings.LastIndex(f[3], ":")
		port, err := strconv.Atoi(f[3][i+1:])
		pid, _ := strconv.Atoi(m[2])
		if err != nil || port < 1024 || seen[[2]int{pid, port}] {
			continue
		}
		seen[[2]int{pid, port}] = true
		list = append(list, listener{port, pid, m[1]})
	}
	return list
}

// parents maps each process to its parent, from ps.
func parents() map[int]int {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, "ps", "-A", "-o", "pid=,ppid=").Output()
	m := map[int]int{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 {
			pid, _ := strconv.Atoi(f[0])
			ppid, _ := strconv.Atoi(f[1])
			m[pid] = ppid
		}
	}
	return m
}

// processDir is a process's working folder ("" when it can't be read).
func processDir(pid int) string {
	if dir, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid)); err == nil {
		return dir // Linux
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, "lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn").Output()
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "n/") {
			return line[1:]
		}
	}
	return ""
}

// Ports lists what a project listens on: ports its tabs opened (whatever
// runs in them), then other processes of yours working in its folder.
func (a *App) Ports(path string) []PortView {
	if _, ok := a.project(path); !ok {
		return nil
	}
	list := listeners(false)
	if len(list) == 0 {
		return nil
	}
	// which tab each session's program is
	type tab struct {
		id    int
		path  string
		title string
	}
	tabs := map[int]tab{}
	for _, info := range a.sessions.List() {
		if pid := a.sessions.Pid(info.ID); pid > 0 {
			m := metaOf(info)
			tabs[pid] = tab{info.ID, m.Path, m.Title}
		}
	}
	up := parents()
	var mine, others []PortView
	for _, l := range list {
		if l.pid == os.Getpid() {
			continue // Quarters' own remote-access server
		}
		v := PortView{Port: l.port, PID: l.pid, Process: l.name}
		found := false
		for p, n := l.pid, 0; p > 1 && n < 64; p, n = up[p], n+1 {
			if t, ok := tabs[p]; ok {
				found = true
				if t.path == path {
					v.Tab, v.Title = t.id, t.title
					mine = append(mine, v)
				}
				break
			}
		}
		if !found {
			if dir := processDir(l.pid); dir != "" && within(path, dir) {
				others = append(others, v)
			}
		}
	}
	byPort := func(s []PortView) { sort.Slice(s, func(i, j int) bool { return s[i].Port < s[j].Port }) }
	byPort(mine)
	byPort(others)
	return append(mine, others...)
}

// StopPort ends the process listening on port with pid: SIGTERM, then
// SIGKILL if it's still there after 3 s. Only a process of yours that is
// listening right now can be stopped.
func (a *App) StopPort(pid, port int) error {
	ok := false
	for _, l := range listeners(true) {
		if l.pid == pid && l.port == port && pid != os.Getpid() {
			ok = true
		}
	}
	if !ok {
		return errors.New("that port isn't open any more")
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return err
	}
	for range 30 {
		time.Sleep(100 * time.Millisecond)
		if syscall.Kill(pid, 0) != nil {
			listeners(true)
			return nil
		}
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	listeners(true)
	return nil
}
