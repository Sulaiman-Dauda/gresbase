// Package netutil holds small networking helpers shared across packages —
// notably trusted-proxy-aware client IP resolution.
package netutil

import (
	"net"
	"strings"
)

// HostOnly strips the port (and any IPv6 brackets) from an address, returning
// just the host/IP portion.
func HostOnly(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	addr = strings.TrimPrefix(addr, "[")
	addr = strings.TrimSuffix(addr, "]")
	return addr
}

// IPInCIDRs reports whether ip falls inside any of the given CIDR ranges (or
// equals a bare IP entry).
func IPInCIDRs(ip string, cidrs []string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !strings.Contains(c, "/") {
			if other := net.ParseIP(c); other != nil && other.Equal(parsed) {
				return true
			}
			continue
		}
		if _, network, err := net.ParseCIDR(c); err == nil && network.Contains(parsed) {
			return true
		}
	}
	return false
}

// ClientIP resolves the real client IP. When no trusted proxies are configured,
// or the immediate peer (remoteAddr) is not within a trusted CIDR, the peer
// address is returned and the client-supplied forwarding headers are ignored —
// the only safe default, since those headers are trivially spoofable. When the
// peer IS a trusted proxy, the X-Forwarded-For chain is walked from the right
// and the first address that is not itself a trusted proxy is returned (falling
// back to X-Real-IP, then the peer).
func ClientIP(remoteAddr, xff, xRealIP string, trustedProxies []string) string {
	peer := HostOnly(remoteAddr)

	if len(trustedProxies) == 0 || !IPInCIDRs(peer, trustedProxies) {
		return peer
	}

	if xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			candidate := HostOnly(strings.TrimSpace(parts[i]))
			if candidate == "" {
				continue
			}
			if !IPInCIDRs(candidate, trustedProxies) {
				return candidate
			}
		}
	}
	if ip := strings.TrimSpace(xRealIP); ip != "" {
		return HostOnly(ip)
	}
	return peer
}
