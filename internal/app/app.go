package app

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/transport/web/server"
)

// Component is a long-running part of the application. Run blocks until ctx is cancelled or the component
// fails.
type Component interface {
	Run(ctx context.Context) error
}

// Setup connects to the database and returns the components to run, configured from the environment.
func Setup(ctx context.Context) ([]Component, error) {
	dbConfig, err := mysql.ConfigFromURL(MustGetEnvAsString(ctx, "DATABASE_URL"))
	if err != nil {
		return nil, fmt.Errorf("parsing DATABASE_URL: %w", err)
	}

	db, err := mysql.Connect(ctx, dbConfig)
	if err != nil {
		return nil, fmt.Errorf("connecting to MySQL: %w", err)
	}
	repository := mysql.New(db)

	authValidators, err := authValidatorsFromEnv(ctx, repository, now)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("setting up authentication: %w", err)
	}

	addr := ":" + strconv.Itoa(MustGetEnvAsInt(ctx, "PORT"))
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("listening on [%s]: %w", addr, err)
	}

	return []Component{
		&server.Server{
			Listener: listener,
			Handler: NewHTTPHandler(HTTPDependencies{
				Pinger:                repository,
				AuthValidators:        authValidators,
				UserGetter:            repository,
				UserEnsurer:           repository,
				DisplayNameUpdater:    repository,
				APITokenCreator:       repository,
				ActiveAPITokenCounter: repository,
				ActiveAPITokenLister:  repository,
				APITokenRevoker:       repository,
				Now:                   now,
			}),
		},
	}, nil
}

// now is the production clock. Times are UTC, as the database stores them.
func now() time.Time {
	return time.Now().UTC()
}
