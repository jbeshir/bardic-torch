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

// ErrUnavailable is returned, wrapped, when a write could not finish in time: its transaction was still hitting
// lock waits or deadlocks after its retries, or ran past its deadline. The write did not happen, and trying
// again later may work.
var ErrUnavailable = errors.New("database busy")
