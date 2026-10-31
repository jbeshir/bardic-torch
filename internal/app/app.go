package app

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/transport/web/server"
)

// bootstrapModeratorsEnv lists, comma-separated, the IDs of users to make moderators at startup.
const bootstrapModeratorsEnv = "BOOTSTRAP_MODERATOR_USER_IDS"

// Component is a long-running part of the application. Run blocks until ctx is cancelled or the component
// fails.
type Component interface {
	Run(ctx context.Context) error
}

// Setup connects to the database, promotes the moderators named in BOOTSTRAP_MODERATOR_USER_IDS, and returns
// the components to run, configured from the environment.
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

	bootstrap := &command.BootstrapModerators{Promoter: repository, Now: now}
	if _, err := bootstrap.Execute(ctx, MustGetEnvAsStrings(ctx, bootstrapModeratorsEnv)); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("bootstrapping moderators: %w", err)
	}

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
				UserBlocker:           repository,
				UserUnblocker:         repository,
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
