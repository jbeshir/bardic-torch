package datasources

import "errors"

// ErrNotFound is returned, possibly wrapped, when the requested record does not exist.
var ErrNotFound = errors.New("not found")

// ErrQueryTimeout is returned, wrapped, when the database stopped a query that ran longer than its execution
// time limit.
var ErrQueryTimeout = errors.New("query exceeded its execution time limit")

// UnknownTermError is returned, possibly wrapped, when a search filters on a term slug that no public term of the
// facets the filter accepts has. Unknown and unapproved terms give the same error, so that the error does not
// reveal that an unapproved term exists.
type UnknownTermError struct {
	// Slug is the slug as the search gave it.
	Slug string
}

func (e *UnknownTermError) Error() string {
	return "unknown term [" + e.Slug + "]"
}

// ErrDocumentChanged is returned, wrapped, by a write given a duplicate check (DuplicateCheck) of other names than
// its locked document's, or none for a new entity it would write: the document changed after the caller checked
// it. The write did not happen; the caller checks the document again and retries.
var ErrDocumentChanged = errors.New("the document changed since its duplicate check")

// ErrUnavailable is returned, wrapped, when the database could not serve a request: it cannot be reached (a
// connection could not be made or broke), or a write could not finish in time (its transaction was still hitting
// lock waits or deadlocks after its retries, or ran past its deadline). A write that fails so did not happen, but
// for a connection that broke during its commit; trying again later may work.
var ErrUnavailable = errors.New("database unavailable")

// ErrUserBlocked is returned, wrapped, by a write whose actor was blocked after their request was authenticated:
// every write that locks the actor's users row checks the block again under the lock. The write did not happen.
var ErrUserBlocked = errors.New("user is blocked")

// ErrActorChanged is returned, wrapped, by a submission or revert of an interactive moderator, which is approved at
// once, whose users row, once locked, no longer holds the moderator role: their role changed after their request
// was authenticated. The write did not happen; the caller runs it again with the role the row holds.
var ErrActorChanged = errors.New("the actor's role changed since the request was authenticated")
