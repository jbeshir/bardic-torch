package datasources

import "errors"

// ErrNotFound is returned, possibly wrapped, when the requested record does not exist.
var ErrNotFound = errors.New("not found")

// ErrUnavailable is returned, wrapped, when the database cannot be reached or turns the connection away (too many
// connections, shutting down). Trying again later may work.
var ErrUnavailable = errors.New("database unavailable")
