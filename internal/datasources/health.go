package datasources

import "context"

// Pinger checks that a datasource is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Repository is everything the HTTP API reads and writes. It exists for wiring only; each consumer takes the
// narrowest it needs.
type Repository interface {
	Pinger
	Users
}
