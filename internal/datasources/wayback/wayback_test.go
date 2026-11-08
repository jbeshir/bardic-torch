package wayback

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testUserAgent is the user agent the tests' clients send.
const testUserAgent = "BardicTorchLinkChecker/1.0 (+https://bardictorch.org)"

// pageURL is the page the tests look up.
const pageURL = "https://songs.example/the-copper-kettle?verse=2&lang=en"

// closest is an availability answer whose closest copy is at copyURL with status and available.
func closest(copyURL, status string, available bool) string {
	availableJSON := "false"
	if available {
		availableJSON = "true"
	}

	return `{"url":"songs.example/the-copper-kettle","archived_snapshots":{"closest":{"status":"` + status +
		`","available":` + availableJSON + `,"url":"` + copyURL + `","timestamp":"20240101000000"}}}`
}

// archivedCopy is the copy the archive has of pageURL.
const archivedCopy = "http://web.archive.org/web/20240101000000/https://songs.example/the-copper-kettle"

// newClient returns a Client asking a fake API that answers status and body.
func newClient(t *testing.T, status int, body string) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	return New(Config{BaseURL: server.URL + "/wayback/available", UserAgent: testUserAgent}, server.Client())
}

func TestWayback_Available(t *testing.T) {
	t.Parallel()

	upgraded := strings.Replace(archivedCopy, "http://", "https://", 1)
	longCopy := archivedCopy + "/" + strings.Repeat("a", 2048)
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   *string
		// wantErr is whether the lookup fails with datasources.ErrArchiveUnavailable.
		wantErr bool
	}{
		{name: "available copy is upgraded to https", status: http.StatusOK,
			body: closest(archivedCopy, "200", true), want: &upgraded},
		{name: "https copy kept", status: http.StatusOK, body: closest(upgraded, "200", true), want: &upgraded},
		{name: "not available", status: http.StatusOK, body: closest(archivedCopy, "200", false)},
		{name: "copy archived with status 404", status: http.StatusOK, body: closest(archivedCopy, "404", true)},
		{name: "no snapshot", status: http.StatusOK, body: `{"url":"x","archived_snapshots":{}}`},
		{name: "copy on another host", status: http.StatusOK,
			body: closest("http://web.archive.org.evil.example/web/1/x", "200", true)},
		{name: "copy with another scheme", status: http.StatusOK,
			body: closest("javascript://web.archive.org/x", "200", true)},
		{name: "copy over 2048 bytes", status: http.StatusOK, body: closest(longCopy, "200", true)},
		{name: "malformed JSON", status: http.StatusOK, body: `{"archived_snapshots":`, wantErr: true},
		{name: "server error", status: http.StatusBadGateway, body: closest(archivedCopy, "200", true),
			wantErr: true},
		{name: "API not found", status: http.StatusNotFound, body: "", wantErr: true},
		{name: "oversize answer", status: http.StatusOK,
			body: closest(archivedCopy, "200", true) + strings.Repeat(" ", 64<<10), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := newClient(t, tc.status, tc.body).Available(t.Context(), pageURL)
			if tc.wantErr {
				require.ErrorIs(t, err, datasources.ErrArchiveUnavailable)
				assert.NotContains(t, err.Error(), "songs.example", "errors never quote the page's URL")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestWayback_UnreachableHidesURL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	client := New(Config{BaseURL: server.URL, UserAgent: testUserAgent}, &http.Client{})

	_, err := client.Available(t.Context(), pageURL)
	require.ErrorIs(t, err, datasources.ErrArchiveUnavailable)
	assert.NotContains(t, err.Error(), "songs.example")
}

func TestWayback_SendsUserAgentAndEscapedURL(t *testing.T) {
	t.Parallel()

	var gotPath, gotUserAgent string
	var gotQuery map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotUserAgent, gotQuery = r.URL.Path, r.UserAgent(), r.URL.Query()
		_, _ = w.Write([]byte(closest(archivedCopy, "200", true)))
	}))
	t.Cleanup(server.Close)

	client := New(Config{BaseURL: server.URL + "/wayback/available", UserAgent: testUserAgent}, server.Client())
	_, err := client.Available(t.Context(), pageURL)
	require.NoError(t, err)

	assert.Equal(t, "/wayback/available", gotPath)
	assert.Equal(t, testUserAgent, gotUserAgent)
	assert.Equal(t, map[string][]string{"url": {pageURL}}, gotQuery,
		"the page's URL is one query parameter, its own query escaped rather than merged")
}
