package router

import (
	"crypto/sha256"
	"crypto/subtle"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// The headers a client IP is derived from. dokku's nginx sets X-Forwarded-For to the address it was connected
// from. The FE, which calls the API from its server, sends the IP of the browser it is serving in
// X-Bardic-Client-IP, and proves it is the FE with X-Bardic-Proxy-Secret. Neither header is ever logged.
const (
	forwardedForHeader = "X-Forwarded-For"
	clientIPHeader     = "X-Bardic-Client-IP"
	proxyProofHeader   = "X-Bardic-Proxy-Secret"
)

// ipv6KeyBits is the prefix length IPv6 client IPs are grouped by: a single subscriber is usually given a /56 or a
// /64, so a key per address would let one client spread over a vast number of keys.
const ipv6KeyBits = 56

// ClientIPConfig says which hops in front of the API are trusted to report the client's IP.
type ClientIPConfig struct {
	// TrustedProxies are the networks of the reverse proxies whose X-Forwarded-For is believed: in production
	// dokku's nginx, on the docker bridge network.
	TrustedProxies []netip.Prefix
	// ProxySecret is the secret the FE sends to attribute a request to the client IP it names. Empty, no request
	// is attributed.
	ProxySecret string
}

// ClientIP returns the IP of the client a request is from, and whether the FE attributed it (docs/PLAN.md §5.1
// "Rate limits"):
//  1. the socket peer's IP, with an IPv4-mapped IPv6 address read as IPv4;
//  2. if the peer is a trusted proxy, instead the last entry of the last X-Forwarded-For line, the one the proxy
//     wrote, if it is an IP; X-Forwarded-For from any other peer is ignored, as the client wrote it;
//  3. if the request carries the configured proxy secret and an X-Bardic-Client-IP that is an IP, instead that
//     IP, attributed.
func ClientIP(r *http.Request, cfg ClientIPConfig) (netip.Addr, bool) {
	ip := peerIP(r.RemoteAddr)
	if trusted(ip, cfg.TrustedProxies) {
		if lines := r.Header.Values(forwardedForHeader); len(lines) > 0 {
			last := lines[len(lines)-1]
			if forwarded, ok := parseIP(last[strings.LastIndex(last, ",")+1:]); ok {
				ip = forwarded
			}
		}
	}

	if proxySecretMatches(r.Header.Get(proxyProofHeader), cfg.ProxySecret) {
		if attributed, ok := parseIP(r.Header.Get(clientIPHeader)); ok {
			return attributed, true
		}
	}

	return ip, false
}

// peerIP is the IP of a request's RemoteAddr, which net/http sets to the peer's host and port. It is the zero
// Addr, which no trusted network contains, if RemoteAddr holds no IP, which only a handler called directly sees.
func peerIP(remoteAddr string) netip.Addr {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip, _ := parseIP(host)
	return ip
}

// parseIP parses s, ignoring surrounding spaces, as an IP without a zone, reading an IPv4-mapped IPv6 address as
// the IPv4 address.
func parseIP(s string) (netip.Addr, bool) {
	ip, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil || ip.Zone() != "" {
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}

func trusted(ip netip.Addr, networks []netip.Prefix) bool {
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// proxySecretMatches reports whether got is the configured secret want, which must not be empty. It compares the
// secrets' SHA-256 digests in constant time, so the time it takes reveals neither how much of got matches nor how
// long want is.
func proxySecretMatches(got, want string) bool {
	if want == "" {
		return false
	}
	gotDigest, wantDigest := sha256.Sum256([]byte(got)), sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(gotDigest[:], wantDigest[:]) == 1
}

// clientIPKey is the rate-limit key of a client IP: an IPv4 address itself, and an IPv6 address's /56 network.
func clientIPKey(ip netip.Addr) string {
	if ip.Is6() {
		return netip.PrefixFrom(ip, ipv6KeyBits).Masked().String()
	}
	return ip.String()
}
