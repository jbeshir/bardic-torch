package main

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/datasources/wayback"
	"github.com/jbeshir/bardic-torch/internal/linkcheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The flags the cases give more than once.
const (
	limitFlag       = "--limit"
	maxDurationFlag = "--max-duration"
)

func TestParseOptions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
		want options
		// wantErr is whether the command line is refused with ErrUsage.
		wantErr bool
	}{
		{name: "defaults", want: options{limit: 400, archiveLimit: 200, maxDuration: 20 * time.Minute}},
		{name: "every flag", args: []string{limitFlag, "50", "--archive-limit=0", maxDurationFlag, "5m"},
			want: options{limit: 50, archiveLimit: 0, maxDuration: 5 * time.Minute}},
		{name: "zero limit", args: []string{limitFlag, "0"}, wantErr: true},
		{name: "limit too large", args: []string{limitFlag, "10001"}, wantErr: true},
		{name: "limit beyond int32", args: []string{limitFlag, "4294967296"}, wantErr: true},
		{name: "negative archive limit", args: []string{"--archive-limit", "-1"}, wantErr: true},
		{name: "duration too short", args: []string{maxDurationFlag, "30s"}, wantErr: true},
		{name: "duration too long", args: []string{maxDurationFlag, "7h"}, wantErr: true},
		{name: "unknown flag", args: []string{"--url", "x"}, wantErr: true},
		{name: "argument", args: []string{"now"}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseOptions(tc.args)
			if tc.wantErr {
				require.ErrorIs(t, err, ErrUsage)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestOptions_Request(t *testing.T) {
	t.Parallel()

	assert.Equal(t, command.CheckLinksRequest{
		Limit: 400, ArchiveLimit: 200, Workers: 4, MaxDuration: 20 * time.Minute, ArchiveSpacing: time.Second,
		LockPingInterval: time.Minute,
	}, options{limit: 400, archiveLimit: 200, maxDuration: 20 * time.Minute}.request())
}

func TestNewLogger(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	logger, err := newLogger("", &out)
	require.NoError(t, err)
	logger.Debug("hidden")
	logger.Info("shown")
	assert.NotContains(t, out.String(), "hidden", "the default level is INFO")
	assert.Contains(t, out.String(), `"msg":"shown"`)

	_, err = newLogger("LOUD", &out)
	require.Error(t, err)
}

func TestRun_RefusesBadDatabaseURL(t *testing.T) {
	t.Parallel()

	err := run(t.Context(), nil, "postgres://nowhere/db")
	require.Error(t, err)
	err = run(t.Context(), []string{limitFlag, "0"}, "")
	require.ErrorIs(t, err, ErrUsage)
}

// TestNewCheckLinks_RefusesInternalAddresses checks the production wiring of the checker's address guard, with no
// external network: neither the checker nor its dialer reaches a loopback server, even on an allowed port, and the
// archive client asks the Wayback Machine's own API through a client with no proxy.
func TestNewCheckLinks_RefusesInternalAddresses(t *testing.T) {
	t.Parallel()

	var connections, requests atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	t.Cleanup(server.Close)

	run := newCheckLinks(mysql.New(nil))
	assert.NotEqual(t, datasources.LinkOutcomeOK, run.Prober.Probe(t.Context(), server.URL))
	assert.Equal(t, datasources.LinkOutcomeRefused, run.Prober.Probe(t.Context(), "http://127.0.0.1:443/"),
		"refused before dialing")

	// The checker's guard refuses loopback before it dials, so only the dialer itself shows that it checks the
	// socket's address too.
	dial := proberConfig().Dial
	for _, address := range []string{server.Listener.Addr().String(), "127.0.0.1:443"} {
		conn, err := dial(t.Context(), "tcp", address)
		if conn != nil {
			_ = conn.Close()
		}
		require.ErrorIs(t, err, linkcheck.ErrRefused, address)
	}
	assert.Zero(t, connections.Load(), "connections the loopback server accepted")
	assert.Zero(t, requests.Load(), "requests the loopback server answered")

	assert.Equal(t, wayback.New(wayback.Config{
		BaseURL: "https://archive.org/wayback/available", UserAgent: linkcheck.UserAgent,
	}, &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{}}), run.Archive)
}
