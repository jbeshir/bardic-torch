package linkcheck

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/datasources"
)

func TestCheckAddress(t *testing.T) {
	tests := map[string]bool{
		"93.184.216.34:80":         true,
		"93.184.216.34:443":        true,
		"[2606:2800:220:1::1]:443": true,
		"93.184.216.34:8080":       false,
		"10.0.0.1:80":              false,
		"172.16.0.1:443":           false,
		"192.168.1.1:80":           false,
		"127.0.0.1:80":             false,
		"[::1]:80":                 false,
		"169.254.169.254:80":       false,
		"100.64.0.1:80":            false,
		"0.0.0.0:80":               false,
		"224.0.0.1:80":             false,
		"[fe80::1]:80":             false,
		"[fd00::1]:80":             false,
		"[::ffff:10.0.0.1]:80":     false,
		"[64:ff9b::a00:1]:80":      false,
		"[2002:a00:1::]:80":        false,
	}
	for address, allowed := range tests {
		err := checkAddress(netip.MustParseAddrPort(address))
		assert.Equal(t, allowed, err == nil, address)
		if !allowed {
			assert.ErrorIs(t, err, ErrRefused, address)
		}
	}
}

func TestCheckURL(t *testing.T) {
	tests := map[string]bool{
		"https://example.com/a":    true,
		"http://example.com":       true,
		"https://u:p@example.com/": false,
		"https://u@example.com/":   false,
		"ftp://example.com/":       false,
		"file:///etc/passwd":       false,
	}
	for raw, allowed := range tests {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		assert.Equal(t, allowed, checkURL(u) == nil, raw)
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name   string
		status int
		err    error
		want   datasources.LinkOutcome
	}{
		{"2xx", 204, nil, datasources.LinkOutcomeOK},
		{"404", 404, nil, datasources.LinkOutcomeGone},
		{"410", 410, nil, datasources.LinkOutcomeGone},
		{"NXDOMAIN", 0, &net.DNSError{IsNotFound: true}, datasources.LinkOutcomeGone},
		{"401", 401, nil, datasources.LinkOutcomeBlocked},
		{"403", 403, nil, datasources.LinkOutcomeBlocked},
		{"429", 429, nil, datasources.LinkOutcomeBlocked},
		{"guard", 0, ErrRefused, datasources.LinkOutcomeBlocked},
		{"redirect loop", 0, errTooManyRedirects, datasources.LinkOutcomeBlocked},
		{"500", 500, nil, datasources.LinkOutcomeUnreachable},
		{"503", 503, nil, datasources.LinkOutcomeUnreachable},
		{"timeout", 0, context.DeadlineExceeded, datasources.LinkOutcomeUnreachable},
		{"connection error", 0, errors.New("connection refused"), datasources.LinkOutcomeUnreachable},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, Classify(tt.status, tt.err), tt.name)
	}
}

func TestProbeRefusesLoopbackByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the guard let a request through to a loopback address")
	}))
	defer srv.Close()

	assert.Equal(t, datasources.LinkOutcomeBlocked, New().Probe(context.Background(), srv.URL))
	assert.Equal(t, datasources.LinkOutcomeBlocked, New().Probe(context.Background(), "https://u:p@example.com/"))
}

func TestProbe(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/head-refused", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/gone", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusGone) })
	mux.HandleFunc("/to-private", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://10.0.0.1/", http.StatusFound)
	})
	mux.HandleFunc("/loop", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop", http.StatusFound)
	})
	mux.HandleFunc("/slow", func(http.ResponseWriter, *http.Request) { time.Sleep(200 * time.Millisecond) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Allow the test server, and what the production guard allows beside it.
	server := netip.MustParseAddrPort(srv.Listener.Addr().String())
	prober := newProber(func(addr netip.AddrPort) error {
		if addr == server {
			return nil
		}

		return checkAddress(addr)
	}, 0)
	prober.deadline = 50 * time.Millisecond

	tests := map[string]datasources.LinkOutcome{
		"/head-refused": datasources.LinkOutcomeOK,
		"/gone":         datasources.LinkOutcomeGone,
		"/to-private":   datasources.LinkOutcomeBlocked,
		"/loop":         datasources.LinkOutcomeBlocked,
		"/slow":         datasources.LinkOutcomeUnreachable,
	}
	for path, want := range tests {
		assert.Equal(t, want, prober.Probe(context.Background(), srv.URL+path), path)
	}
}
