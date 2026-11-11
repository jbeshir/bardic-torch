//go:build integration

package mysql_test

import (
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRepository_GetPublicPerson_PerformancesNeedPublicAttachment checks that a person's page shows the
// performances they perform in only on links with a public attachment: to a public piece, or to a public person
// with a page. A link attached only to a person who prefers no page, or to one who is not public, is theirs.
func TestRepository_GetPublicPerson_PerformancesNeedPublicAttachment(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
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

// TestRepository_GetPublicRevision_LinksVisibleNow checks that a revision drops only a link whose normalized URL
// is a hidden link's now, and shows every other as written: one deleted since, one given another URL and then
// hidden, and one whose path differs in case from a hidden link's are all shown.
func TestRepository_GetPublicRevision_LinksVisibleNow(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
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
	assert.Equal(t, []string{visiblePath, visibleHost}, snapshot)
	assert.Equal(t, []string{visible, deleted, moved, recased}, base)
}

// TestRepository_GetPublicRevision_RespelledHiddenLinkStaysRedacted checks that a link edited to another
// spelling of its URL, with the same normalized URL, and then hidden is redacted from a revision holding the
// spelling it had before.
func TestRepository_GetPublicRevision_RespelledHiddenLinkStaysRedacted(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
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

// TestRepository_GetPublicRevision_RespelledLinkShownAsWritten checks that a link edited to another spelling of
// its URL, with the same normalized URL, is shown in a revision as that revision wrote it: the base shows the
// entered spelling, the snapshot the new one.
func TestRepository_GetPublicRevision_RespelledLinkShownAsWritten(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
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
	assert.Equal(t, []string{entered}, base)
}

// TestRepository_GetPublicRevision_NonASCIILinkURL checks that a revision whose links hold non-ASCII URLs reads
// and keeps its redaction. A non-ASCII URL is matched by its normalized form, which percent-encodes it, so it is
// never compared with the ASCII normalized_url column as it is (which MySQL refuses, error 3988): it is dropped
// if a hidden link has it, and otherwise shown as written.
func TestRepository_GetPublicRevision_NonASCIILinkURL(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
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
	assert.Equal(t, []string{shownRaw, unknownRaw, shownEncoded}, snapshot)
	assert.Equal(t, []string{shownRaw}, base)
}
