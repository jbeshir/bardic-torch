//go:build integration

package mysql_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) { catalogtest.Main(m) }

// detailFixture is a catalogue where every visibility rule of the piece and person pages has something to hide:
// unapproved terms (and an approved term under an unapproved parent), persons, pieces, collections and events;
// a prefers_no_page person referenced from everywhere a person can be; and a hidden link.
type detailFixture struct {
	kettle, pendingPiece      catalogtest.Ref
	wren, noPage, pendingWren catalogtest.Ref
	prolific                  catalogtest.Ref
	songbook                  catalogtest.Ref
}

// kettleCheckedAt is a fixture time; DATETIME columns are read back in UTC.
var kettleCheckedAt = time.Date(2026, 9, 1, 8, 30, 0, 0, time.UTC)

// Fixture strings the tests compare more than once.
const (
	revelName        = "Midwinter Revel"
	revelSlug        = "midwinter-revel"
	warName          = "War"
	swordsName       = "Swords"
	northmarchName   = "Northmarch"
	eastmarchName    = "Eastmarch"
	hollisName       = "Hollis Fenn"
	hollisURL        = "https://example.org/hollis"
	revelPerformance = "Sung at the revel"
	wrenName         = "Wren of the Vale"
	wrenURL          = "https://example.org/wren"
	kettleTitle      = "The Copper Kettle"
	kettleVideoURL   = "https://example.org/kettle/video"
)

// manyItems is one more than a capped list holds.
const manyItems = domain.MaxNestedListItems + 1

var details = catalogtest.NewShared(func(c *catalogtest.Catalog) detailFixture {
	war := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic, Name: warName}})
	swords := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic, Name: swordsName,
		ParentID: &war.ID}})
	century := c.Term(catalogtest.Term{
		Doc:  domain.TermDocument{Facet: domain.FacetOrigin, Name: "16th Century (Origin)", DisplayName: "16th Century"},
		Slug: "16th-century",
	})
	pendingTerm := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetMood, Name: "Wistful"},
		Unapproved: true})
	underPending := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetMood, Name: "Very Wistful",
		ParentID: &pendingTerm.ID}})
	northmarch := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetAffiliation,
		Name: northmarchName, Position: 2}})
	eastmarch := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetAffiliation,
		Name: eastmarchName, Position: 1}})
	southmarch := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetAffiliation,
		Name: "Southmarch"}, Unapproved: true})

	songbook := c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: "The Northmarch Songbook"}})
	pendingBook := c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: "Draft Songbook"},
		Unapproved: true})
	revel := c.Event(catalogtest.Event{Doc: domain.EventDocument{Name: revelName}})
	pendingEvent := c.Event(catalogtest.Event{Doc: domain.EventDocument{Name: "Unconfirmed Moot"}, Unapproved: true})

	bards := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Northmarch Bards",
		Kind: domain.PersonKindGroup, IsStub: true}})
	pendingWren := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Pending Wren"}, Unapproved: true})
	wren := c.Person(catalogtest.Person{
		Doc: domain.PersonDocument{
			Name:             wrenName,
			NotesMD:          new("A bard of the marches."),
			LicensingStatus:  domain.LicensingStatusKnown,
			LicensingNotesMD: new("Sing freely."),
			Aliases: []domain.DocPersonAlias{
				{Name: "Wren", Kind: domain.PersonAliasKindSCA},
				{Name: "Jo Legalname", Kind: domain.PersonAliasKindLegal},
				{Name: "Little Wren", Kind: domain.PersonAliasKindSCA},
			},
			Roles:          []domain.PersonRole{domain.PersonRoleSongwriter, domain.PersonRoleSinger},
			AffiliationIDs: []int64{northmarch.ID, southmarch.ID, eastmarch.ID},
			Links: []domain.DocPersonLink{
				{DocLink: domain.DocLink{URL: "https://example.org/kettle/hidden",
					Roles: []domain.LinkRole{domain.LinkRoleOther}}},
				{
					DocLink: domain.DocLink{URL: wrenURL, Title: new("Wren's songs"),
						CollectionID: &pendingBook.ID, Roles: []domain.LinkRole{domain.LinkRoleWebsite}},
					Performances: []domain.DocPersonPerformance{
						{DocPerformance: domain.DocPerformance{Year: new(int16(2024)),
							DescriptionVerbatim: "Wren at the fair", EventID: &pendingEvent.ID}},
					},
				},
			},
		},
		SortName: wrenName,
	})
	noPage := c.Person(catalogtest.Person{
		Doc: domain.PersonDocument{
			Name: hollisName, Status: domain.PersonStatusPrefersNoPage, NotesMD: new("Private notes."),
			Aliases:        []domain.DocPersonAlias{{Name: "Holly", Kind: domain.PersonAliasKindSCA}},
			Roles:          []domain.PersonRole{domain.PersonRoleSinger},
			AffiliationIDs: []int64{northmarch.ID},
			Links: []domain.DocPersonLink{{DocLink: domain.DocLink{URL: hollisURL,
				Roles: []domain.LinkRole{domain.LinkRoleWebsite}}}},
		},
	})
	c.Piece(catalogtest.Piece{
		Doc: domain.PieceDocument{
			Title: "The Brass Kettle", Kind: domain.PieceKindPoem,
			ConservationStatus: domain.ConservationStatusLowRisk,
			Credits:            []domain.DocCredit{{PersonID: &wren.ID, Role: domain.CreditRoleAuthor}},
		},
		SortTitle: "Brass Kettle",
	})
	pendingPiece := c.Piece(catalogtest.Piece{
		Doc: domain.PieceDocument{Title: "The Pewter Kettle",
			Credits: []domain.DocCredit{{PersonID: &wren.ID, Role: domain.CreditRoleLyrics}}},
		Unapproved: true,
	})
	kettle := c.Piece(catalogtest.Piece{
		Doc: domain.PieceDocument{
			Title:              kettleTitle,
			ConservationStatus: domain.ConservationStatusEndangered,
			IsStub:             true,
			NotesMD:            new("A drinking song."),
			VersionNotesMD:     new("Two verses."),
			LicensingStatus:    domain.LicensingStatusKnown,
			LicensingNotesMD:   new("Free with credit."),
			Aliases: []domain.DocPieceAlias{
				{Text: "Kettle Song", Kind: domain.PieceAliasKindAltTitle},
				{Text: "Old Kettle", Kind: domain.PieceAliasKindRedirect},
			},
			TermIDs: []int64{century.ID, swords.ID, pendingTerm.ID, war.ID, underPending.ID},
			Credits: []domain.DocCredit{
				{PersonID: &wren.ID, Role: domain.CreditRoleLyrics},
				{Role: domain.CreditRoleTune, Attribution: domain.AttributionTraditional,
					Certainty: domain.CertaintyProbable},
				{PersonID: &noPage.ID, Role: domain.CreditRoleTune},
				{PersonID: &pendingWren.ID, Role: domain.CreditRoleAuthor},
				{Role: domain.CreditRoleLyrics, Attribution: domain.AttributionUnknown,
					Certainty: domain.CertaintyDisputed},
			},
			Links: []domain.DocPieceLink{
				{
					DocLink: domain.DocLink{
						URL: kettleVideoURL, Title: new("At the revel"),
						Description: new("A recording."), Platform: new("example.org"),
						PlaceType:    new(domain.LinkPlaceTypeRecordingPlatform),
						CollectionID: &songbook.ID, Locator: new("p. 12"),
						Roles: []domain.LinkRole{domain.LinkRolePerformance, domain.LinkRoleLyrics},
					},
					Performances: []domain.DocPerformance{
						{
							EventID: &revel.ID, Year: new(int16(2025)), Uploader: new("Northmarch Bards"),
							DescriptionVerbatim: revelPerformance,
							PerformerIDs:        []int64{wren.ID, noPage.ID, pendingWren.ID, bards.ID},
						},
						{EventID: &pendingEvent.ID, DescriptionVerbatim: "Sung at the moot"},
					},
				},
				{
					DocLink: domain.DocLink{URL: "https://example.org/kettle/hidden",
						Roles: []domain.LinkRole{domain.LinkRoleSheetMusic}},
					Performances: []domain.DocPerformance{{DescriptionVerbatim: "Hidden performance",
						PerformerIDs: []int64{wren.ID}}},
				},
				{DocLink: domain.DocLink{URL: "https://example.org/kettle/archive", CollectionID: &pendingBook.ID,
					Roles: []domain.LinkRole{domain.LinkRoleTextSource}}},
			},
		},
		SortTitle: "Copper Kettle",
	})
	c.SetLinkState("https://example.org/kettle/hidden", catalogtest.LinkState{IsHidden: true})
	c.SetLinkState("https://example.org/kettle/archive", catalogtest.LinkState{Liveness: domain.LivenessDead,
		LastCheckedAt: &kettleCheckedAt, ArchivedURL: new("https://archive.example.org/kettle")})
	// Position, not insertion order, orders credits and links: reverse the credits, and move the archive link in
	// front of the video link.
	c.Exec("UPDATE piece_credits SET position = 4 - position WHERE piece_id = ?", kettle.ID)
	c.Exec("UPDATE link_attachments a JOIN links l ON l.id = a.link_id SET a.position = 0 WHERE l.url = ?",
		"https://example.org/kettle/archive")
	c.Exec("UPDATE link_attachments a JOIN links l ON l.id = a.link_id SET a.position = 5 WHERE l.url = ?",
		kettleVideoURL)

	// A performance of an unapproved piece on Wren's own link: left out of her page, as it may name the piece.
	c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Fair Recorder",
		Links: []domain.DocPersonLink{{DocLink: domain.DocLink{URL: "https://example.org/pewter",
			Roles: []domain.LinkRole{domain.LinkRolePerformance}},
			Performances: []domain.DocPersonPerformance{{PieceID: &pendingPiece.ID,
				DocPerformance: domain.DocPerformance{DescriptionVerbatim: "Wren sings the Pewter Kettle",
					PerformerIDs: []int64{wren.ID}}}}}}}})

	return detailFixture{
		kettle: kettle, pendingPiece: pendingPiece,
		wren: wren, noPage: noPage, pendingWren: pendingWren,
		prolific: seedProlific(c), songbook: songbook,
	}
})

// seedProlific seeds a person with one more work and one more performance than their page shows.
func seedProlific(c *catalogtest.Catalog) catalogtest.Ref {
	piper := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Prolific Piper"}})

	credits := make([]domain.DocCredit, manyItems)
	performances := make([]domain.DocPerformance, manyItems)
	for i := range manyItems {
		credits[i] = domain.DocCredit{PersonID: &piper.ID, Role: domain.CreditRoleTune}
		performances[i] = domain.DocPerformance{Year: new(int16(1800 + i)), DescriptionVerbatim: fmt.Sprint("Take ", i),
			PerformerIDs: []int64{piper.ID}}
	}
	c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Endless Reel", Credits: credits,
		Links: []domain.DocPieceLink{{DocLink: domain.DocLink{URL: "https://example.org/endless",
			Roles: []domain.LinkRole{domain.LinkRolePerformance}}, Performances: performances}}}})

	return piper
}

func detailRepository(t *testing.T) (*mysql.Repository, detailFixture) {
	t.Helper()

	db, fx := details.Get(t)
	return mysql.New(db), fx
}

var (
	wrenSummary  = domain.PersonSummary{Slug: "wren-of-the-vale", Name: wrenName, Kind: domain.PersonKindCommunityBard}
	bardsSummary = domain.PersonSummary{Slug: "northmarch-bards", Name: "Northmarch Bards", Kind: domain.PersonKindGroup,
		IsStub: true}
	// hollisSummary is the person who prefers no page, as a reference names them: without a link.
	hollisSummary = domain.PersonSummary{Slug: hollisFennSlug, Name: hollisName, Kind: domain.PersonKindCommunityBard,
		IsStub: true, NoLink: true}
	kettleSummary = domain.PieceSummary{
		Slug: "the-copper-kettle", Title: kettleTitle, Kind: domain.PieceKindSong,
		ConservationStatus: domain.ConservationStatusEndangered, IsStub: true, CreditNames: []string{hollisName, wrenName},
	}
	brassSummary = domain.PieceSummary{
		Slug: "the-brass-kettle", Title: "The Brass Kettle", Kind: domain.PieceKindPoem,
		ConservationStatus: domain.ConservationStatusLowRisk, CreditNames: []string{wrenName},
	}
)

// TestRepository_GetPublicPiece reads a piece with every field and list set, with the references to
// entities that are not public left out, credits by attribution and certainty without unapproved persons and
// with the person who prefers no page unlinked, and links without the hidden one, each once with all its roles, with collections,
// locators and performances with public events and performers only.
func TestRepository_GetPublicPiece(t *testing.T) {
	t.Parallel()

	repo, fx := detailRepository(t)
	piece, err := repo.GetPublicPiece(t.Context(), fx.kettle.Slug)
	require.NoError(t, err)
	clearUpdatedAt(t, &piece.UpdatedAt)

	assert.Equal(t, domain.Piece{
		PieceSummary:     kettleSummary,
		NotesMD:          new("A drinking song."),
		VersionNotesMD:   new("Two verses."),
		LicensingStatus:  domain.LicensingStatusKnown,
		LicensingNotesMD: new("Free with credit."),
		Aliases: []domain.PieceAlias{
			{Text: "Kettle Song", Kind: domain.PieceAliasKindAltTitle},
			{Text: "Old Kettle", Kind: domain.PieceAliasKindRedirect},
		},
		// By facet (topic before origin), then position; neither the unapproved mood term nor its approved child.
		Terms: []domain.TermSummary{
			{Slug: "swords", Facet: domain.FacetTopic, DisplayName: swordsName},
			{Slug: "war", Facet: domain.FacetTopic, DisplayName: warName},
			{Slug: "16th-century", Facet: domain.FacetOrigin, DisplayName: "16th Century"},
		},
		// By position, which is not the order they were written in.
		Credits: []domain.PieceCredit{
			{Role: domain.CreditRoleLyrics, Attribution: domain.AttributionUnknown,
				Certainty: domain.CertaintyDisputed},
			{Role: domain.CreditRoleTune, Attribution: domain.AttributionNamed, Certainty: domain.CertaintyCertain,
				Person: &hollisSummary},
			{Role: domain.CreditRoleTune, Attribution: domain.AttributionTraditional,
				Certainty: domain.CertaintyProbable},
			{Role: domain.CreditRoleLyrics, Attribution: domain.AttributionNamed, Certainty: domain.CertaintyCertain,
				Person: &wrenSummary},
		},
		Links: []domain.PieceLink{
			{Link: domain.Link{
				URL: "https://example.org/kettle/archive", Liveness: domain.LivenessDead,
				LastCheckedAt: &kettleCheckedAt, ArchivedURL: new("https://archive.example.org/kettle"),
				Roles: []domain.LinkRole{domain.LinkRoleTextSource},
			}},
			{
				Link: domain.Link{
					URL: kettleVideoURL, Title: new("At the revel"),
					Description: new("A recording."), Platform: new("example.org"),
					PlaceType:  new(domain.LinkPlaceTypeRecordingPlatform),
					Collection: &domain.CollectionRef{ID: fx.songbook.ID, Name: "The Northmarch Songbook"},
					Locator:    new("p. 12"), Liveness: domain.LivenessUnknown,
					Roles: []domain.LinkRole{domain.LinkRolePerformance, domain.LinkRoleLyrics},
				},
				Performances: []domain.PiecePerformance{
					{
						Year: new(int16(2025)), Uploader: new("Northmarch Bards"),
						DescriptionVerbatim: revelPerformance,
						Event:               &domain.EventSummary{Slug: revelSlug, Name: revelName},
						Performers:          []domain.PersonSummary{wrenSummary, hollisSummary, bardsSummary},
					},
					{DescriptionVerbatim: "Sung at the moot"},
				},
			},
		},
	}, piece)
}

// TestRepository_GetPublicPiece_NotFound checks that an unapproved piece is not found, like an unknown one.
func TestRepository_GetPublicPiece_NotFound(t *testing.T) {
	t.Parallel()

	repo, fx := detailRepository(t)
	for _, slug := range []string{fx.pendingPiece.Slug, "no-such-piece", "The-Copper-Kettle"} {
		_, err := repo.GetPublicPiece(t.Context(), slug)
		require.ErrorIs(t, err, datasources.ErrNotFound, slug)
	}
}

// TestRepository_GetPublicPerson reads a person with public aliases only, roles, affiliations without the
// unapproved one, links without the hidden one, works on public pieces and performances.
func TestRepository_GetPublicPerson(t *testing.T) {
	t.Parallel()

	repo, fx := detailRepository(t)
	person, err := repo.GetPublicPerson(t.Context(), fx.wren.Slug)
	require.NoError(t, err)
	clearUpdatedAt(t, &person.UpdatedAt)

	assert.Equal(t, domain.Person{
		PersonSummary: wrenSummary,
		Status:        domain.PersonStatusActive,
		Profile: &domain.PersonProfile{
			NotesMD:          new("A bard of the marches."),
			LicensingStatus:  domain.LicensingStatusKnown,
			LicensingNotesMD: new("Sing freely."),
			Aliases: []domain.PersonAlias{
				{Name: "Wren", Kind: domain.PersonAliasKindSCA},
				{Name: "Jo Legalname", Kind: domain.PersonAliasKindLegal},
				{Name: "Little Wren", Kind: domain.PersonAliasKindSCA},
			},
			Roles: []domain.PersonRole{domain.PersonRoleSinger, domain.PersonRoleSongwriter},
			// By position.
			Affiliations: []domain.TermSummary{
				{Slug: "eastmarch", Facet: domain.FacetAffiliation, DisplayName: eastmarchName},
				{Slug: "northmarch", Facet: domain.FacetAffiliation, DisplayName: northmarchName},
			},
			Links: []domain.Link{{URL: wrenURL, Title: new("Wren's songs"),
				Liveness: domain.LivenessUnknown, Roles: []domain.LinkRole{domain.LinkRoleWebsite}}},
			Works: []domain.PersonWork{
				{Piece: brassSummary, Role: domain.CreditRoleAuthor, Certainty: domain.CertaintyCertain},
				{Piece: kettleSummary, Role: domain.CreditRoleLyrics, Certainty: domain.CertaintyCertain},
			},
			WorksTotal: 2,
			Performances: []domain.PersonPerformance{
				{
					Piece: &kettleSummary,
					Link: domain.PerformanceLink{URL: kettleVideoURL, Title: new("At the revel"),
						Platform: new("example.org")},
					Year:                new(int16(2025)),
					Event:               &domain.EventSummary{Slug: revelSlug, Name: revelName},
					DescriptionVerbatim: revelPerformance,
				},
				{
					Link:                domain.PerformanceLink{URL: wrenURL, Title: new("Wren's songs")},
					Year:                new(int16(2024)),
					DescriptionVerbatim: "Wren at the fair",
				},
			},
			PerformancesTotal: 2,
		},
	}, person)
}

// TestRepository_GetPublicPerson_NoPage checks that a person who prefers no page has only their identity, as a
// stub.
func TestRepository_GetPublicPerson_NoPage(t *testing.T) {
	t.Parallel()

	repo, fx := detailRepository(t)
	person, err := repo.GetPublicPerson(t.Context(), fx.noPage.Slug)
	require.NoError(t, err)
	clearUpdatedAt(t, &person.UpdatedAt)

	assert.Equal(t, domain.Person{
		PersonSummary: domain.PersonSummary{Slug: "hollis-fenn", Name: hollisName, Kind: domain.PersonKindCommunityBard,
			IsStub: true},
		Status: domain.PersonStatusPrefersNoPage,
	}, person)
}

// TestRepository_GetPublicPerson_Caps checks that works and performances stop at the cap, with totals that
// count every one, and that performances come newest year first.
func TestRepository_GetPublicPerson_Caps(t *testing.T) {
	t.Parallel()

	repo, fx := detailRepository(t)
	person, err := repo.GetPublicPerson(t.Context(), fx.prolific.Slug)
	require.NoError(t, err)
	clearUpdatedAt(t, &person.UpdatedAt)

	profile := person.Profile
	require.NotNil(t, profile)
	assert.Len(t, profile.Works, domain.MaxNestedListItems)
	assert.Equal(t, int64(manyItems), profile.WorksTotal)
	require.Len(t, profile.Performances, domain.MaxNestedListItems)
	assert.Equal(t, int64(manyItems), profile.PerformancesTotal)
	assert.Equal(t, new(int16(1800+manyItems-1)), profile.Performances[0].Year)
	assert.Equal(t, new(int16(1801)), profile.Performances[domain.MaxNestedListItems-1].Year)
}

func TestRepository_GetPublicPerson_NotFound(t *testing.T) {
	t.Parallel()

	repo, fx := detailRepository(t)
	for _, slug := range []string{fx.pendingWren.Slug, "no-such-person"} {
		_, err := repo.GetPublicPerson(t.Context(), slug)
		require.ErrorIs(t, err, datasources.ErrNotFound, slug)
	}
}

// TestRepository_CatalogueReads_UnknownEnumValue checks that a value a newer release added to an ENUM fails the
// read, rather than reaching a response that promises a closed set of values.
func TestRepository_CatalogueReads_UnknownEnumValue(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	c := catalogtest.New(t, db)
	piece := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: kettleTitle}})
	person := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: wrenName}})
	for _, stmt := range []string{
		"ALTER TABLE pieces MODIFY kind ENUM('song', 'poem', 'story', 'dance_music', 'instrumental', 'ballad') " +
			"NOT NULL",
		"UPDATE pieces SET kind = 'ballad'",
		"ALTER TABLE person_roles MODIFY role ENUM('singer', 'songwriter', 'musician', 'instrumentalist', " +
			"'storyteller', 'poet', 'jester', 'dancer', 'harper') NOT NULL",
		fmt.Sprintf("INSERT INTO person_roles (person_id, role) VALUES (%d, 'harper')", person.ID),
	} {
		_, err := db.ExecContext(t.Context(), stmt)
		require.NoError(t, err, stmt)
	}

	repo := mysql.New(db)
	_, err := repo.GetPublicPiece(t.Context(), piece.Slug)
	require.ErrorIs(t, err, domain.ErrUnknownEnumValue)
	require.ErrorContains(t, err, "[ballad]")
	require.NotErrorIs(t, err, datasources.ErrNotFound)

	_, err = repo.GetPublicPerson(t.Context(), person.Slug)
	require.ErrorIs(t, err, domain.ErrUnknownEnumValue)
	require.ErrorContains(t, err, "[harper]")
}

// TestRepository_PieceCreditNamesAndUpdatedAt checks that a piece's credit_names are up to five public persons in
// credit order without repeats, on its page and in a list, and that updated_at is the current revision's approval
// time, or its creation time when nobody approved it.
func TestRepository_PieceCreditNamesAndUpdatedAt(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	c := catalogtest.New(t, db)
	var credits []domain.DocCredit
	var want []string
	for _, name := range []string{"Ann", "Bea", "Cat", "Dot", "Eve", "Fay"} {
		person := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: name}})
		credits = append(credits, domain.DocCredit{PersonID: &person.ID, Role: domain.CreditRoleLyrics})
		if len(want) < 5 {
			want = append(want, name)
		}
		if name == "Ann" {
			credits = append(credits, domain.DocCredit{PersonID: &person.ID, Role: domain.CreditRoleTune})
		}
	}
	piece := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: kettleTitle, Credits: credits}})
	repo := mysql.New(db)

	got, err := repo.GetPublicPiece(t.Context(), piece.Slug)
	require.NoError(t, err)
	assert.Equal(t, want, got.CreditNames)
	list, err := repo.ListPublicPieces(t.Context(), datasources.PieceQuery{Sort: domain.PieceSortTitle,
		Page: domain.Page{Limit: domain.DefaultPageLimit}})
	require.NoError(t, err)
	require.Len(t, list.Pieces, 1)
	assert.Equal(t, want, list.Pieces[0].CreditNames)

	var createdAt time.Time
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT created_at FROM revisions WHERE id = ?",
		piece.RevisionID).Scan(&createdAt))
	assert.Equal(t, createdAt, got.UpdatedAt, "an import revision has no reviewer")
	approvedAt := createdAt.Add(time.Hour)
	_, err = db.ExecContext(t.Context(), "UPDATE revisions SET reviewed_at = ? WHERE id = ?", approvedAt,
		piece.RevisionID)
	require.NoError(t, err)
	got, err = repo.GetPublicPiece(t.Context(), piece.Slug)
	require.NoError(t, err)
	assert.Equal(t, approvedAt, got.UpdatedAt)
}

// clearUpdatedAt checks that a page's updated_at is set and clears it, for the tests that compare a whole page;
// TestRepository_PieceCreditNamesAndUpdatedAt checks its value.
func clearUpdatedAt(t *testing.T, at *time.Time) {
	t.Helper()

	require.False(t, at.IsZero(), "updated_at is set")
	*at = time.Time{}
}
