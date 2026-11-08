// Package wayback looks up archived copies of pages with the Wayback Machine's availability API.
package wayback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/datasources"
)

// maxResponseBytes is the largest availability answer read; a larger one is a failure on the archive's side.
const maxResponseBytes = 64 << 10

// maxArchivedURLBytes is the longest archived copy's URL kept, as links' archived_url column holds.
const maxArchivedURLBytes = 2048

// archiveHost is the only host an archived copy may be on.
const archiveHost = "web.archive.org"

var _ datasources.ArchiveLookup = (*Client)(nil)

// Config is the configuration of a Client.
type Config struct {
	// BaseURL is the availability API's endpoint, https://archive.org/wayback/available in production.
	BaseURL string
	// UserAgent is sent with every request.
	UserAgent string
}

// Client asks the availability API for the closest archived copy of a page.
type Client struct {
	config Config
	client *http.Client
}

// New returns a Client that asks the API at config.BaseURL through client. The client's timeout bounds a lookup;
// it should use no proxy from the environment and keep no cookies.
func New(config Config, client *http.Client) *Client {
	return &Client{config: config, client: client}
}

// availability is the part of an availability answer the lookup reads.
type availability struct {
	ArchivedSnapshots struct {
		Closest *struct {
			Available bool   `json:"available"`
			URL       string `json:"url"`
			Status    string `json:"status"`
		} `json:"closest"`
	} `json:"archived_snapshots"`
}

// Available returns the URL of the closest archived copy of pageURL, upgraded to https, or nil if the archive has
// none it accepts: a copy must be available, have been archived with status 200, be on web.archive.org and have a
// URL of at most 2048 bytes. Errors wrap datasources.ErrArchiveUnavailable and never quote pageURL.
func (c *Client) Available(ctx context.Context, pageURL string) (*string, error) {
	endpoint, err := url.Parse(c.config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("%w: the availability API's URL does not parse", datasources.ErrArchiveUnavailable)
	}
	endpoint.RawQuery = url.Values{"url": {pageURL}}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: building the request", datasources.ErrArchiveUnavailable)
	}
	req.Header.Set("User-Agent", c.config.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", datasources.ErrArchiveUnavailable, withoutURL(err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", datasources.ErrArchiveUnavailable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: reading the answer: %w", datasources.ErrArchiveUnavailable, withoutURL(err))
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("%w: the answer is over %d bytes", datasources.ErrArchiveUnavailable, maxResponseBytes)
	}

	var answer availability
	if err := json.Unmarshal(body, &answer); err != nil {
		return nil, fmt.Errorf("%w: the answer is not JSON", datasources.ErrArchiveUnavailable)
	}

	return acceptedCopy(answer), nil
}

// acceptedCopy returns the https URL of the answer's closest copy, or nil if there is none or it is not accepted.
func acceptedCopy(answer availability) *string {
	closest := answer.ArchivedSnapshots.Closest
	if closest == nil || !closest.Available || closest.Status != "200" {
		return nil
	}
	archived, err := url.Parse(closest.URL)
	if err != nil || (archived.Scheme != "http" && archived.Scheme != "https") || archived.User != nil ||
		!strings.EqualFold(archived.Host, archiveHost) {
		return nil
	}
	archived.Scheme = "https"
	archived.Host = archiveHost
	copyURL := archived.String()
	if len(copyURL) > maxArchivedURLBytes {
		return nil
	}

	return &copyURL
}

// withoutURL returns err without the request URL a *url.Error quotes, which holds the page's URL.
func withoutURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}

	return err
}
