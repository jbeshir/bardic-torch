package datasources

import (
	"context"
	"errors"
)

// LinkOutcome is what checking whether a link works found.
type LinkOutcome string

const (
	// LinkOutcomeOK is a link whose last answer was a success (2xx).
	LinkOutcomeOK LinkOutcome = "ok"
	// LinkOutcomeGone is a definite failure: the site says the page is gone (404, 410), or its host name does not
	// exist.
	LinkOutcomeGone LinkOutcome = "gone"
	// LinkOutcomeUnreachable is a failure that may pass: a server error (5xx), a timeout or a connection failure.
	LinkOutcomeUnreachable LinkOutcome = "unreachable"
	// LinkOutcomeBlocked is a site that refused the checker (401, 403, 429, 999, any other answer that is neither a
	// success nor gone, or a redirect loop), so whether the link works is not known.
	LinkOutcomeBlocked LinkOutcome = "blocked"
	// LinkOutcomeRefused is a link the checker would not follow: its URL, or a redirect's, has a scheme other than
	// http or https, credentials, a port other than 80 or 443, or a host that is not a public address.
	LinkOutcomeRefused LinkOutcome = "refused"
)

// LinkProber checks whether a link works. It never sends cookies or credentials, and never reaches an address
// that is not public.
type LinkProber interface {
	Probe(ctx context.Context, url string) LinkOutcome
}

// ErrArchiveUnavailable is an archive lookup that could not be made: the archive failed, answered unreadably or
// could not be reached in time. Trying again later may work.
var ErrArchiveUnavailable = errors.New("archive lookup unavailable")

// ArchiveLookup finds an archived copy of a page.
type ArchiveLookup interface {
	// Available returns the URL of an archived copy of url, nil if the archive has none, or an error wrapping
	// ErrArchiveUnavailable.
	Available(ctx context.Context, url string) (*string, error)
}

// The errors of the link checker's run lock.
var (
	// ErrLinkCheckInProgress is returned when another run of the link checker holds the lock.
	ErrLinkCheckInProgress = errors.New("another link check is running")
	// ErrLinkCheckLockLost is returned when the run's lock is no longer held, as when its connection dropped.
	ErrLinkCheckLockLost = errors.New("the link check lock was lost")
)

// LinkCheckLocker takes the lock that lets one run of the link checker at a time write.
type LinkCheckLocker interface {
	// LockLinkCheck takes the lock without waiting, or returns ErrLinkCheckInProgress.
	LockLinkCheck(ctx context.Context) (LinkCheckLock, error)
}

// LinkCheckLock is a held link checker lock.
type LinkCheckLock interface {
	// Ping checks that the lock is still held, returning ErrLinkCheckLockLost if not.
	Ping(ctx context.Context) error
	// Release releases the lock. It is safe to call once only.
	Release(ctx context.Context)
}
