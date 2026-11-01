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

// TestRepository_GetPublicRevision_FormerName checks that a revision shows a person's former name only while it
// is their name or a public alias of theirs: once it is a hidden alias, the snapshot and base show their current
// name, with a page and without.
func TestRepository_GetPublicRevision_FormerName(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
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

// TestRepository_GetPublicRevision_LinksVisibleNow checks that a revision shows a link only if a link that is
// not hidden now has exactly its URL, as entered or normalized: not one deleted since, one given another URL
// and then hidden, or one a hidden link has in another case.
func TestRepository_GetPublicRevision_LinksVisibleNow(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	c := catalogtest.New(t, db)
	const (
		visible    = "https://example.org/visible"
		normalized = "https://example.org/Normalized"
		deleted    = "https://example.org/deleted"
		moved      = "https://example.org/moved"
		recased    = "https://example.org/Recased"
		visibleCI  = "https://example.org/Visible"
	)
	links := []domain.DocPieceLink{}
	for _, url := range []string{visible, normalized, deleted, moved, recased} {
		links = append(links, domain.DocPieceLink{DocLink: domain.DocLink{URL: url,
			Roles: []domain.LinkRole{domain.LinkRoleLyrics}}})
	}
	piece := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "The Lark's Rest", Links: links}})
	c.Exec("UPDATE links SET url = ?, normalized_url = ? WHERE url = ?", "https://example.org/entered",
		normalized, normalized)
	c.Exec("DELETE FROM links WHERE url = ?", deleted)
	c.Exec("UPDATE links SET url = ?, is_hidden = TRUE WHERE url = ?", moved+"-elsewhere", moved)
	c.Exec("UPDATE links SET url = LOWER(url), is_hidden = TRUE WHERE url = ?", recased)

	// A later revision holds a URL that differs from a visible link's in case only.
	later := c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece, EntityID: piece.ID,
		Snapshot: domain.PieceDocument{SchemaVersion: domain.DocumentSchemaVersion, Title: "The Lark's Rest",
			Kind: domain.PieceKindSong, Conservation: domain.DocConservation{Status: domain.ConservationStatusUnassessed},
			LicensingStatus: domain.LicensingStatusUnknown,
			Links: []domain.DocPieceLink{{DocLink: domain.DocLink{URL: visibleCI,
				Roles: []domain.LinkRole{domain.LinkRoleLyrics}}}}},
		BaseRevisionID: piece.RevisionID})

	revision, err := mysql.New(db).GetPublicRevision(t.Context(), later)
	require.NoError(t, err)
	pieceRevision, ok := revision.(domain.PieceRevision)
	require.True(t, ok, "%T", revision)
	assert.Empty(t, pieceRevision.Snapshot.Links)
	require.NotNil(t, pieceRevision.Base)
	var urls []string
	for _, link := range pieceRevision.Base.Snapshot.Links {
		urls = append(urls, link.URL)
	}
	assert.Equal(t, []string{visible, normalized}, urls)
}

// TestRepository_GetPublicRevision_NonASCIILinkURL checks that a revision whose links hold non-ASCII URLs reads
// and keeps its redaction. Normalized URLs are ASCII, and MySQL refuses to compare an ASCII column with a
// non-ASCII parameter, so such a URL must match only the URLs as entered: shown if a visible link was entered
// with it, dropped if a hidden link was or no link has it.
func TestRepository_GetPublicRevision_NonASCIILinkURL(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	c := catalogtest.New(t, db)
	const (
		shownEncoded  = "https://example.org/caf%C3%A9"
		shownRaw      = "https://example.org/café"
		hiddenEncoded = "https://example.org/cr%C3%A8me"
		hiddenRaw     = "https://example.org/crème"
		unknownRaw    = "https://example.org/naïve"
	)
	links := func(urls ...string) []domain.DocPieceLink {
		out := []domain.DocPieceLink{}
		for _, url := range urls {
			out = append(out, domain.DocPieceLink{DocLink: domain.DocLink{URL: url,
				Roles: []domain.LinkRole{domain.LinkRoleLyrics}}})
		}
		return out
	}
	// catalogtest stores each URL as its own normalized URL, which the ASCII column needs, so the links are
	// seeded encoded and then given the URL as entered.
	piece := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "The Willow Glen",
		Links: links(shownEncoded, hiddenEncoded)}})
	c.Exec("UPDATE links SET url = ? WHERE url = ?", shownRaw, shownEncoded)
	c.Exec("UPDATE links SET url = ?, is_hidden = TRUE WHERE url = ?", hiddenRaw, hiddenEncoded)

	later := c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece, EntityID: piece.ID,
		Snapshot: domain.PieceDocument{SchemaVersion: domain.DocumentSchemaVersion, Title: "The Willow Glen",
			Kind: domain.PieceKindSong, Conservation: domain.DocConservation{Status: domain.ConservationStatusUnassessed},
			LicensingStatus: domain.LicensingStatusUnknown,
			Links:           links(shownRaw, hiddenRaw, unknownRaw, shownEncoded, hiddenEncoded)},
		BaseRevisionID: piece.RevisionID})

	revision, err := mysql.New(db).GetPublicRevision(t.Context(), later)
	require.NoError(t, err)
	pieceRevision, ok := revision.(domain.PieceRevision)
	require.True(t, ok, "%T", revision)
	urls := func(got []domain.SnapshotPieceLink) []string {
		var out []string
		for _, link := range got {
			out = append(out, link.URL)
		}
		return out
	}
	assert.Equal(t, []string{shownRaw, shownEncoded}, urls(pieceRevision.Snapshot.Links))
	require.NotNil(t, pieceRevision.Base)
	assert.Equal(t, []string{shownEncoded}, urls(pieceRevision.Base.Snapshot.Links))
}
