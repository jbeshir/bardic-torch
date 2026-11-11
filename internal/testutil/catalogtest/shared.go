//go:build integration

package catalogtest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
)

// Shared is a seeded database that read-only tests share, so a test binary pays for creating and seeding it once
// instead of once per test (creating a migrated database takes seconds, and MySQL serialises much of that DDL).
// A package declares one Shared per fixture and installs Main as its TestMain:
//
//	var pieces = catalogtest.NewShared(func(c *catalogtest.Catalog) pieceFixture {
//		war := c.Term(catalogtest.Term{Doc: domain.TermDocument{Name: "War"}})
//		return pieceFixture{war: war, kettle: c.Piece(...)}
//	})
//
//	func TestMain(m *testing.M) { catalogtest.Main(m) }
//
//	func TestPieceDetail(t *testing.T) {
//		t.Parallel()
//		db, fx := pieces.Get(t)
//		...
//	}
//
// The database is created and seeded by the first Get, and dropped by Main after every test has run. Get's pool
// is read-only (transaction_read_only), so a test that writes fails with MySQL error 1792 instead of changing
// what other tests see; a test that needs to write seeds its own database with New.
type Shared[F any] struct {
	seed func(c *Catalog) F

	once    sync.Once
	db      *sql.DB
	fixture F
	seeded  bool
}

// NewShared returns a Shared database that seed fills; seed returns what tests need to know about the data, such
// as Refs.
func NewShared[F any](seed func(c *Catalog) F) *Shared[F] {
	return &Shared[F]{seed: seed}
}

// Get returns a read-only pool to the shared database and the fixture seed returned, creating and seeding the
// database on the first call. If seeding failed, every call fails its test.
func (s *Shared[F]) Get(t testing.TB) (*sql.DB, F) {
	t.Helper()

	if !mainRunning.Load() {
		t.Fatal("catalogtest: a Shared database needs catalogtest.Main as the package's TestMain")
	}
	s.once.Do(func() { s.create(t) })
	if !s.seeded {
		t.Fatal("catalogtest: seeding the shared database failed; see the first test that used it")
	}

	return s.db, s.fixture
}

// create creates, registers for dropping, and seeds the database, failing t on error.
func (s *Shared[F]) create(t testing.TB) {
	t.Helper()

	// The database outlives t, so it is created and dropped with a context t does not cancel.
	ctx := context.WithoutCancel(t.Context())
	dbURL, drop, err := mysqltest.CreateDatabase(ctx)
	if err != nil {
		t.Fatal(err)
	}
	registerCleanup(func() error { return drop(context.Background()) })

	writable, err := mysqltest.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writable.Close() }()
	fixture := s.seed(New(t, writable))

	cfg, err := mysql.ConfigFromURL(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Params["transaction_read_only"] = "ON"
	readOnly, err := mysql.Connect(ctx, cfg, mysqltest.Pool())
	if err != nil {
		t.Fatal(err)
	}
	// Registered after drop, so it runs before it.
	registerCleanup(readOnly.Close)

	s.db, s.fixture, s.seeded = readOnly, fixture, true
}

var (
	mainRunning atomic.Bool

	cleanupMu sync.Mutex
	cleanups  []func() error
)

func registerCleanup(f func() error) {
	cleanupMu.Lock()
	defer cleanupMu.Unlock()
	cleanups = append(cleanups, f)
}

// Main runs the package's tests, then closes and drops every Shared database they created, and exits. Use it
// as TestMain in every package that uses Shared.
func Main(m *testing.M) {
	mainRunning.Store(true)
	code := m.Run()

	cleanupMu.Lock()
	var errs []error
	for i := len(cleanups) - 1; i >= 0; i-- {
		errs = append(errs, cleanups[i]())
	}
	cleanupMu.Unlock()
	if err := errors.Join(errs...); err != nil {
		fmt.Fprintf(os.Stderr, "catalogtest: dropping shared databases: %v\n", err)
		if code == 0 {
			code = 1
		}
	}

	os.Exit(code)
}
