//go:build integration

package command

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/datasources/wayback"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/linkcheck"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) { catalogtest.Main(m) }

// lockHolderQuery returns the connection ID holding the link check lock, NULL if none.
const lockHolderQuery = "SELECT IS_USED_LOCK(LEFT(CONCAT('bardic-torch-check-links:', DATABASE()), 64))"

// siteHost is the host of the test site's links, which the test resolver gives a public address.
const siteHost = "songs-of-the-vale.example"

// archivedCopyURL is the copy the test archive has of every page.
const archivedCopyURL = "http://web.archive.org/web/20240101000000/http://" + siteHost + "/archived"

// sitePublicAddr is the address the test resolver gives siteHost; the test dialer connects it to the test site.
var sitePublicAddr = netip.MustParseAddr("93.184.215.14")

// siteResolver resolves siteHost only.
type siteResolver struct{}

func (siteResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if host != siteHost {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}

	return []netip.Addr{sitePublicAddr}, nil
}

// linkSite is a test web site and Wayback Machine. The site answers /ok 200 and /gone 404, and waits on hold, if
// set, before answering /slow. It records every path requested.
type linkSite struct {
	mu        sync.Mutex
	requested []string
	hold      chan struct{}
	site      *httptest.Server
	archive   *httptest.Server
}

func newLinkSite(t *testing.T) *linkSite {
	t.Helper()
	s := &linkSite{}
	s.site = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requested = append(s.requested, r.URL.Path)
		hold := s.hold
		s.mu.Unlock()
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusOK)
		case "/slow":
			if hold != nil {
				select {
				case <-hold:
				case <-r.Context().Done():
				}
			}
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.site.Close)
	s.archive = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"archived_snapshots":{"closest":{"status":"200","available":true,` +
			`"url":"` + archivedCopyURL + `"}}}`))
	}))
	t.Cleanup(s.archive.Close)

	return s
}

// paths returns the paths the site was asked for.
func (s *linkSite) paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.requested...)
}

// prober returns a checker with the production allowlist whose connections to sitePublicAddr reach the site.
func (s *linkSite) prober() *linkcheck.Checker {
	target := s.site.Listener.Addr().String()

	return linkcheck.New(linkcheck.Config{
		UserAgent: linkcheck.UserAgent, AllowAddress: linkcheck.PublicAddress, Resolver: siteResolver{},
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != netip.AddrPortFrom(sitePublicAddr, 80).String() {
				return nil, &net.OpError{Op: "dial", Net: network, Err: net.ErrClosed}
			}
			return (&net.Dialer{}).DialContext(ctx, "tcp", target)
		},
		Deadline: 5 * time.Second, HostInterval: 0,
	})
}

// run returns a run with the site, the archive and repo as its store and lock.
func (s *linkSite) run(repo *mysql.Repository) *CheckLinks {
	return &CheckLinks{
		Locker: repo, Links: repo, Prober: s.prober(),
		Archive: wayback.New(wayback.Config{BaseURL: s.archive.URL, UserAgent: linkcheck.UserAgent}, s.archive.Client()),
		Now:     time.Now,
	}
}

// checkedLink is a links row's checker columns.
type checkedLink struct {
	liveness      domain.Liveness
	failures      int
	checked       bool
	archivedURL   sql.NullString
	archiveLooked bool
}

// insertLink stores a link at path on the site, returning its ID.
func insertLink(t *testing.T, db *sql.DB, path string, failures int, hidden bool) int64 {
	t.Helper()
	url := "http://" + siteHost + path
	res, err := db.ExecContext(t.Context(), "INSERT INTO links (url, normalized_url, liveness_failures, is_hidden) "+
		"VALUES (?, ?, ?, ?)", url, url, failures, hidden)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)

	return id
}

// linkState reads the checker columns of the link id.
func linkState(t *testing.T, db *sql.DB, id int64) checkedLink {
	t.Helper()
	var l checkedLink
	var checked, archiveChecked sql.NullTime
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT liveness, liveness_failures, last_checked_at, "+
		"archived_url, archive_checked_at FROM links WHERE id = ?", id).
		Scan(&l.liveness, &l.failures, &checked, &l.archivedURL, &archiveChecked))
	l.checked, l.archiveLooked = checked.Valid, archiveChecked.Valid

	return l
}

func integrationRequest() CheckLinksRequest {
	return CheckLinksRequest{Limit: 400, ArchiveLimit: 200, Workers: 4, MaxDuration: time.Minute,
		ArchiveSpacing: 0, LockPingInterval: time.Minute}
}

func TestCheckLinks_EndToEnd(t *testing.T) {
	t.Parallel()

	db := catalogtest.Writable(t)
	repo := mysql.New(db)
	site := newLinkSite(t)
	ok := insertLink(t, db, "/ok", 2, false)
	gone := insertLink(t, db, "/gone", 0, false)
	dying := insertLink(t, db, "/dying", 2, false)
	hidden := insertLink(t, db, "/hidden", 2, true)
	internal := "http://127.0.0.1:3306/"
	res, err := db.ExecContext(t.Context(), "INSERT INTO links (url, normalized_url) VALUES (?, ?)", internal, internal)
	require.NoError(t, err)
	refused, err := res.LastInsertId()
	require.NoError(t, err)

	var logs bytes.Buffer
	result, err := site.run(repo).Execute(logContext(t, &logs), integrationRequest())
	require.NoError(t, err)

	assert.Equal(t, map[datasources.LinkOutcome]int{
		datasources.LinkOutcomeOK: 1, datasources.LinkOutcomeGone: 2, datasources.LinkOutcomeRefused: 1,
	}, result.Outcomes)
	assert.Equal(t, domain.LivenessOK, linkState(t, db, ok).liveness)
	assert.Equal(t, 0, linkState(t, db, ok).failures)
	assert.Equal(t, checkedLink{liveness: domain.LivenessUnknown, failures: 1, checked: true,
		archivedURL: sql.NullString{
			String: strings.Replace(archivedCopyURL, "http:", "https:", 1), Valid: true}, archiveLooked: true}, linkState(t, db, gone))
	assert.Equal(t, domain.LivenessDead, linkState(t, db, dying).liveness)
	assert.Equal(t, 3, linkState(t, db, dying).failures)
	assert.Equal(t, domain.LivenessBlocked, linkState(t, db, refused).liveness)
	assert.Equal(t, checkedLink{liveness: domain.LivenessUnknown, failures: 2}, linkState(t, db, hidden),
		"a hidden link is neither checked nor looked up")
	assert.Equal(t, 4, result.ArchiveLookups)
	assert.NotContains(t, site.paths(), "/hidden")
	assert.NotContains(t, logs.String(), siteHost)
	assert.NotContains(t, logs.String(), "127.0.0.1")

	// Nothing is due a second time.
	again, err := site.run(repo).Execute(t.Context(), integrationRequest())
	require.NoError(t, err)
	assert.Empty(t, again.Outcomes)
	assert.Zero(t, again.ArchiveLookups)
}

// TestCheckLinks_SecondRunExitsOnTheLock checks that a run started while another holds the lock does nothing, and
// that the lock is free again once the first finishes.
func TestCheckLinks_SecondRunExitsOnTheLock(t *testing.T) {
	t.Parallel()

	db := catalogtest.Writable(t)
	repo := mysql.New(db)
	site := newLinkSite(t)
	site.hold = make(chan struct{})
	slow := insertLink(t, db, "/slow", 0, false)

	first := make(chan error, 1)
	go func() {
		_, err := site.run(repo).Execute(t.Context(), integrationRequest())
		first <- err
	}()
	require.Eventually(t, func() bool {
		var holder sql.NullInt64
		return db.QueryRowContext(t.Context(), lockHolderQuery).Scan(&holder) == nil && holder.Valid &&
			len(site.paths()) > 0
	}, 10*time.Second, 10*time.Millisecond)

	second, err := site.run(repo).Execute(t.Context(), integrationRequest())
	require.NoError(t, err)
	assert.True(t, second.LockHeld)

	close(site.hold)
	require.NoError(t, <-first)
	assert.Equal(t, domain.LivenessOK, linkState(t, db, slow).liveness)
	var holder sql.NullInt64
	require.NoError(t, db.QueryRowContext(t.Context(), lockHolderQuery).Scan(&holder))
	assert.False(t, holder.Valid, "the lock is released")
}

// grantingLocker grants every run the lock, so that runs can bypass it.
type grantingLocker struct{}

func (grantingLocker) LockLinkCheck(context.Context) (datasources.LinkCheckLock, error) {
	return grantedLock{}, nil
}

type grantedLock struct{}

func (grantedLock) Ping(context.Context) error { return nil }
func (grantedLock) Release(context.Context)    {}

// listingTogether is a store whose ListLinksDueForCheck returns only once every run has listed, so that the runs
// work from one due set.
type listingTogether struct {
	datasources.LinkChecker
	listed *sync.WaitGroup
}

func (l listingTogether) ListLinksDueForCheck(
	ctx context.Context, checkedBefore time.Time, limit int32,
) ([]datasources.LinkCheckTarget, error) {
	targets, err := l.LinkChecker.ListLinksDueForCheck(ctx, checkedBefore, limit)
	l.listed.Done()
	l.listed.Wait()

	return targets, err
}

// TestCheckLinks_ConcurrentRunsCountOnce runs two checks of the same due links at once, bypassing the lock: the
// compare-and-set lets one result per link through, so each failure is counted once.
func TestCheckLinks_ConcurrentRunsCountOnce(t *testing.T) {
	t.Parallel()

	db := catalogtest.Writable(t)
	repo := mysql.New(db)
	site := newLinkSite(t)
	ids := []int64{insertLink(t, db, "/gone-1", 0, false), insertLink(t, db, "/gone-2", 1, false),
		insertLink(t, db, "/gone-3", 2, false)}

	var listed sync.WaitGroup
	listed.Add(2)
	results := make([]CheckLinksResult, 2)
	var runs sync.WaitGroup
	for i := range results {
		runs.Go(func() {
			run := site.run(repo)
			run.Locker, run.Links = grantingLocker{}, listingTogether{LinkChecker: repo, listed: &listed}
			result, err := run.Execute(t.Context(), integrationRequest())
			assert.NoError(t, err)
			results[i] = result
		})
	}
	runs.Wait()

	for i, id := range ids {
		assert.Equal(t, i+1, linkState(t, db, id).failures, "link %d counted once", id)
	}
	assert.Equal(t, len(ids), results[0].Outcomes[datasources.LinkOutcomeGone]+
		results[1].Outcomes[datasources.LinkOutcomeGone])
	assert.Equal(t, len(ids), results[0].NotRecorded+results[1].NotRecorded)
}

// hidingProber hides each link in the database before checking it, as a moderator might mid-run.
type hidingProber struct {
	t     *testing.T
	db    *sql.DB
	inner datasources.LinkProber
}

func (p hidingProber) Probe(ctx context.Context, url string) datasources.LinkOutcome {
	if _, err := p.db.ExecContext(ctx, "UPDATE links SET is_hidden = TRUE WHERE url = ?", url); err != nil {
		p.t.Errorf("hiding the link: %v", err)
	}

	return p.inner.Probe(ctx, url)
}

func TestCheckLinks_HiddenAfterSelectionNotWritten(t *testing.T) {
	t.Parallel()

	db := catalogtest.Writable(t)
	repo := mysql.New(db)
	site := newLinkSite(t)
	id := insertLink(t, db, "/gone", 2, false)

	run := site.run(repo)
	run.Prober = hidingProber{t: t, db: db, inner: run.Prober}
	result, err := run.Execute(t.Context(), integrationRequest())
	require.NoError(t, err)
	assert.Equal(t, 1, result.NotRecorded)
	assert.Equal(t, checkedLink{liveness: domain.LivenessUnknown, failures: 2}, linkState(t, db, id))
}

func TestCheckLinks_SkipsHiddenLinks(t *testing.T) {
	t.Parallel()

	db := catalogtest.Writable(t)
	repo := mysql.New(db)
	site := newLinkSite(t)
	id := insertLink(t, db, "/gone", 2, true)

	result, err := site.run(repo).Execute(t.Context(), integrationRequest())
	require.NoError(t, err)
	assert.Empty(t, result.Outcomes)
	assert.Zero(t, result.ArchiveLookups)
	assert.Empty(t, site.paths())
	assert.Equal(t, checkedLink{liveness: domain.LivenessUnknown, failures: 2}, linkState(t, db, id))
}

// TestCheckLinks_LockHeldOnPinnedConn checks that the lock is held by one connection for as long as it is taken,
// whatever the pool's other connections do, that a second taker is refused, that Ping notices the connection
// being killed, and that Release frees the lock.
func TestCheckLinks_LockHeldOnPinnedConn(t *testing.T) {
	t.Parallel()

	db := catalogtest.Writable(t)
	repo := mysql.New(db)
	holder := func() sql.NullInt64 {
		var id sql.NullInt64
		require.NoError(t, db.QueryRowContext(t.Context(), lockHolderQuery).Scan(&id))
		return id
	}

	lock, err := repo.LockLinkCheck(t.Context())
	require.NoError(t, err)
	first := holder()
	require.True(t, first.Valid)

	// The pool's other connections are taken and returned, and closed; the lock's stays with the lock.
	for range 3 {
		conn, err := db.Conn(t.Context())
		require.NoError(t, err)
		require.NoError(t, conn.PingContext(t.Context()))
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		_ = conn.Close()
	}
	assert.Equal(t, first, holder())
	require.NoError(t, lock.Ping(t.Context()))
	_, err = repo.LockLinkCheck(t.Context())
	require.ErrorIs(t, err, datasources.ErrLinkCheckInProgress)

	lock.Release(t.Context())
	assert.False(t, holder().Valid, "Release frees the lock")

	// A lock whose connection is killed is lost, and Ping says so.
	lock, err = repo.LockLinkCheck(t.Context())
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), fmt.Sprintf("KILL %d", holder().Int64))
	require.NoError(t, err)
	require.ErrorIs(t, lock.Ping(t.Context()), datasources.ErrLinkCheckLockLost)
	lock.Release(t.Context())
	assert.False(t, holder().Valid)
}
