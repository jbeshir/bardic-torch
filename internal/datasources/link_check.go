package datasources

import (
	"context"
	"errors"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// ErrArchiveUnavailable is returned when the archive could not be asked, or gave an answer that could not be used.
var ErrArchiveUnavailable = errors.New("archive lookup failed")

// LinkOutcome is what one check of a link found.
type LinkOutcome string

// The outcomes of a check.
const (
	// LinkOutcomeOK is a 2xx answer.
	LinkOutcomeOK LinkOutcome = "ok"
	// LinkOutcomeGone is a 404, a 410 or a host name that does not exist.
	LinkOutcomeGone LinkOutcome = "gone"
	// LinkOutcomeBlocked is a refusal by the site (401, 403, 429 and the like) or by the checker's guard.
	LinkOutcomeBlocked LinkOutcome = "blocked"
	// LinkOutcomeUnreachable is a timeout, a 5xx or a connection error: nothing is known about the link.
	LinkOutcomeUnreachable LinkOutcome = "unreachable"
)

// DueLink is a link due for a check or an archive lookup.
type DueLink struct {
	ID       uint64
	URL      string
	Liveness domain.Liveness
	// Failures is the number of consecutive 404, 410 or NXDOMAIN answers.
	Failures uint8
}

// LinkChecks stores the link checker's state.
type LinkChecks interface {
	// ListLinksDueForCheck returns the visible links never checked or last checked before checkedBefore.
	ListLinksDueForCheck(ctx context.Context, checkedBefore time.Time) ([]DueLink, error)
	// UpdateLinkCheck records the result of a check.
	UpdateLinkCheck(ctx context.Context, id uint64, liveness domain.Liveness, failures uint8, checkedAt time.Time) error
	// ListLinksDueForArchive returns the visible links without an archived copy whose archive was never
	// looked up or not since checkedBefore, dead links first.
	ListLinksDueForArchive(ctx context.Context, checkedBefore time.Time) ([]DueLink, error)
	// UpdateLinkArchive records an archive lookup, and the copy it found, if any.
	UpdateLinkArchive(ctx context.Context, id uint64, archivedURL string, checkedAt time.Time) error
}

// LinkProber checks whether a link works.
type LinkProber interface {
	Probe(ctx context.Context, rawURL string) LinkOutcome
}

// ArchiveLookup finds archived copies of pages.
type ArchiveLookup interface {
	// Available returns the URL of an archived copy of pageURL, or "" if there is none.
	Available(ctx context.Context, pageURL string) (string, error)
}
