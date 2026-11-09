package datasources

import (
	"context"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// LinkCheckTarget is a visible link due for a liveness check, as the checker read it.
type LinkCheckTarget struct {
	ID       int64
	URL      string
	Liveness domain.Liveness
	// Failures is the number of consecutive definite failures recorded.
	Failures uint8
	// LastCheckedAt is nil for a link never checked. It is the stamp UpdateLinkCheck compares.
	LastCheckedAt *time.Time
}

// LinkCheckResult is the outcome of checking a link, to be recorded if the link is still as the checker read it.
type LinkCheckResult struct {
	ID       int64
	Liveness domain.Liveness
	Failures uint8
	// CheckedAt is the new check time.
	CheckedAt time.Time
	// ExpectedLastCheckedAt is the LinkCheckTarget's LastCheckedAt.
	ExpectedLastCheckedAt *time.Time
	// ProbedURL is the LinkCheckTarget's URL, the one checked.
	ProbedURL string
}

// LinkArchiveTarget is a visible link whose archived copy is due to be looked up.
type LinkArchiveTarget struct {
	ID  int64
	URL string
}

// LinkArchiveResult is the outcome of looking up a link's archived copy.
type LinkArchiveResult struct {
	ID int64
	// ArchivedURL is the copy found, or nil if none was.
	ArchivedURL *string
	CheckedAt   time.Time
}

// LinkChecker is the store of the link checker. It never reads or writes a hidden link.
type LinkChecker interface {
	// ListLinksDueForCheck returns up to limit visible links never checked or last checked before checkedBefore:
	// the never checked first, then the longest unchecked, then by ID.
	ListLinksDueForCheck(ctx context.Context, checkedBefore time.Time, limit int32) ([]LinkCheckTarget, error)
	// UpdateLinkCheck records a result, only if the link is still visible and its URL and last check time are still
	// the ones the checker read. It returns false if nothing was written.
	UpdateLinkCheck(ctx context.Context, result LinkCheckResult) (bool, error)
	// ListLinksDueForArchive returns up to limit visible links with no archived copy whose archive was never
	// looked up or not since checkedBefore, dead links first.
	ListLinksDueForArchive(ctx context.Context, checkedBefore time.Time, limit int32) ([]LinkArchiveTarget, error)
	// UpdateLinkArchive records a lookup of a visible link's archive. It sets the archived copy if the result has
	// one and never clears one already recorded. It returns false if the link is hidden or gone.
	UpdateLinkArchive(ctx context.Context, result LinkArchiveResult) (bool, error)
}
