package remote

import (
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
)

// The home network: with Options.LAN the server also answers at this
// Mac's private addresses and its .local name, over plain HTTP. Passkeys
// can't work there, so browsers sign in with the password.

// cgnat is 100.64.0.0/10, which Tailscale uses.
var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// private reports whether ip is on a home or private network.
func private(ip net.IP) bool {
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || cgnat.Contains(ip))
}

// localName is this Mac's Bonjour name, e.g. "my-mac.local".
func localName() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return ""
	}
	h = strings.ToLower(strings.TrimSuffix(h, "."))
	if i := strings.IndexByte(h, '.'); i >= 0 {
		h = h[:i]
	}
	return h + ".local"
}

// lanHost reports whether a Host header names this Mac on the home network:
// a private IP address or the .local name, with the server's port. A name
// an attacker controls (DNS rebinding) is never one of these.
func (s *Server) lanHost(hostport string) bool {
	if !s.opts.LAN {
		return false
	}
	host, port, err := net.SplitHostPort(strings.ToLower(hostport))
	if err != nil || port != s.port {
		return false
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return private(ip) && !ip.IsLoopback()
	}
	return s.mdns != "" && host == s.mdns
}

// nearby reports whether the browser connects from this Mac or the home
// network (a tunnel's connections come from this Mac).
func (s *Server) nearby(r *http.Request) bool {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return private(net.ParseIP(host))
}

// originOf is the origin a request's page must have: the home-network
// address it was opened at, or the URL's.
func (s *Server) originOf(r *http.Request) string {
	if s.lanHost(r.Host) {
		return "http://" + strings.ToLower(r.Host)
	}
	return s.origin
}

// reqSecure reports whether a request came over HTTPS (to the browser).
func (s *Server) reqSecure(r *http.Request) bool { return s.secure && !s.lanHost(r.Host) }

// LANAddresses are the addresses browsers on the home network can open:
// the Wi-Fi and Ethernet ports' (macOS en0…, Linux eth0, enp3s0, wlan0,
// wlp2s0…) and Tailscale's, not virtual machines' or containers' bridges.
// IP addresses come first: .local needs Bonjour (Avahi on Linux).
func LANAddresses(port string) []string {
	type addr struct{ ifc, ip string }
	var found []addr
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil || !private(n.IP) || n.IP.IsLinkLocalUnicast() {
				continue
			}
			if physicalPort(ifc.Name) || cgnat.Contains(n.IP) {
				found = append(found, addr{ifc.Name, n.IP.String()})
			}
		}
	}
	sort.Slice(found, func(i, j int) bool { // en0 (usually the main one) first
		if found[i].ifc != found[j].ifc {
			return found[i].ifc < found[j].ifc
		}
		return found[i].ip < found[j].ip
	})
	var ips []string
	for _, a := range found {
		ips = append(ips, "http://"+a.ip+":"+port)
	}
	if name := localName(); name != "" {
		ips = append(ips, "http://"+name+":"+port)
	}
	return ips
}

// physicalPort reports whether an interface is a Wi-Fi or Ethernet port.
func physicalPort(name string) bool {
	for _, p := range []string{"en", "eth", "wl"} { // en0, enp3s0, eth0, wlan0, wlp2s0
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
