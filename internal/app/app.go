package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/transport/web/router"
	"github.com/jbeshir/bardic-torch/internal/transport/web/server"
)

// bootstrapModeratorsEnv lists, comma-separated, the IDs of users to make moderators at startup.
const bootstrapModeratorsEnv = "BOOTSTRAP_MODERATOR_USER_IDS"

// Component is a long-running part of the application. Run blocks until ctx is cancelled or the component
// fails.
type Component interface {
	Run(ctx context.Context) error
}

// Setup connects to the database, promotes the moderators named in BOOTSTRAP_MODERATOR_USER_IDS, stores missing
// or stale display name keys, gives the performances with no owner their owner, purges the contact requests past
// their retention, and returns the components to run, configured from the environment.
func Setup(ctx context.Context) ([]Component, error) {
	dbConfig, err := mysql.ConfigFromURL(MustGetEnvAsString(ctx, "DATABASE_URL"))
	if err != nil {
		return nil, fmt.Errorf("parsing DATABASE_URL: %w", err)
	}

	db, err := mysql.Connect(ctx, dbConfig, mysql.ProductionPool())
	if err != nil {
		return nil, fmt.Errorf("connecting to MySQL: %w", err)
	}
	repository := mysql.New(db)

	bootstrap := &command.BootstrapModerators{Promoter: repository, Now: now}
	if _, err := bootstrap.Execute(ctx, MustGetEnvAsStrings(ctx, bootstrapModeratorsEnv)); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("bootstrapping moderators: %w", err)
	}

	backfill := &command.BackfillDisplayNameKeys{Lister: repository, Setter: repository}
	if _, err := backfill.Execute(ctx, command.Empty{}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("backfilling display name keys: %w", err)
	}

	owners := &command.BackfillPerformanceOwners{Backfiller: repository}
	if _, err := owners.Execute(ctx, command.Empty{}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("backfilling performance owners: %w", err)
	}

	purge := &command.PurgeContactRequests{Purger: repository, Now: now}
	if _, err := purge.Execute(ctx, command.Empty{}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("purging contact requests: %w", err)
	}

	authValidators, err := authValidatorsFromEnv(ctx, repository, now)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("setting up authentication: %w", err)
	}
	verifier := contactVerifierFromEnv(ctx, &http.Client{})

	addr := ":" + strconv.Itoa(MustGetEnvAsInt(ctx, "PORT"))
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("listening on [%s]: %w", addr, err)
	}

	return []Component{
		&server.Server{
			Listener: listener,
			Handler:  NewHTTPHandler(repositoryHTTPDependencies(repository, authValidators, verifier)),
		},
	}, nil
}

// repositoryHTTPDependencies are the HTTP handler's dependencies with repository as every datasource, verifier as
// the contact form's Turnstile verifier, and the production clock.
func repositoryHTTPDependencies(
	repository *mysql.Repository, authValidators []router.AuthValidator, verifier datasources.TurnstileVerifier,
) HTTPDependencies {
	return HTTPDependencies{
		Pinger:                         repository,
		AuthValidators:                 authValidators,
		UserGetter:                     repository,
		UserEnsurer:                    repository,
		DisplayNameUpdater:             repository,
		UserBlocker:                    repository,
		UserUnblocker:                  repository,
		APITokenCreator:                repository,
		ActiveAPITokenCounter:          repository,
		ActiveAPITokenLister:           repository,
		APITokenRevoker:                repository,
		PublicPieceLister:              repository,
		PublicPieceGetter:              repository,
		PublicPersonLister:             repository,
		PublicPersonGetter:             repository,
		PublicTermLister:               repository,
		PublicTermGetter:               repository,
		PublicCollectionLister:         repository,
		PublicCollectionGetter:         repository,
		PublicEventLister:              repository,
		PublicEventGetter:              repository,
		PublicHistoryLister:            repository,
		PublicCollectionHistoryLister:  repository,
		PublicRevisionGetter:           repository,
		PublicNeedsWorkLister:          repository,
		PublicPersonPerformanceLister:  repository,
		PublicCollectionCitationLister: repository,
		PublicEventPerformanceLister:   repository,
		EditorRefResolver:              repository,
		EditableDocumentGetter:         repository,
		DraftSaver:                     repository,
		DraftUpdater:                   repository,
		DraftGetter:                    repository,
		DraftDiscarder:                 repository,
		DuplicateFinder:                repository,
		RevisionSubmitter:              repository,
		RevisionWithdrawer:             repository,
		RevisionReverter:               repository,
		MyRevisionLister:               repository,
		MyRevisionGetter:               repository,
		PendingVersionGetter:           repository,
		ModerationQueueLister:          repository,
		ModerationRevisionGetter:       repository,
		ModerationLinkGetter:           repository,
		RevisionApprover:               repository,
		RevisionRejecter:               repository,
		RevisionRebaser:                repository,
		LinkReporter:                   repository,
		LinkReportLister:               repository,
		LinkReportResolver:             repository,
		LinkHiddenSetter:               repository,
		ContactRequestCreator:          repository,
		ContactRequestLister:           repository,
		ContactRequestResolver:         repository,
		ContactRequestPurger:           repository,
		TurnstileVerifier:              verifier,
		Now:                            now,
	}
}

// now is the production clock, giving times as the database stores them.
func now() time.Time {
	return storedTime(time.Now())
}

// storedTime returns t as the database stores it: in UTC and truncated to whole seconds. The DATETIME columns
// have no fractional seconds and MySQL rounds them on insert, so a time with fractions returned to a client
// could be a second off the stored value it is later read back as.
func storedTime(t time.Time) time.Time {
	return t.UTC().Truncate(time.Second)
}
