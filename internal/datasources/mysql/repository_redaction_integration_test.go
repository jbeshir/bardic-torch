//go:build integration

package mysql_test

import (
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRepository_GetPublicPerson_PerformancesNeedPublicAttachment checks that a person's page shows the
// performances they perform in only on links with a public attachment: to a public piece, or to a public person
// with a page. A link attached only to a person who prefers no page, or to one who is not public, is theirs.
func TestRepository_GetPublicPerson_PerformancesNeedPublicAttachment(t *testing.T) {
	t.Parallel()

	db := catalogtest.Writable(t)
	c := catalogtest.New(t, db)
	performer := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Tamsin Reed"}})
	performance := func(url, description string) []domain.DocPersonLink {
		return []domain.DocPersonLink{{
			DocLink: domain.DocLink{URL: url, Roles: []domain.LinkRole{domain.LinkRolePerformance}},
			Performances: []domain.DocPersonPerformance{{DocPerformance: domain.DocPerformance{
				DescriptionVerbatim: description, PerformerIDs: []int64{performer.ID}}}},
		}}
	}
	c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Nell Hart", Status: domain.PersonStatusPrefersNoPage,
		Links: performance("https://example.org/nell", "On a no-page person's link")}})
	c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Draft Bard",
		Links: performance("https://example.org/draft", "On an unapproved person's link")}, Unapproved: true})
	c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Odo Marsh",
		Links: performance("https://example.org/odo", "On a public person's link")}})
	c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "The Mill Wheel",
		Links: []domain.DocPieceLink{{
			DocLink: domain.DocLink{URL: "https://example.org/mill", Roles: []domain.LinkRole{domain.LinkRolePerformance}},
			Performances: []domain.DocPerformance{{DescriptionVerbatim: "On a public piece's link",
				PerformerIDs: []int64{performer.ID}}},
		}}}})
	// A link attached both to a no-page person and to a public piece has a public attachment.
	c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Ivy Lowe", Status: domain.PersonStatusPrefersNoPage,
		Links: performance("https://example.org/shared", "On a shared link")}})
	c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "The Shared Tune",
		Links: []domain.DocPieceLink{{DocLink: domain.DocLink{URL: "https://example.org/shared",
			Roles: []domain.LinkRole{domain.LinkRolePerformance}}}}}})

	person, err := mysql.New(db).GetPublicPerson(t.Context(), performer.Slug)
	require.NoError(t, err)
	require.NotNil(t, person.Profile)
	var descriptions []string
	for _, p := range person.Profile.Performances {
		descriptions = append(descriptions, p.DescriptionVerbatim)
	}
	assert.ElementsMatch(t, []string{"On a public person's link", "On a public piece's link", "On a shared link"},
		descriptions)
	assert.Equal(t, int64(3), person.Profile.PerformancesTotal)
}

// TestRepository_GetPublicRevision_FormerName checks that a revision shows a person's former name only while it
// is their name or a public alias of theirs: once it is a hidden alias, the snapshot and base show their current
// name, with a page and without.
func TestRepository_GetPublicRevision_FormerName(t *testing.T) {
	t.Parallel()

	db := catalogtest.Writable(t)
	c := catalogtest.New(t, db)
	repo := mysql.New(db)
	// No subtests: catalogtest must be called from the test's own goroutine.
	for _, status := range []domain.PersonStatus{domain.PersonStatusActive, domain.PersonStatusPrefersNoPage} {
		doc := domain.PersonDocument{
			SchemaVersion: domain.DocumentSchemaVersion, Name: "Briar " + string(status),
			Kind: domain.PersonKindCommunityBard, Status: status, LicensingStatus: domain.LicensingStatusUnknown,
			Aliases: []domain.DocPersonAlias{
				{Name: "Former " + string(status), Kind: domain.PersonAliasKindLegal},
				{Name: "Stage " + string(status), Kind: domain.PersonAliasKindStage, IsPublic: true},
			},
		}
		person := c.Person(catalogtest.Person{Doc: doc})
		named := func(name string, base int64) int64 {
			former := doc
			former.Name = name
			return c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePerson, EntityID: person.ID,
				Snapshot: former, BaseRevisionID: base})
		}
		hidden := named("Former "+string(status), 0)
		public := named("Stage "+string(status), hidden)
		recased := named("BRIAR "+string(status), public)

		for id, want := range map[int64]string{
			person.RevisionID: doc.Name, hidden: doc.Name, public: "Stage " + string(status), recased: doc.Name,
		} {
			revision, err := repo.GetPublicRevision(t.Context(), id)
			require.NoError(t, err)
			personRevision, ok := revision.(domain.PersonRevision)
			require.True(t, ok, "%T", revision)
			assert.Equal(t, want, personRevision.Snapshot.Name, id)
		}

		revision, err := repo.GetPublicRevision(t.Context(), public)
		require.NoError(t, err)
		personRevision, ok := revision.(domain.PersonRevision)
		require.True(t, ok, "%T", revision)
		require.NotNil(t, personRevision.Base)
		assert.Equal(t, doc.Name, personRevision.Base.Snapshot.Name, "the base's former name is hidden")
	}
}

// lyricsLinks returns links with the lyrics role and the given URLs, in order.
func lyricsLinks(urls ...string) []domain.DocPieceLink {
	out := []domain.DocPieceLink{}
	for _, url := range urls {
		out = append(out, domain.DocPieceLink{DocLink: domain.DocLink{URL: url,
			Roles: []domain.LinkRole{domain.LinkRoleLyrics}}})
	}
	return out
}

// laterPieceRevision seeds a revision of the piece holding links with the given URLs, based on its current
// revision.
func laterPieceRevision(c *catalogtest.Catalog, piece catalogtest.Ref, title string, urls ...string) int64 {
	return c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece, EntityID: piece.ID,
		Snapshot: domain.PieceDocument{SchemaVersion: domain.DocumentSchemaVersion, Title: title,
			Kind: domain.PieceKindSong, Conservation: domain.DocConservation{Status: domain.ConservationStatusUnassessed},
			LicensingStatus: domain.LicensingStatusUnknown, Links: lyricsLinks(urls...)},
		BaseRevisionID: piece.RevisionID})
}

// pieceRevisionLinkURLs reads a piece revision and returns the URLs of the links its snapshot and its base's
// snapshot show.
func pieceRevisionLinkURLs(t *testing.T, repo *mysql.Repository, id int64) (snapshot, base []string) {
	t.Helper()

	revision, err := repo.GetPublicRevision(t.Context(), id)
	require.NoError(t, err)
	pieceRevision, ok := revision.(domain.PieceRevision)
	require.True(t, ok, "%T", revision)
	for _, link := range pieceRevision.Snapshot.Links {
		snapshot = append(snapshot, link.URL)
	}
	if pieceRevision.Base != nil {
		for _, link := range pieceRevision.Base.Snapshot.Links {
			base = append(base, link.URL)
		}
	}

	return snapshot, base
}

// normalized returns domain.NormalizeURL(url), failing the test if url does not normalize.
func normalized(t *testing.T, url string) string {
	t.Helper()

	n, err := domain.NormalizeURL(url)
	require.NoError(t, err)

	return n
}

// TestRepository_GetPublicRevision_LinksVisibleNow checks that a revision shows a link only if its normalized URL
// is a link's that is not hidden now: not one deleted since, one given another URL and then hidden, or one whose
// path differs in case from a hidden or visible link's. A URL that differs from a visible link's only in the
// case of its host is the same link, and shows under the link's URL.
func TestRepository_GetPublicRevision_LinksVisibleNow(t *testing.T) {
	t.Parallel()

	db := catalogtest.Writable(t)
	c := catalogtest.New(t, db)
	const (
		title       = "The Lark's Rest"
		visible     = "https://example.org/visible"
		deleted     = "https://example.org/deleted"
		moved       = "https://example.org/moved"
		recased     = "https://example.org/Recased"
		visiblePath = "https://example.org/Visible"
		visibleHost = "https://EXAMPLE.org/visible"
	)
	piece := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: title,
		Links: lyricsLinks(visible, deleted, moved, recased)}})
	c.Exec("DELETE FROM links WHERE url = ?", deleted)
	c.Exec("UPDATE links SET url = ?, normalized_url = ?, is_hidden = TRUE WHERE url = ?", moved+"-elsewhere",
		normalized(t, moved+"-elsewhere"), moved)
	c.Exec("UPDATE links SET url = LOWER(url), normalized_url = LOWER(normalized_url), is_hidden = TRUE "+
		"WHERE url = ?", recased)

	later := laterPieceRevision(c, piece, title, visiblePath, visibleHost)

	snapshot, base := pieceRevisionLinkURLs(t, mysql.New(db), later)
	assert.Equal(t, []string{visible}, snapshot)
	assert.Equal(t, []string{visible}, base)
}

// TestRepository_GetPublicRevision_RespelledHiddenLinkStaysRedacted checks that a link edited to another
// spelling of its URL, with the same normalized URL, and then hidden is redacted from a revision holding the
// spelling it had before.
func TestRepository_GetPublicRevision_RespelledHiddenLinkStaysRedacted(t *testing.T) {
	t.Parallel()

	db := catalogtest.Writable(t)
	c := catalogtest.New(t, db)
	const (
		title    = "The Hollow Oak"
		entered  = "https://Example.org:443/x?utm_source=1"
		respelt  = "https://example.org/x"
		elsewise = "https://example.org/y"
	)
	require.Equal(t, normalized(t, entered), normalized(t, respelt))
	piece := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: title, Links: lyricsLinks(entered, elsewise)}})
	c.Exec("UPDATE links SET url = ?, is_hidden = TRUE WHERE url = ?", respelt, entered)

	later := laterPieceRevision(c, piece, title, respelt, elsewise)

	snapshot, base := pieceRevisionLinkURLs(t, mysql.New(db), later)
	assert.Equal(t, []string{elsewise}, snapshot)
	assert.Equal(t, []string{elsewise}, base)
}

// TestRepository_GetPublicRevision_RespelledVisibleLinkStillShown checks that a link edited to another spelling of
// its URL, with the same normalized URL, is still shown, under its URL now, in a revision holding the spelling it
// had before: a revision never shows a spelling, such as its tracking parameters, that the link no longer holds.
func TestRepository_GetPublicRevision_RespelledVisibleLinkStillShown(t *testing.T) {
	t.Parallel()

	db := catalogtest.Writable(t)
	c := catalogtest.New(t, db)
	const (
		title   = "The Hollow Oak"
		entered = "https://Example.org:443/x?utm_source=1"
		respelt = "https://example.org/x"
	)
	piece := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: title, Links: lyricsLinks(entered)}})
	c.Exec("UPDATE links SET url = ? WHERE url = ?", respelt, entered)

	later := laterPieceRevision(c, piece, title, respelt)

	snapshot, base := pieceRevisionLinkURLs(t, mysql.New(db), later)
	assert.Equal(t, []string{respelt}, snapshot)
	assert.Equal(t, []string{respelt}, base)
}

// TestRepository_GetPublicRevision_NonASCIILinkURL checks that a revision whose links hold non-ASCII URLs reads
// and keeps its redaction. A non-ASCII URL is matched by its normalized form, which percent-encodes it, so it is
// never compared with the ASCII normalized_url column as it is (which MySQL refuses, error 3988): it is shown if
// a visible link has its normalized URL, under that link's URL however the revision spelt it, and dropped if a
// hidden link has it or no link does.
func TestRepository_GetPublicRevision_NonASCIILinkURL(t *testing.T) {
	t.Parallel()

	db := catalogtest.Writable(t)
	c := catalogtest.New(t, db)
	const (
		title         = "The Willow Glen"
		shownEncoded  = "https://example.org/caf%C3%A9"
		shownRaw      = "https://example.org/café"
		hiddenEncoded = "https://example.org/cr%C3%A8me"
		hiddenRaw     = "https://example.org/crème"
		unknownRaw    = "https://example.org/naïve"
	)
	piece := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: title, Links: lyricsLinks(shownRaw, hiddenRaw)}})
	c.SetLinkState(hiddenRaw, catalogtest.LinkState{IsHidden: true})

	later := laterPieceRevision(c, piece, title, shownRaw, hiddenRaw, unknownRaw, shownEncoded, hiddenEncoded)

	snapshot, base := pieceRevisionLinkURLs(t, mysql.New(db), later)
	assert.Equal(t, []string{shownRaw, shownRaw}, snapshot)
	assert.Equal(t, []string{shownRaw}, base)
}
