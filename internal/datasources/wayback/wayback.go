// Package wayback looks up archived copies of pages with the Wayback Machine's availability API.
package wayback

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
)

const (
	// availabilityURL is the availability API's endpoint.
	availabilityURL = "https://archive.org/wayback/available"
	// archiveHost is the only host an archived copy may be on.
	archiveHost = "web.archive.org"
	// maxAnswerBytes is the most of an answer read.
	maxAnswerBytes = 64 << 10
	// maxArchivedURLBytes is the longest archived URL kept, as links' archived_url column holds.
	maxArchivedURLBytes = 2048
)

var _ datasources.ArchiveLookup = (*Client)(nil)

// Client asks the availability API for the closest archived copy of a page.
type Client struct {
	baseURL   string
	userAgent string
	client    *http.Client
}

// New returns a Client for the Wayback Machine, sending userAgent.
func New(userAgent string) *Client {
	return &Client{baseURL: availabilityURL, userAgent: userAgent, client: &http.Client{Timeout: 10 * time.Second}}
}

// Available returns the https URL of the closest archived copy of pageURL, or "" if the archive has none it
// accepts: one archived with status 200, on web.archive.org, with a URL of at most 2048 bytes. Errors wrap
// datasources.ErrArchiveUnavailable and never contain pageURL.
func (c *Client) Available(ctx context.Context, pageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"?"+url.Values{"url": {pageURL}}.Encode(), nil)
	if err != nil {
		return "", datasources.ErrArchiveUnavailable
	}
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.client.Do(req)
	if err != nil {
		return "", datasources.ErrArchiveUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", datasources.ErrArchiveUnavailable
	}

	var answer struct {
		ArchivedSnapshots struct {
			Closest *snapshot `json:"closest"`
		} `json:"archived_snapshots"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAnswerBytes)).Decode(&answer); err != nil {
		return "", datasources.ErrArchiveUnavailable
	}

	return acceptedCopy(answer.ArchivedSnapshots.Closest), nil
}

// snapshot is the closest copy of an availability answer.
type snapshot struct {
	Available bool   `json:"available"`
	URL       string `json:"url"`
	Status    string `json:"status"`
}

// acceptedCopy returns the https URL of s, or "" if there is no copy or it is not accepted.
func acceptedCopy(s *snapshot) string {
	if s == nil || !s.Available || s.Status != "200" {
		return ""
	}
	archived, err := url.Parse(s.URL)
	if err != nil || (archived.Scheme != "http" && archived.Scheme != "https") || archived.User != nil ||
		!strings.EqualFold(archived.Host, archiveHost) {
		return ""
	}
	archived.Scheme, archived.Host = "https", archiveHost
	if copyURL := archived.String(); len(copyURL) <= maxArchivedURLBytes {
		return copyURL
	}

	return ""
}
