package datasources

import "context"

// Pinger checks that a datasource is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}
