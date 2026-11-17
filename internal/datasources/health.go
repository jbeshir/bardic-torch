package datasources

import "context"

// Pinger checks that a datasource is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Repository is everything the HTTP API reads and writes. It exists for wiring only; each consumer takes the
// narrowest of Pinger, Users and Catalogue it needs.
type Repository interface {
	Pinger
	Users
	Catalogue
	Moderation
	Revisions
}
