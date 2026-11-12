package linkcheck

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
)

// The production settings of a Prober.
const (
	// UserAgent is the user agent the prober sends.
	UserAgent = "BardicTorchLinkChecker/1.0 (+https://bardictorch.org)"
	// linkDeadline bounds the check of one link: both requests and every redirect of each.
	linkDeadline = 15 * time.Second
	// hostInterval is the least time between two requests to one host.
	hostInterval = time.Second
	maxRedirects = 5
	// maxBodyBytes is the most of a body read before the connection is closed.
	maxBodyBytes = 64 << 10
)

var errTooManyRedirects = errors.New("too many redirects")

var _ datasources.LinkProber = (*Prober)(nil)

// Prober checks links with HEAD and, if that does not give a 2xx, GET, following up to five redirects. It uses no
// proxy, keeps no cookies and sends no credentials. It checks one link at a time.
type Prober struct {
	client   *http.Client
	deadline time.Duration
	interval time.Duration
	// last is when each host was last requested.
	last map[string]time.Time
}

// New returns a Prober that only connects to public addresses on ports 80 and 443.
func New() *Prober {
	return newProber(checkAddress, hostInterval)
}

func newProber(check func(netip.AddrPort) error, interval time.Duration) *Prober {
	p := &Prober{deadline: linkDeadline, interval: interval, last: map[string]time.Time{}}
	transport := &http.Transport{
		DialContext:         guardedDialer(check).DialContext,
		DisableKeepAlives:   true,
		DisableCompression:  true,
		TLSHandshakeTimeout: linkDeadline,
	}
	p.client = &http.Client{
		Transport: spaced{next: transport, p: p},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return errTooManyRedirects
			}

			return checkURL(req.URL)
		},
	}

	return p
}

// Probe checks whether rawURL works, within the prober's deadline.
func (p *Prober) Probe(ctx context.Context, rawURL string) datasources.LinkOutcome {
	ctx, cancel := context.WithTimeout(ctx, p.deadline)
	defer cancel()

	u, err := url.Parse(rawURL)
	if err == nil {
		err = checkURL(u)
	}
	if err != nil {
		return Classify(0, ErrRefused)
	}

	outcome := Classify(p.fetch(ctx, http.MethodHead, rawURL))
	if outcome == datasources.LinkOutcomeOK || ctx.Err() != nil {
		return outcome
	}

	return Classify(p.fetch(ctx, http.MethodGet, rawURL))
}

// fetch makes one request, following redirects, and returns the final status.
func (p *Prober) fetch(ctx context.Context, method, rawURL string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return 0, ErrRefused
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBodyBytes))
	_ = resp.Body.Close()

	return resp.StatusCode, nil
}

// Classify returns the outcome of a request that ended with status, or failed with err.
func Classify(status int, err error) datasources.LinkOutcome {
	var dnsErr *net.DNSError
	switch {
	case errors.Is(err, ErrRefused), errors.Is(err, errTooManyRedirects):
		return datasources.LinkOutcomeBlocked
	case errors.As(err, &dnsErr) && dnsErr.IsNotFound:
		return datasources.LinkOutcomeGone
	case err != nil, status >= 500:
		return datasources.LinkOutcomeUnreachable
	case status >= 200 && status < 300:
		return datasources.LinkOutcomeOK
	case status == http.StatusNotFound || status == http.StatusGone:
		return datasources.LinkOutcomeGone
	default:
		// 401, 403, 429 and every other answer that says neither that the page works nor that it is gone: the
		// site is refusing the checker.
		return datasources.LinkOutcomeBlocked
	}
}

// spaced waits for the host's turn before each request, redirects included.
type spaced struct {
	next http.RoundTripper
	p    *Prober
}

func (s spaced) RoundTrip(req *http.Request) (*http.Response, error) {
	host := strings.ToLower(req.URL.Hostname())
	if wait := time.Until(s.p.last[host].Add(s.p.interval)); wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		}
	}
	s.p.last[host] = time.Now()

	return s.next.RoundTrip(req)
}
