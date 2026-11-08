package linkcheck

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
)

// The production settings of a Checker.
const (
	// UserAgent is the user agent the checker sends.
	UserAgent = "BardicTorchLinkChecker/1.0 (+https://bardictorch.org)"
	// LinkDeadline bounds the check of one link: both requests and every redirect of each.
	LinkDeadline = 15 * time.Second
	// HostInterval is the least time between two requests to one host.
	HostInterval = time.Second
)

// maxRedirects is the most redirects a request follows; a longer chain is treated as a redirect loop.
const maxRedirects = 5

// maxBodyBytes is the most of a body the checker reads before closing the connection.
const maxBodyBytes = 64 << 10

// errTooManyRedirects is a redirect chain longer than maxRedirects, usually a loop a site makes for clients
// without cookies, which the checker never keeps.
var errTooManyRedirects = errors.New("too many redirects")

var _ datasources.LinkProber = (*Checker)(nil)

// Config is the configuration of a Checker. Every field is required.
type Config struct {
	// UserAgent is sent with every request.
	UserAgent string
	// AllowAddress reports whether a connection may go to an address: PublicAddress in production.
	AllowAddress func(netip.Addr) bool
	// Resolver looks up host names: net.DefaultResolver in production.
	Resolver Resolver
	// Dial connects to an address AllowAddress accepted: GuardedDial(AllowAddress) in production.
	Dial DialFunc
	// Deadline bounds the check of one link: LinkDeadline in production.
	Deadline time.Duration
	// HostInterval is the least time between two requests to one host: HostInterval in production.
	HostInterval time.Duration
}

// Checker checks links, as HEAD and if that does not succeed GET, following up to five redirects. It uses no proxy,
// keeps no cookies, sends no credentials, and reads at most 64 KiB of a body. It is safe for concurrent use.
type Checker struct {
	client    *http.Client
	userAgent string
	deadline  time.Duration
}

// New returns a Checker with config.
func New(config Config) *Checker {
	g := guard{allow: config.AllowAddress, resolver: config.Resolver, dial: config.Dial}
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            g.dialContext,
		DisableKeepAlives:      true,
		DisableCompression:     true,
		TLSHandshakeTimeout:    config.Deadline,
		MaxResponseHeaderBytes: maxBodyBytes,
	}
	spacer := &hostSpacer{interval: config.HostInterval, next: map[string]time.Time{}}

	return &Checker{
		client: &http.Client{
			Transport:     spacedTransport{next: transport, spacer: spacer},
			CheckRedirect: checkRedirect,
		},
		userAgent: config.UserAgent,
		deadline:  config.Deadline,
	}
}

// Probe checks whether rawURL works, within the checker's deadline: HEAD first, then GET if HEAD did not succeed,
// as many servers answer HEAD wrongly. A URL the guard refuses is never requested.
func (c *Checker) Probe(ctx context.Context, rawURL string) datasources.LinkOutcome {
	ctx, cancel := context.WithTimeout(ctx, c.deadline)
	defer cancel()

	u, err := url.Parse(rawURL)
	if err != nil {
		return datasources.LinkOutcomeRefused
	}
	if err := allowedURL(u); err != nil {
		return Classify(0, err)
	}

	outcome := Classify(c.fetch(ctx, http.MethodHead, u))
	if outcome == datasources.LinkOutcomeOK || outcome == datasources.LinkOutcomeRefused || ctx.Err() != nil {
		return outcome
	}

	return Classify(c.fetch(ctx, http.MethodGet, u))
}

// fetch makes one request, following redirects, and returns the final status.
func (c *Checker) fetch(ctx context.Context, method string, u *url.URL) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return 0, fmt.Errorf("%w: building the request", ErrRefused)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "*/*")

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBodyBytes))
	_ = resp.Body.Close()

	return resp.StatusCode, nil
}

// Classify returns the outcome of a check whose last request ended with status, or failed with err.
func Classify(status int, err error) datasources.LinkOutcome {
	if err != nil {
		return classifyError(err)
	}
	switch {
	case status >= 200 && status < 300:
		return datasources.LinkOutcomeOK
	case status == http.StatusNotFound || status == http.StatusGone:
		return datasources.LinkOutcomeGone
	case status >= 500 && status < 600:
		return datasources.LinkOutcomeUnreachable
	default:
		// 401, 403, 429, LinkedIn's 999, and every other answer that says neither that the page works nor that it
		// is gone: the site is refusing the checker, so whether the link works is not known.
		return datasources.LinkOutcomeBlocked
	}
}

// classifyError returns the outcome of a request that failed with err.
func classifyError(err error) datasources.LinkOutcome {
	var dnsErr *net.DNSError
	switch {
	case errors.Is(err, ErrRefused):
		return datasources.LinkOutcomeRefused
	case errors.Is(err, errTooManyRedirects):
		return datasources.LinkOutcomeBlocked
	case errors.As(err, &dnsErr) && dnsErr.IsNotFound:
		return datasources.LinkOutcomeGone
	default:
		return datasources.LinkOutcomeUnreachable
	}
}

// allowedURL returns an error wrapping ErrRefused unless u is an http or https URL with a host and no credentials.
func allowedURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: scheme not allowed", ErrRefused)
	}
	if u.User != nil {
		return fmt.Errorf("%w: credentials in the URL", ErrRefused)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("%w: no host", ErrRefused)
	}

	return nil
}

// checkRedirect is the client's redirect policy: at most maxRedirects, each to a URL the guard allows.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > maxRedirects {
		return errTooManyRedirects
	}

	return allowedURL(req.URL)
}

// spacedTransport waits for the host's turn before each request, redirects included.
type spacedTransport struct {
	next   http.RoundTripper
	spacer *hostSpacer
}

func (t spacedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.spacer.wait(req.Context(), req.URL.Hostname()); err != nil {
		return nil, err
	}

	return t.next.RoundTrip(req)
}

// hostSpacer spaces requests to each host at least interval apart.
type hostSpacer struct {
	interval time.Duration
	mu       sync.Mutex
	// next is when each host may next be requested.
	next map[string]time.Time
}

// wait reserves host's next turn and waits for it, or for ctx to end.
func (s *hostSpacer) wait(ctx context.Context, host string) error {
	host = strings.ToLower(host)
	s.mu.Lock()
	now := time.Now()
	at := now
	if next, ok := s.next[host]; ok && next.After(now) {
		at = next
	}
	s.next[host] = at.Add(s.interval)
	s.mu.Unlock()

	delay := at.Sub(now)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
