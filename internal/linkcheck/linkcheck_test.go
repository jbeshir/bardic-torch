package linkcheck

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"slices"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The hosts, URLs and addresses the tests use more than once.
const (
	kettleHost     = "kettle.example"
	kettle443      = kettleHost + ":443"
	kettle80       = kettleHost + ":80"
	kettlePage     = "http://" + kettleHost + "/page"
	publicV4       = "93.184.215.14"
	loopbackV4     = "127.0.0.1"
	mappedLoopback = "::ffff:127.0.0.1"
	unspecifiedV4  = "0.0.0.0"
	ietfV4         = "192.0.0.1"
	reservedV4     = "240.0.0.1"
)

// publicSiteAddr is the address the fake resolver gives every site host: a global unicast address the tests never
// reach, as the fake dialer connects to the test server instead.
var publicSiteAddr = netip.MustParseAddr(publicV4)

// fakeResolver resolves names from a table; a name it does not have does not exist.
type fakeResolver struct {
	mu    sync.Mutex
	addrs map[string][]netip.Addr
	// lookups counts the lookups of each name.
	lookups map[string]int
	// rebind, if set, is what a name resolves to from its second lookup on.
	rebind map[string][]netip.Addr
}

func (r *fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lookups == nil {
		r.lookups = map[string]int{}
	}
	r.lookups[host]++
	if again, ok := r.rebind[host]; ok && r.lookups[host] > 1 {
		return again, nil
	}
	addrs, ok := r.addrs[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}

	return addrs, nil
}

// recordingDial is a DialFunc that records each address dialled and connects to target instead, or fails with
// err if target is empty.
type recordingDial struct {
	mu     sync.Mutex
	dialed []string
	target string
	err    error
}

func (d *recordingDial) dial(ctx context.Context, _, address string) (net.Conn, error) {
	d.mu.Lock()
	d.dialed = append(d.dialed, address)
	d.mu.Unlock()
	if d.target == "" {
		return nil, d.err
	}

	return (&net.Dialer{}).DialContext(ctx, "tcp", d.target)
}

func (d *recordingDial) addresses() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	return append([]string(nil), d.dialed...)
}

// site is a test web server reached as http://kettle.example and http://harp.example, through the guard with the
// production allowlist: the fake resolver gives both names a public address and the fake dialer connects that
// address to the server.
type site struct {
	server   *httptest.Server
	resolver *fakeResolver
	dialer   *recordingDial
}

func newSite(t *testing.T, handler http.Handler) *site {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &site{
		server: server,
		resolver: &fakeResolver{addrs: map[string][]netip.Addr{
			kettleHost: {publicSiteAddr}, "harp.example": {publicSiteAddr},
		}},
		dialer: &recordingDial{target: server.Listener.Addr().String()},
	}
}

// checker returns a Checker reaching the site with the production allowlist and deadline.
func (s *site) checker(deadline time.Duration) *Checker {
	return New(Config{
		Resolver: s.resolver, Dial: s.dialer.dial,
		Deadline: deadline, HostInterval: 0,
	})
}

// statusHandler answers every request to path p with status, and serves the redirects the cases use.
func statusHandler(status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/moved":
			http.Redirect(w, r, "/page", http.StatusMovedPermanently)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/to-loopback":
			http.Redirect(w, r, "http://127.0.0.1/admin", http.StatusFound)
		case "/to-mysql":
			http.Redirect(w, r, "http://harp.example:3306/", http.StatusFound)
		case "/to-file":
			http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
		case "/to-credentials":
			http.Redirect(w, r, "http://reader@harp.example/", http.StatusFound)
		default:
			w.WriteHeader(status)
		}
	})
}

func TestClassify(t *testing.T) {
	t.Parallel()

	wrapped := func(err error) error { return &url.Error{Op: "Head", URL: "http://kettle.example/", Err: err} }
	for _, tc := range []struct {
		name   string
		status int
		err    error
		want   datasources.LinkOutcome
	}{
		{name: "200", status: http.StatusOK, want: datasources.LinkOutcomeOK},
		{name: "204", status: http.StatusNoContent, want: datasources.LinkOutcomeOK},
		{name: "404", status: http.StatusNotFound, want: datasources.LinkOutcomeGone},
		{name: "410", status: http.StatusGone, want: datasources.LinkOutcomeGone},
		{name: "500", status: http.StatusInternalServerError, want: datasources.LinkOutcomeUnreachable},
		{name: "503", status: http.StatusServiceUnavailable, want: datasources.LinkOutcomeUnreachable},
		{name: "401", status: http.StatusUnauthorized, want: datasources.LinkOutcomeBlocked},
		{name: "403", status: http.StatusForbidden, want: datasources.LinkOutcomeBlocked},
		{name: "429", status: http.StatusTooManyRequests, want: datasources.LinkOutcomeBlocked},
		{name: "999", status: 999, want: datasources.LinkOutcomeBlocked},
		{name: "400", status: http.StatusBadRequest, want: datasources.LinkOutcomeBlocked},
		{name: "301 without a location", status: http.StatusMovedPermanently, want: datasources.LinkOutcomeBlocked},
		{name: "timeout", err: wrapped(context.DeadlineExceeded), want: datasources.LinkOutcomeUnreachable},
		{name: "connection refused", err: wrapped(&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}),
			want: datasources.LinkOutcomeUnreachable},
		{name: "NXDOMAIN", err: wrapped(&net.DNSError{Err: "no such host", IsNotFound: true}),
			want: datasources.LinkOutcomeGone},
		{name: "DNS server failure", err: wrapped(&net.DNSError{Err: "server misbehaving", IsTemporary: true}),
			want: datasources.LinkOutcomeUnreachable},
		{name: "redirect loop", err: wrapped(errTooManyRedirects), want: datasources.LinkOutcomeBlocked},
		{name: "guard refusal", err: wrapped(fmt.Errorf("%w: port not allowed", ErrRefused)),
			want: datasources.LinkOutcomeRefused},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, Classify(tc.status, tc.err))
		})
	}
}

// TestChecker_Probe checks links end to end through the guard, with the production allowlist.
func TestChecker_Probe(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		status int
		url    string
		want   datasources.LinkOutcome
	}{
		{name: "200", status: http.StatusOK, url: kettlePage, want: datasources.LinkOutcomeOK},
		{name: "301 then 200", status: http.StatusOK, url: "http://kettle.example/moved",
			want: datasources.LinkOutcomeOK},
		{name: "404", status: http.StatusNotFound, url: kettlePage, want: datasources.LinkOutcomeGone},
		{name: "410", status: http.StatusGone, url: kettlePage, want: datasources.LinkOutcomeGone},
		{name: "500", status: http.StatusInternalServerError, url: kettlePage,
			want: datasources.LinkOutcomeUnreachable},
		{name: "403", status: http.StatusForbidden, url: kettlePage,
			want: datasources.LinkOutcomeBlocked},
		{name: "429", status: http.StatusTooManyRequests, url: kettlePage,
			want: datasources.LinkOutcomeBlocked},
		{name: "999", status: 999, url: kettlePage, want: datasources.LinkOutcomeBlocked},
		{name: "NXDOMAIN", status: http.StatusOK, url: "http://unknown.example/page",
			want: datasources.LinkOutcomeGone},
		{name: "explicit port 80 allowed", status: http.StatusOK, url: "http://kettle.example:80/page",
			want: datasources.LinkOutcomeOK},
		{name: "redirect loop", status: http.StatusOK, url: "http://kettle.example/loop",
			want: datasources.LinkOutcomeBlocked},
		{name: "port 8080", status: http.StatusOK, url: "http://kettle.example:8080/page",
			want: datasources.LinkOutcomeRefused},
		{name: "loopback literal", status: http.StatusOK, url: "http://127.0.0.1/page",
			want: datasources.LinkOutcomeRefused},
		{name: "ftp scheme", status: http.StatusOK, url: "ftp://kettle.example/page",
			want: datasources.LinkOutcomeRefused},
		{name: "credentials", status: http.StatusOK, url: "http://reader@kettle.example/page",
			want: datasources.LinkOutcomeRefused},
		{name: "redirect to loopback", status: http.StatusOK, url: "http://kettle.example/to-loopback",
			want: datasources.LinkOutcomeRefused},
		{name: "redirect to port 3306", status: http.StatusOK, url: "http://kettle.example/to-mysql",
			want: datasources.LinkOutcomeRefused},
		{name: "redirect to file", status: http.StatusOK, url: "http://kettle.example/to-file",
			want: datasources.LinkOutcomeRefused},
		{name: "redirect to credentials", status: http.StatusOK, url: "http://kettle.example/to-credentials",
			want: datasources.LinkOutcomeRefused},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := newSite(t, statusHandler(tc.status))
			assert.Equal(t, tc.want, s.checker(LinkDeadline).Probe(t.Context(), tc.url))
			for _, address := range s.dialer.addresses() {
				assert.Equal(t, publicSiteAddr.String(), netip.MustParseAddrPort(address).Addr().String(),
					"only the checked public address is dialled")
			}
		})
	}
}

func TestChecker_ConnectionRefusedIsUnreachable(t *testing.T) {
	t.Parallel()

	s := newSite(t, statusHandler(http.StatusOK))
	s.dialer.target = ""
	s.dialer.err = &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}

	assert.Equal(t, datasources.LinkOutcomeUnreachable, s.checker(LinkDeadline).Probe(t.Context(),
		kettlePage))
}

func TestChecker_HeadNon2xxRetriedWithGet(t *testing.T) {
	t.Parallel()

	for _, headStatus := range []int{http.StatusMethodNotAllowed, http.StatusForbidden, http.StatusNotFound,
		http.StatusInternalServerError} {
		t.Run(fmt.Sprint(headStatus), func(t *testing.T) {
			t.Parallel()

			var mu sync.Mutex
			var methods []string
			s := newSite(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				methods = append(methods, r.Method)
				mu.Unlock()
				if r.Method == http.MethodHead {
					w.WriteHeader(headStatus)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))

			assert.Equal(t, datasources.LinkOutcomeOK, s.checker(LinkDeadline).Probe(t.Context(),
				kettlePage))
			mu.Lock()
			defer mu.Unlock()
			assert.Equal(t, []string{http.MethodHead, http.MethodGet}, methods)
		})
	}
}

func TestChecker_HeadSuccessNeedsNoGet(t *testing.T) {
	t.Parallel()

	var gets int
	var mu sync.Mutex
	s := newSite(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			mu.Lock()
			gets++
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))

	assert.Equal(t, datasources.LinkOutcomeOK, s.checker(LinkDeadline).Probe(t.Context(), "http://kettle.example/"))
	mu.Lock()
	defer mu.Unlock()
	assert.Zero(t, gets)
}

// TestChecker_RedirectChainDeadline checks that one deadline covers a link's whole redirect chain, and HEAD and GET
// together: in the slow case each hop is well within the deadline, but the chain is not, so the chain is cut short
// and no GET follows. Only lateness-proof facts are asserted: a loaded host can only make the slow chain slower,
// and the fast case's deadline is far beyond any scheduling delay.
func TestChecker_RedirectChainDeadline(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		deadline time.Duration
		hop      time.Duration
		want     datasources.LinkOutcome
		// wantWholeChain is whether the site sees every HEAD of the chain.
		wantWholeChain bool
	}{
		{name: "fast chain within the deadline", deadline: 10 * time.Second, hop: 0,
			want: datasources.LinkOutcomeOK, wantWholeChain: true},
		// Six hops of a quarter of the deadline each take one and a half deadlines, so the sixth is never asked for.
		{name: "slow chain past the deadline", deadline: 300 * time.Millisecond, hop: 75 * time.Millisecond,
			want: datasources.LinkOutcomeUnreachable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var mu sync.Mutex
			requests := map[string]int{}
			s := newSite(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				requests[r.Method]++
				mu.Unlock()
				time.Sleep(tc.hop)
				var hop int
				_, _ = fmt.Sscanf(r.URL.Path, "/hop/%d", &hop)
				if hop < maxRedirects {
					http.Redirect(w, r, fmt.Sprintf("/hop/%d", hop+1), http.StatusFound)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))

			assert.Equal(t, tc.want, s.checker(tc.deadline).Probe(t.Context(), "http://kettle.example/hop/0"))
			mu.Lock()
			defer mu.Unlock()
			if tc.wantWholeChain {
				assert.Equal(t, maxRedirects+1, requests[http.MethodHead], "every hop of the chain")
			} else {
				assert.Less(t, requests[http.MethodHead], maxRedirects+1, "no hop is asked for after the deadline")
			}
			assert.Zero(t, requests[http.MethodGet], "the deadline covers HEAD and GET together")
		})
	}
}

func TestChecker_DiscardsBodyAfterLimit(t *testing.T) {
	t.Parallel()

	// The site answers GET with an endless body; the check succeeds only if it stops reading.
	s := newSite(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		chunk := make([]byte, 4096)
		for r.Context().Err() == nil {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))

	start := time.Now()
	assert.Equal(t, datasources.LinkOutcomeOK, s.checker(5*time.Second).Probe(t.Context(), "http://kettle.example/"))
	assert.Less(t, time.Since(start), 4*time.Second)
}

func TestChecker_SendsNoCookies(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var cookies, userAgents []string
	s := newSite(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		cookies = append(cookies, r.Header.Get("Cookie"))
		userAgents = append(userAgents, r.UserAgent())
		mu.Unlock()
		if r.URL.Path == "/set" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc", Secure: true, HttpOnly: true,
				SameSite: http.SameSiteStrictMode})
			http.Redirect(w, r, "/page", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))

	s.checker(LinkDeadline).Probe(t.Context(), "http://kettle.example/set")
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, cookies, 4, "HEAD and GET, each redirected once")
	for i := range cookies {
		assert.Empty(t, cookies[i])
		assert.Equal(t, UserAgent, userAgents[i])
	}
}

// arrivals is a site handler answering 200 that records when each request for each host arrived.
type arrivals struct {
	mu sync.Mutex
	at map[string][]time.Time
}

func (a *arrivals) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	if a.at == nil {
		a.at = map[string][]time.Time{}
	}
	a.at[r.Host] = append(a.at[r.Host], time.Now())
	a.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (a *arrivals) of(host string) []time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()

	return append([]time.Time(nil), a.at[host]...)
}

// TestChecker_SpacesRequestsToAHost checks that the k-th request to a host reaches it no sooner than k intervals
// after the first could be sent. Only that lower bound is asserted, which lateness cannot break.
func TestChecker_SpacesRequestsToAHost(t *testing.T) {
	t.Parallel()

	const interval = 100 * time.Millisecond
	site := &arrivals{}
	s := newSite(t, site)
	checker := New(Config{Resolver: s.resolver, Dial: s.dialer.dial, Deadline: LinkDeadline, HostInterval: interval})

	start := time.Now()
	var wg sync.WaitGroup
	outcomes := make([]datasources.LinkOutcome, 3)
	for i := range outcomes {
		wg.Go(func() { outcomes[i] = checker.Probe(t.Context(), kettlePage) })
	}
	wg.Wait()

	assert.Equal(t, []datasources.LinkOutcome{datasources.LinkOutcomeOK, datasources.LinkOutcomeOK,
		datasources.LinkOutcomeOK}, outcomes)
	got := site.of(kettleHost)
	require.Len(t, got, 3, "one HEAD per check")
	slices.SortFunc(got, time.Time.Compare)
	for k, at := range got {
		assert.GreaterOrEqual(t, at.Sub(start), time.Duration(k)*interval, "request %d to one host", k)
	}
}

// TestChecker_SpacingIsPerHost checks that a host's turn, an hour away, holds up neither another host nor anything
// but that host: the other host's check succeeds well within its deadline, and the host's own next check runs out
// of time waiting without a request.
func TestChecker_SpacingIsPerHost(t *testing.T) {
	t.Parallel()

	site := &arrivals{}
	s := newSite(t, site)
	checker := New(Config{Resolver: s.resolver, Dial: s.dialer.dial, Deadline: LinkDeadline, HostInterval: time.Hour})

	assert.Equal(t, datasources.LinkOutcomeOK, checker.Probe(t.Context(), kettlePage))
	assert.Equal(t, datasources.LinkOutcomeOK, checker.Probe(t.Context(), "http://harp.example/page"),
		"another host is not held up")

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	assert.Equal(t, datasources.LinkOutcomeUnreachable, checker.Probe(ctx, kettlePage), "the host waits its turn")
	assert.Len(t, site.of(kettleHost), 1)
	assert.Len(t, site.of("harp.example"), 1)
}

func TestPublicAddress(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		addr string
		want bool
	}{
		{addr: publicV4, want: true},
		{addr: "2606:4700::6810:84e5", want: true},
		{addr: "::ffff:93.184.215.14", want: true},
		{addr: "64:ff9b::5db8:d70e", want: true},
		{addr: "2002:5db8:d70e::1", want: true},
		{addr: loopbackV4}, {addr: "::1"}, {addr: "10.1.2.3"}, {addr: "172.16.0.1"}, {addr: "172.31.255.255"},
		{addr: "192.168.1.1"}, {addr: "169.254.169.254"}, {addr: "100.64.0.1"}, {addr: "fc00::1"}, {addr: "fd12::1"},
		{addr: "fe80::1"}, {addr: "fe80::1%eth0"}, {addr: mappedLoopback}, {addr: "::ffff:10.0.0.1"},
		{addr: unspecifiedV4}, {addr: "::"}, {addr: "64:ff9b::7f00:1"}, {addr: "64:ff9b::a00:1"},
		{addr: "2002:7f00:1::"}, {addr: "2002:c0a8:101::1"}, {addr: ietfV4}, {addr: "198.18.0.1"},
		{addr: "198.19.255.255"}, {addr: reservedV4}, {addr: "255.255.255.255"}, {addr: "224.0.0.1"},
		{addr: "ff02::1"}, {addr: "192.0.2.1"}, {addr: "198.51.100.1"}, {addr: "203.0.113.1"},
		{addr: "2001:db8::1"}, {addr: "2001::1"}, {addr: "::7f00:1"}, {addr: "64:ff9b:1::1"},
		{addr: "192.88.99.1"}, {addr: "100::1"},
	} {
		t.Run(tc.addr, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, PublicAddress(netip.MustParseAddr(tc.addr)))
		})
	}
}

// TestSafeDialer_RefusesInternalAddresses checks that the guard dials nothing for a host that resolves to an
// address that is not public, or on a port other than 80 and 443, and dials a public address on 443.
func TestSafeDialer_RefusesInternalAddresses(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		address string
		addr    string
		allowed bool
	}{
		{address: kettle443, addr: loopbackV4},
		{address: kettle443, addr: "::1"},
		{name: "10/8", address: kettle443, addr: "10.20.30.40"},
		{name: "172.16/12", address: kettle443, addr: "172.20.0.5"},
		{name: "192.168/16", address: kettle80, addr: "192.168.0.10"},
		{name: "169.254/16", address: kettle80, addr: "169.254.169.254"},
		{name: "100.64/10", address: kettle80, addr: "100.100.100.100"},
		{name: "fc00::/7", address: kettle443, addr: "fd00::5"},
		{name: "fe80::/10", address: kettle443, addr: "fe80::1"},
		{address: kettle443, addr: mappedLoopback},
		{address: kettle443, addr: unspecifiedV4},
		{name: "NAT64 of loopback", address: kettle443, addr: "64:ff9b::7f00:1"},
		{name: "6to4 of loopback", address: kettle443, addr: "2002:7f00:1::"},
		{address: kettle443, addr: ietfV4},
		{address: kettle443, addr: "198.18.0.1"},
		{address: kettle443, addr: reservedV4},
		{address: kettle443, addr: "255.255.255.255"},
		{name: "loopback literal", address: "127.0.0.1:80", addr: publicV4},
		{name: "port 3306", address: "kettle.example:3306", addr: publicV4},
		{name: "port 8080", address: "kettle.example:8080", addr: publicV4},
		{name: "no port", address: kettleHost, addr: publicV4},
		{name: "global address on 443", address: kettle443, addr: publicV4, allowed: true},
		{name: "global IPv6 on 80", address: kettle80, addr: "2606:4700::6810:84e5", allowed: true},
	} {
		t.Run(cmp.Or(tc.name, tc.addr), func(t *testing.T) {
			t.Parallel()

			dialer := &recordingDial{err: errors.New("dialled")}
			g := guard{
				resolver: &fakeResolver{addrs: map[string][]netip.Addr{kettleHost: {netip.MustParseAddr(tc.addr)}}},
				dial:     dialer.dial,
			}
			_, err := g.dialContext(t.Context(), "tcp", tc.address)
			if !tc.allowed {
				require.ErrorIs(t, err, ErrRefused)
				assert.Empty(t, dialer.addresses(), "nothing is dialled")
				return
			}
			require.NotErrorIs(t, err, ErrRefused)
			_, port, _ := net.SplitHostPort(tc.address)
			assert.Equal(t, []string{net.JoinHostPort(tc.addr, port)}, dialer.addresses())
		})
	}
}

// TestSafeDialer_SkipsInternalAddressesOfAHost checks that of a host's addresses only the public ones are dialled.
func TestSafeDialer_SkipsInternalAddressesOfAHost(t *testing.T) {
	t.Parallel()

	dialer := &recordingDial{err: errors.New("dialled")}
	g := guard{dial: dialer.dial, resolver: &fakeResolver{addrs: map[string][]netip.Addr{
		kettleHost: {netip.MustParseAddr("10.0.0.1"), publicSiteAddr, netip.MustParseAddr("::1")},
	}}}
	_, _ = g.dialContext(t.Context(), "tcp", kettle443)
	assert.Equal(t, []string{publicSiteAddr.String() + ":443"}, dialer.addresses())
}

// TestSafeDialer_DialsTheCheckedAddress checks that a name that resolves to a public address and then to a
// private one (DNS rebinding) is dialled at the address checked, once resolved.
func TestSafeDialer_DialsTheCheckedAddress(t *testing.T) {
	t.Parallel()

	resolver := &fakeResolver{
		addrs:  map[string][]netip.Addr{kettleHost: {publicSiteAddr}},
		rebind: map[string][]netip.Addr{kettleHost: {netip.MustParseAddr(loopbackV4)}},
	}
	dialer := &recordingDial{err: errors.New("dialled")}
	g := guard{resolver: resolver, dial: dialer.dial}

	_, _ = g.dialContext(t.Context(), "tcp", kettle443)
	_, err := g.dialContext(t.Context(), "tcp", kettle443)
	require.ErrorIs(t, err, ErrRefused, "the second lookup's private address is refused")
	assert.Equal(t, []string{publicSiteAddr.String() + ":443"}, dialer.addresses())
}

// TestGuardedDial_ChecksTheSocketAddress checks the production dialer's last check, on the address the socket
// connects to: a loopback listener is refused whatever the port, and an allowed address on a port other than 80
// and 443 is refused too.
func TestGuardedDial_ChecksTheSocketAddress(t *testing.T) {
	t.Parallel()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	for _, tc := range []struct {
		name  string
		allow func(netip.Addr) bool
	}{
		{name: "loopback", allow: PublicAddress},
		{name: "allowed address, port not allowed", allow: func(netip.Addr) bool { return true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conn, err := GuardedDial(tc.allow)(t.Context(), "tcp", listener.Addr().String())
			if conn != nil {
				_ = conn.Close()
			}
			require.ErrorIs(t, err, ErrRefused)
		})
	}
}
