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
// performances they perform in only on links with a public attachment, to a public piece or person: a link
// attached only to a person who is not public is theirs.
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
	// A link attached both to an unapproved person and to a public piece has a public attachment.
	c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Ivy Lowe",
		Links: performance("https://example.org/shared", "On a shared link")}, Unapproved: true})
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

// TestRepository_GetPublicRevision_NonASCIILinkURL checks that a revision whose links hold non-ASCII URLs reads
// and leaves out the hidden link. A URL finds its link by its normalized form, which percent-encodes it, so it is
// never compared with the ASCII normalized_url column as it is (which MySQL refuses, error 3988): a link that is
// hidden is left out in either spelling, and every other URL is shown as written.
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
