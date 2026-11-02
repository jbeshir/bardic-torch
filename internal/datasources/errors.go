package datasources

import "errors"

// ErrNotFound is returned, possibly wrapped, when the requested record does not exist.
var ErrNotFound = errors.New("not found")
