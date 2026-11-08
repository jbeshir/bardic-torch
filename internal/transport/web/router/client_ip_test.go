package router

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The addresses of the client-IP tests.
const (
	directIP     = "198.51.100.7"
	directPeer   = directIP + ":4711"
	forwardedIP  = "1.2.3.4"
	proxyIP      = "172.17.0.1"
	ipv6ClientIP = "2001:db8::7"
	nginxEntry   = "192.0.2.80"
	attributedIP = "203.0.113.7"
	groupedKey   = "2001:db8:1:100::/56"
)

// TestClientIP checks how a request's client IP is derived: from the socket peer, from the last entry of the last
// X-Forwarded-For line only when the peer is a trusted proxy, and from X-Bardic-Client-IP, attributed, only with
// the configured proxy secret.
func TestClientIP(t *testing.T) {
	t.Parallel()

	const secret = "a-proxy-secret-for-tests"
	trustedOnly := ClientIPConfig{
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("::1/128")},
	}
	withSecret := trustedOnly
	withSecret.ProxySecret = secret

	for _, tc := range []struct {
		name           string
		cfg            ClientIPConfig
		peer           string
		forwardedFor   []string
		secret         string
		clientIP       string
		want           string
		wantAttributed bool
	}{
		{name: "direct peer", cfg: withSecret, peer: directPeer, want: directIP},
		{name: "untrusted peer with spoofed forwarded for", cfg: withSecret, peer: directPeer,
			forwardedFor: []string{forwardedIP}, want: directIP},
		{name: "trusted peer", cfg: trustedOnly, peer: limitedProxy, forwardedFor: []string{forwardedIP},
			want: forwardedIP},
		{name: "trusted peer appended to a client's entry", cfg: trustedOnly, peer: limitedProxy,
			forwardedFor: []string{"9.9.9.9, 1.2.3.4"}, want: forwardedIP},
		{name: "trusted peer, two lines", cfg: trustedOnly, peer: limitedProxy,
			forwardedFor: []string{"9.9.9.9, 8.8.8.8", "5.5.5.5,1.2.3.4"}, want: forwardedIP},
		{name: "trusted peer, spaces", cfg: trustedOnly, peer: limitedProxy,
			forwardedFor: []string{" 9.9.9.9 ,  1.2.3.4 "}, want: forwardedIP},
		{name: "trusted peer, garbage", cfg: trustedOnly, peer: limitedProxy,
			forwardedFor: []string{"1.2.3.4, not-an-ip"}, want: proxyIP},
		{name: "trusted peer, empty last entry", cfg: trustedOnly, peer: limitedProxy,
			forwardedFor: []string{"1.2.3.4,"}, want: proxyIP},
		{name: "trusted peer, entry with a port", cfg: trustedOnly, peer: limitedProxy,
			forwardedFor: []string{"1.2.3.4:80"}, want: proxyIP},
		{name: "trusted peer, entry with a zone", cfg: trustedOnly, peer: limitedProxy,
			forwardedFor: []string{"fe80::1%eth0"}, want: proxyIP},
		{name: "trusted peer without forwarded for", cfg: trustedOnly, peer: limitedProxy,
			want: proxyIP},
		{name: "trusted IPv6 peer", cfg: trustedOnly, peer: "[::1]:41000", forwardedFor: []string{ipv6ClientIP},
			want: ipv6ClientIP},
		{name: "mapped trusted peer", cfg: trustedOnly, peer: "[::ffff:172.17.0.1]:41000",
			forwardedFor: []string{forwardedIP}, want: forwardedIP},
		{name: "mapped peer", cfg: trustedOnly, peer: "[::ffff:1.2.3.4]:4711", want: forwardedIP},
		{name: "mapped forwarded entry", cfg: trustedOnly, peer: limitedProxy,
			forwardedFor: []string{"::ffff:1.2.3.4"}, want: forwardedIP},
		{name: "peer outside the trusted network", cfg: trustedOnly, peer: "172.32.0.1:41000",
			forwardedFor: []string{forwardedIP}, want: "172.32.0.1"},
		{name: "secret and client IP", cfg: withSecret, peer: limitedProxy,
			forwardedFor: []string{nginxEntry}, secret: secret, clientIP: attributedIP, want: attributedIP,
			wantAttributed: true},
		{name: "secret and client IP from any peer", cfg: withSecret, peer: directPeer, secret: secret,
			clientIP: attributedIP, want: attributedIP, wantAttributed: true},
		{name: "secret and mapped client IP", cfg: withSecret, peer: limitedProxy, secret: secret,
			clientIP: "::ffff:203.0.113.7", want: attributedIP, wantAttributed: true},
		{name: "secret and IPv6 client IP", cfg: withSecret, peer: limitedProxy, secret: secret,
			clientIP: ipv6ClientIP, want: ipv6ClientIP, wantAttributed: true},
		{name: "wrong secret", cfg: withSecret, peer: limitedProxy, forwardedFor: []string{nginxEntry},
			secret: "a-proxy-secret-for-tests-", clientIP: attributedIP, want: nginxEntry},
		{name: "secret of the wrong case", cfg: withSecret, peer: limitedProxy,
			forwardedFor: []string{nginxEntry}, secret: strings.ToUpper(secret), clientIP: attributedIP,
			want: nginxEntry},
		{name: "missing secret", cfg: withSecret, peer: limitedProxy, forwardedFor: []string{nginxEntry},
			clientIP: attributedIP, want: nginxEntry},
		{name: "no secret configured, none sent", cfg: trustedOnly, peer: limitedProxy,
			forwardedFor: []string{nginxEntry}, clientIP: attributedIP, want: nginxEntry},
		{name: "no secret configured, one sent", cfg: trustedOnly, peer: limitedProxy,
			forwardedFor: []string{nginxEntry}, secret: secret, clientIP: attributedIP, want: nginxEntry},
		{name: "secret and malformed client IP", cfg: withSecret, peer: limitedProxy,
			forwardedFor: []string{nginxEntry}, secret: secret, clientIP: "203.0.113.7, 1.1.1.1",
			want: nginxEntry},
		{name: "secret and no client IP", cfg: withSecret, peer: directPeer, secret: secret,
			want: directIP},
		{name: "peer without a port", cfg: withSecret, peer: directIP, want: directIP},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, routingContact, nil)
			req.RemoteAddr = tc.peer
			for _, line := range tc.forwardedFor {
				req.Header.Add(forwardedForHeader, line)
			}
			if tc.secret != "" {
				req.Header.Set(proxyProofHeader, tc.secret)
			}
			if tc.clientIP != "" {
				req.Header.Set(clientIPHeader, tc.clientIP)
			}

			ip, attributed := ClientIP(req, tc.cfg)
			assert.Equal(t, netip.MustParseAddr(tc.want), ip)
			assert.Equal(t, tc.wantAttributed, attributed)
		})
	}
}

// TestClientIP_UnparsablePeer checks that a peer address with no IP, which only a handler called directly sees,
// is never trusted.
func TestClientIP_UnparsablePeer(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, routingContact, nil)
	req.RemoteAddr = "pipe"
	req.Header.Set(forwardedForHeader, forwardedIP)
	ip, attributed := ClientIP(req, ClientIPConfig{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}})
	assert.False(t, ip.IsValid())
	assert.False(t, attributed)
}

// TestProxySecretMatches checks the proxy secret comparison: only the exact configured secret matches, and an
// empty configured secret matches nothing, not even an empty header. The comparison is of SHA-256 digests with
// crypto/subtle, so its time depends on neither the matching prefix nor the lengths.
func TestProxySecretMatches(t *testing.T) {
	t.Parallel()

	const secret = "a-proxy-secret-for-tests"
	for _, tc := range []struct {
		name      string
		got, want string
		match     bool
	}{
		{name: "equal", got: secret, want: secret, match: true},
		{name: "last byte differs", got: "a-proxy-secret-for-testz", want: secret},
		{name: "first byte differs", got: "b-proxy-secret-for-tests", want: secret},
		{name: "prefix", got: secret[:len(secret)-1], want: secret},
		{name: "longer", got: secret + "s", want: secret},
		{name: "missing", got: "", want: secret},
		{name: "none configured", got: "", want: ""},
		{name: "none configured, one sent", got: secret, want: ""},
	} {
		assert.Equalf(t, tc.match, proxySecretMatches(tc.got, tc.want), "%s", tc.name)
	}
}

// TestClientIPKey checks that IPv4 addresses are keys of their own and IPv6 addresses are grouped by /56.
func TestClientIPKey(t *testing.T) {
	t.Parallel()

	for ip, want := range map[string]string{
		attributedIP:                  attributedIP,
		"2001:db8:1:100::1":           groupedKey,
		"2001:db8:1:1ff:ffff::9":      groupedKey,
		"2001:db8:1:200::1":           "2001:db8:1:200::/56",
		"2001:db8:1:ff00:1:2:3:4":     "2001:db8:1:ff00::/56",
		"fd00:aaaa:bbbb:cc11::dddd":   "fd00:aaaa:bbbb:cc00::/56",
		"2001:0db8:0001:0100:0:0:0:0": groupedKey,
	} {
		assert.Equalf(t, want, clientIPKey(netip.MustParseAddr(ip)), "key of %s", ip)
	}
}

// TestClientIP_NeverLogs checks that nothing a contact request's client IP is derived from, nor the IP itself,
// reaches a log line, whether the request is attributed, unattributed or refused.
func TestClientIP_NeverLogs(t *testing.T) {
	t.Parallel()

	f := newLimitFixture()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	send := func(attributed bool) {
		req := httptest.NewRequestWithContext(domain.ContextWithLogger(t.Context(), logger), http.MethodPost,
			routingContact, strings.NewReader(`{"kind":"general","email":"a@b","message":"Hello.","turnstile_token":"t"}`))
		req.Header.Set("Content-Type", jsonContentType)
		req.RemoteAddr = "172.17.0.99:41000"
		req.Header.Add(forwardedForHeader, "198.51.100.66, 192.0.2.66")
		if attributed {
			req.Header.Set(proxyProofHeader, limitedProxySecret)
			req.Header.Set(clientIPHeader, "203.0.113.66")
		}
		f.handler.ServeHTTP(httptest.NewRecorder(), req)
	}
	for range 2 {
		send(true)
		send(false)
	}

	records := logs.String()
	require.Contains(t, records, rateLimitedLog, "nothing was refused, so this proves less")
	require.Contains(t, records, "unattributed", "nothing was warned, so this proves less")
	for _, leaked := range []string{limitedProxySecret, "203.0.113", "198.51.100", "192.0.2", "172.17.0"} {
		assert.NotContains(t, records, leaked)
	}
}
