//go:build integration

package mysql_test

import (
	"encoding/json"
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
// a prefers_no_page person referenced from everywhere a person can be; a hidden alias; and a hidden link.
type detailFixture struct {
	kettle, pendingPiece, staleVersion catalogtest.Ref
	wren, ash, noPage, pendingWren     catalogtest.Ref
	prolific                           catalogtest.Ref
	songbook                           catalogtest.Ref
}

// Fixture times; DATETIME and DATE columns are read back in UTC.
var (
	kettleCheckedAt = time.Date(2026, 9, 1, 8, 30, 0, 0, time.UTC)
	// approvedAt is when the revisions of the entities the tests compare exactly were approved (setApprovedAt).
	approvedAt      = time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	firstAssessment = time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	reassessment    = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
)

// Fixture strings the tests compare more than once.
const (
	revelName        = "Midwinter Revel"
	revelSlug        = "midwinter-revel"
	warName          = "War"
	swordsName       = "Swords"
	northmarchName   = "Northmarch"
	eastmarchName    = "Eastmarch"
	hollisName       = "Hollis Fenn"
	revelPerformance = "Sung at the revel"
	wrenName         = "Wren of the Vale"
	wrenURL          = "https://example.org/wren"
	kettleTitle      = "The Copper Kettle"
	kettleVideoURL   = "https://example.org/kettle/video"
)

// What the link checker found of two links of the detail fixture.
// hiddenArchive is the archive of a hidden link, which no read shows.
const hiddenArchive = "https://web.archive.org/web/1/leakmark-archive"

const (
	kettleVideoArchive = "https://web.archive.org/web/20260101000000/https://example.org/kettle/video"
	wrenPageArchive    = "https://web.archive.org/web/20260101000000/https://example.org/wren"
)

// setApprovedAt makes the approval of the entity ref the time at.
func setApprovedAt(c *catalogtest.Catalog, ref catalogtest.Ref, at time.Time) {
	c.Exec("UPDATE revisions SET created_at = ? WHERE id = ?", at, ref.RevisionID)
}

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
			Pronouns:         new("she/her"),
			NotesMD:          new("A bard of the marches."),
			LicensingStatus:  domain.LicensingStatusKnown,
			LicensingNotesMD: new("Sing freely."),
			Aliases: []domain.DocPersonAlias{
				{Name: "Wren", Kind: domain.PersonAliasKindStage, IsPublic: true},
				{Name: "Jo Hiddenname", Kind: domain.PersonAliasKindLegal},
				{Name: "Little Wren", Kind: domain.PersonAliasKindNickname, IsPublic: true},
			},
			Roles:          []domain.PersonRole{domain.PersonRoleSongwriter, domain.PersonRoleSinger},
			AffiliationIDs: []int64{northmarch.ID, southmarch.ID, eastmarch.ID},
			Relations: []domain.DocPersonRelation{
				{ToPersonID: bards.ID, Type: domain.PersonRelationTypeMemberOf},
				{ToPersonID: pendingWren.ID, Type: domain.PersonRelationTypeSameAs},
			},
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
			Name: hollisName, Status: domain.PersonStatusPrefersNoPage, StatusDate: &domain.Date{Time: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
			StatusSourceURL: new("https://example.org/requests/12"), NotesMD: new("Private notes."),
			Aliases:        []domain.DocPersonAlias{{Name: "Holly", Kind: domain.PersonAliasKindNickname, IsPublic: true}},
			Roles:          []domain.PersonRole{domain.PersonRoleSinger},
			AffiliationIDs: []int64{northmarch.ID},
			Relations:      []domain.DocPersonRelation{{ToPersonID: wren.ID, Type: domain.PersonRelationTypeHeirOf}},
			Links: []domain.DocPersonLink{{DocLink: domain.DocLink{URL: "https://example.org/hollis",
				Roles: []domain.LinkRole{domain.LinkRoleWebsite}}}},
		},
	})
	ash := c.Person(catalogtest.Person{Doc: domain.PersonDocument{
		Name: "Ash Tallow", Kind: domain.PersonKindHistorical,
		Relations: []domain.DocPersonRelation{
			{ToPersonID: wren.ID, Type: domain.PersonRelationTypeHeirOf},
			{ToPersonID: noPage.ID, Type: domain.PersonRelationTypeSameAs},
		},
	}})

	brass := c.Piece(catalogtest.Piece{
		Doc: domain.PieceDocument{
			Title: "The Brass Kettle", Kind: domain.PieceKindPoem,
			SecondaryKinds: []domain.PieceKind{domain.PieceKindStory},
			Conservation:   domain.DocConservation{Status: domain.ConservationStatusLowRisk},
			Credits:        []domain.DocCredit{{PersonID: &wren.ID, Role: domain.CreditRoleAuthor}},
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
			SecondaryKinds:     []domain.PieceKind{domain.PieceKindComedy, domain.PieceKindDanceMusic},
			Conservation:       domain.DocConservation{Status: domain.ConservationStatusEndangered},
			IsStub:             true,
			NotesMD:            new("A drinking song."),
			VersionNotesMD:     new("Two verses."),
			LicensingStatus:    domain.LicensingStatusKnown,
			LicensingNotesMD:   new("Free with credit."),
			Licence:            new(domain.LicenceCCBYSA),
			LicenceVersion:     new(domain.LicenceVersion40),
			HistoricalSourceMD: new("A broadside."),
			PeriodStart:        new(int16(1580)),
			PeriodEnd:          new(int16(1600)),
			Aliases: []domain.DocPieceAlias{
				{Text: "Kettle Song", Kind: domain.PieceAliasKindAltTitle},
				{Text: "Old Kettle", Kind: domain.PieceAliasKindNickname},
			},
			Terms: []domain.DocPieceTerm{
				{TermID: century.ID, Part: new(domain.PiecePartTune)},
				{TermID: swords.ID},
				{TermID: pendingTerm.ID},
				{TermID: war.ID, IsPrimary: true},
				{TermID: underPending.ID},
			},
			Relations: []domain.DocPieceRelation{
				{ToPieceID: brass.ID, Type: domain.PieceRelationTypeVariantOf},
				{ToPieceID: pendingPiece.ID, Type: domain.PieceRelationTypeSettingOf},
			},
			Credits: []domain.DocCredit{
				{PersonID: &wren.ID, Role: domain.CreditRoleLyrics},
				{Role: domain.CreditRoleTune, Attribution: domain.AttributionTraditional,
					Certainty: domain.CertaintyProbable, Part: new(domain.PiecePartTune), Note: new("An old reel.")},
				{PersonID: &noPage.ID, Role: domain.CreditRoleMusic, Note: new("Hollis's tune")},
				{PersonID: &pendingWren.ID, Role: domain.CreditRoleArrangement},
				{Role: domain.CreditRoleTranslation, Attribution: domain.AttributionUnknown,
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
		ConservationHistory: []catalogtest.ConservationEntry{
			{Status: domain.ConservationStatusEndangered, Reason: "Fewer singers.", At: reassessment},
			{Status: domain.ConservationStatusLowRisk, Reason: "First assessment.",
				SourceURL: new("https://example.org/assessment"), At: firstAssessment},
		},
	})
	c.SetLinkState("https://example.org/kettle/hidden", catalogtest.LinkState{IsHidden: true,
		Liveness: domain.LivenessDead, LastCheckedAt: &kettleCheckedAt, ArchivedURL: new(hiddenArchive)})
	c.SetLinkState(kettleVideoURL, catalogtest.LinkState{Liveness: domain.LivenessOK, LastCheckedAt: &kettleCheckedAt,
		ArchivedURL: new(kettleVideoArchive)})
	c.SetLinkState(wrenURL, catalogtest.LinkState{Liveness: domain.LivenessBlocked, ArchivedURL: new(wrenPageArchive)})
	setApprovedAt(c, kettle, approvedAt)
	// A later pending revision is not an approval.
	c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece, EntityID: kettle.ID,
		Snapshot: domain.PieceDocument{Title: "The Copper Kettle, Revised"}, Status: domain.RevisionStatusPending,
		CreatedAt: approvedAt.Add(time.Hour)})
	// A version left in the column by a licence change an earlier release made.
	staleVersion := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "The Stale Kettle",
		LicensingStatus: domain.LicensingStatusKnown, Licence: new(domain.LicenceARR),
		LicenceVersion: new(domain.LicenceVersion30)}})
	setApprovedAt(c, wren, approvedAt)
	setApprovedAt(c, noPage, approvedAt)
	c.SetLinkState("https://example.org/kettle/archive", catalogtest.LinkState{Liveness: domain.LivenessDead,
		LastCheckedAt: &kettleCheckedAt, ArchivedURL: new("https://archive.example.org/kettle")})
	// Position, not insertion order, orders credits and links: reverse the credits, and move the archive link in
	// front of the video link.
	c.Exec("UPDATE piece_credits SET position = 4 - position WHERE piece_id = ?", kettle.ID)
	c.Exec("UPDATE link_attachments a JOIN links l ON l.id = a.link_id SET a.position = 0 WHERE l.url = ?",
		"https://example.org/kettle/archive")
	c.Exec("UPDATE link_attachments a JOIN links l ON l.id = a.link_id SET a.position = 5 WHERE l.url = ?",
		kettleVideoURL)

	// Incoming relations, from a public piece and from an unapproved one.
	c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "The Tin Kettle",
		Relations: []domain.DocPieceRelation{{ToPieceID: kettle.ID, Type: domain.PieceRelationTypeContrafactumOf}}}})
	c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "The Iron Kettle",
		Relations: []domain.DocPieceRelation{{ToPieceID: kettle.ID, Type: domain.PieceRelationTypeVariantOf}}},
		Unapproved: true})
	// A performance of an unapproved piece on Wren's own link: left out of her page, as it may name the piece.
	c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Fair Recorder",
		Links: []domain.DocPersonLink{{DocLink: domain.DocLink{URL: "https://example.org/pewter",
			Roles: []domain.LinkRole{domain.LinkRolePerformance}},
			Performances: []domain.DocPersonPerformance{{PieceID: &pendingPiece.ID,
				DocPerformance: domain.DocPerformance{DescriptionVerbatim: "Wren sings the Pewter Kettle",
					PerformerIDs: []int64{wren.ID}}}}}}}})

	return detailFixture{
		kettle: kettle, pendingPiece: pendingPiece, staleVersion: staleVersion,
		wren: wren, ash: ash, noPage: noPage, pendingWren: pendingWren,
		prolific: seedProlific(c), songbook: songbook,
	}
})

// seedProlific seeds a person with one more work and one more performance than their page shows.
func seedProlific(c *catalogtest.Catalog) catalogtest.Ref {
	piper := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Prolific Piper"}})

	credits := make([]domain.DocCredit, manyItems)
	performances := make([]domain.DocPerformance, manyItems)
	for i := range manyItems {
		credits[i] = domain.DocCredit{PersonID: &piper.ID, Role: domain.CreditRoleMusic}
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
	kettleSummary = domain.PieceSummary{
		Slug: "the-copper-kettle", Title: kettleTitle, Kind: domain.PieceKindSong,
		SecondaryKinds:     []domain.PieceKind{domain.PieceKindDanceMusic, domain.PieceKindComedy},
		ConservationStatus: domain.ConservationStatusEndangered, IsStub: true,
	}
	brassSummary = domain.PieceSummary{
		Slug: "the-brass-kettle", Title: "The Brass Kettle", Kind: domain.PieceKindPoem,
		SecondaryKinds: []domain.PieceKind{domain.PieceKindStory}, ConservationStatus: domain.ConservationStatusLowRisk,
	}
)

// TestRepository_GetPublicPiece reads a piece with every field and list set, with the references to
// entities that are not public left out, credits by attribution and certainty without no-page or
// unapproved persons, and links without the hidden one, each once with all its roles, with collections,
// locators and performances with public events and performers only.
func TestRepository_GetPublicPiece(t *testing.T) {
	t.Parallel()

	repo, fx := detailRepository(t)
	piece, err := repo.GetPublicPiece(t.Context(), fx.kettle.Slug)
	require.NoError(t, err)

	tune := domain.PiecePartTune
	assert.Equal(t, domain.Piece{
		PieceSummary:       kettleSummary,
		UpdatedAt:          approvedAt,
		NotesMD:            new("A drinking song."),
		VersionNotesMD:     new("Two verses."),
		LicensingStatus:    domain.LicensingStatusKnown,
		LicensingNotesMD:   new("Free with credit."),
		Licence:            new(domain.LicenceCCBYSA),
		LicenceVersion:     new(domain.LicenceVersion40),
		HistoricalSourceMD: new("A broadside."),
		PeriodStart:        new(int16(1580)),
		PeriodEnd:          new(int16(1600)),
		Aliases: []domain.PieceAlias{
			{Text: "Kettle Song", Kind: domain.PieceAliasKindAltTitle},
			{Text: "Old Kettle", Kind: domain.PieceAliasKindNickname},
		},
		// By facet (topic before origin), then position; neither the unapproved mood term nor its approved child.
		Terms: []domain.PieceTerm{
			{Term: domain.TermSummary{Slug: "swords", Facet: domain.FacetTopic, DisplayName: swordsName}},
			{Term: domain.TermSummary{Slug: "war", Facet: domain.FacetTopic, DisplayName: warName}, IsPrimary: true},
			{Term: domain.TermSummary{Slug: "16th-century", Facet: domain.FacetOrigin, DisplayName: "16th Century"},
				Part: &tune},
		},
		// By position, which is not the order they were written in.
		Credits: []domain.PieceCredit{
			{Role: domain.CreditRoleTranslation, Attribution: domain.AttributionUnknown,
				Certainty: domain.CertaintyDisputed},
			{Role: domain.CreditRoleTune, Attribution: domain.AttributionTraditional,
				Certainty: domain.CertaintyProbable, Part: &tune, Note: new("An old reel.")},
			{Role: domain.CreditRoleLyrics, Attribution: domain.AttributionNamed, Certainty: domain.CertaintyCertain,
				Person: &wrenSummary},
		},
		Relations: []domain.PieceRelation{
			{Type: domain.PieceRelationTypeVariantOf, Direction: domain.RelationDirectionOutgoing, Piece: brassSummary},
			{Type: domain.PieceRelationTypeContrafactumOf, Direction: domain.RelationDirectionIncoming,
				Piece: domain.PieceSummary{Slug: "the-tin-kettle", Title: "The Tin Kettle", Kind: domain.PieceKindSong,
					ConservationStatus: domain.ConservationStatusUnassessed}},
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
					Locator:    new("p. 12"), Liveness: domain.LivenessOK, LastCheckedAt: &kettleCheckedAt,
					ArchivedURL: new(kettleVideoArchive),
					Roles:       []domain.LinkRole{domain.LinkRolePerformance, domain.LinkRoleLyrics},
				},
				Performances: []domain.PiecePerformance{
					{
						Year: new(int16(2025)), Uploader: new("Northmarch Bards"),
						DescriptionVerbatim: revelPerformance,
						Event: &domain.EventSummary{Slug: revelSlug, Name: revelName,
							Kind: domain.EventKindEvent},
						Performers: []domain.PersonSummary{wrenSummary, bardsSummary},
					},
					{DescriptionVerbatim: "Sung at the moot"},
				},
			},
		},
		ConservationHistory: []domain.ConservationChange{
			{Status: domain.ConservationStatusLowRisk, Reason: "First assessment.",
				SourceURL: new("https://example.org/assessment"), At: firstAssessment},
			{Status: domain.ConservationStatusEndangered, Reason: "Fewer singers.", At: reassessment},
		},
		// The video's place only: the archive link has no place type (and is dead), the hidden link is not counted.
		ConservationSuggestion: &domain.ConservationSuggestion{Status: domain.ConservationStatusEndangered,
			PlaceCount: 1, Places: []domain.LinkPlaceType{domain.LinkPlaceTypeRecordingPlatform}},
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
// unapproved one, relations in both directions without unapproved or no-page persons, links without the hidden
// one, works on public pieces and performances.
func TestRepository_GetPublicPerson(t *testing.T) {
	t.Parallel()

	repo, fx := detailRepository(t)
	person, err := repo.GetPublicPerson(t.Context(), fx.wren.Slug)
	require.NoError(t, err)

	assert.Equal(t, domain.Person{
		PersonSummary: wrenSummary,
		UpdatedAt:     approvedAt,
		Status:        domain.PersonStatusActive,
		Profile: &domain.PersonProfile{
			Pronouns:         new("she/her"),
			NotesMD:          new("A bard of the marches."),
			LicensingStatus:  domain.LicensingStatusKnown,
			LicensingNotesMD: new("Sing freely."),
			Aliases: []domain.PersonAlias{
				{Name: "Wren", Kind: domain.PersonAliasKindStage},
				{Name: "Little Wren", Kind: domain.PersonAliasKindNickname},
			},
			Roles: []domain.PersonRole{domain.PersonRoleSinger, domain.PersonRoleSongwriter},
			// By position.
			Affiliations: []domain.TermSummary{
				{Slug: "eastmarch", Facet: domain.FacetAffiliation, DisplayName: eastmarchName},
				{Slug: "northmarch", Facet: domain.FacetAffiliation, DisplayName: northmarchName},
			},
			Relations: []domain.PersonRelation{
				{Type: domain.PersonRelationTypeMemberOf, Direction: domain.RelationDirectionOutgoing, Person: bardsSummary},
				{Type: domain.PersonRelationTypeHeirOf, Direction: domain.RelationDirectionIncoming,
					Person: domain.PersonSummary{Slug: "ash-tallow", Name: "Ash Tallow", Kind: domain.PersonKindHistorical}},
			},
			Links: []domain.Link{{URL: wrenURL, Title: new("Wren's songs"), Liveness: domain.LivenessBlocked,
				ArchivedURL: new(wrenPageArchive), Roles: []domain.LinkRole{domain.LinkRoleWebsite}}},
			Works: []domain.PersonWork{
				{Piece: brassSummary, Role: domain.CreditRoleAuthor, Certainty: domain.CertaintyCertain},
				{Piece: kettleSummary, Role: domain.CreditRoleLyrics, Certainty: domain.CertaintyCertain},
			},
			WorksTotal: 2,
			Performances: []domain.PersonPerformance{
				{
					Piece: &kettleSummary,
					Link: domain.PerformanceLink{URL: kettleVideoURL, Title: new("At the revel"),
						Platform: new("example.org"), Liveness: domain.LivenessOK, LastCheckedAt: &kettleCheckedAt,
						ArchivedURL: new(kettleVideoArchive)},
					Year: new(int16(2025)),
					Event: &domain.EventSummary{Slug: revelSlug, Name: revelName,
						Kind: domain.EventKindEvent},
					DescriptionVerbatim: revelPerformance,
				},
				{
					Link: domain.PerformanceLink{URL: wrenURL, Title: new("Wren's songs"), Liveness: domain.LivenessBlocked,
						ArchivedURL: new(wrenPageArchive)},
					Year:                new(int16(2024)),
					DescriptionVerbatim: "Wren at the fair",
				},
			},
			PerformancesTotal: 2,
		},
	}, person)
}

// TestRepository_GetPublicPerson_RelationsSkipNoPage checks that a relation to a no-page person is left out from
// the other side too.
func TestRepository_GetPublicPerson_RelationsSkipNoPage(t *testing.T) {
	t.Parallel()

	repo, fx := detailRepository(t)
	person, err := repo.GetPublicPerson(t.Context(), fx.ash.Slug)
	require.NoError(t, err)

	require.NotNil(t, person.Profile)
	assert.Equal(t, []domain.PersonRelation{{Type: domain.PersonRelationTypeHeirOf,
		Direction: domain.RelationDirectionOutgoing, Person: wrenSummary}}, person.Profile.Relations)
}

// TestRepository_GetPublicPerson_NoPage checks that a person who prefers no page has only their identity, as a
// stub.
func TestRepository_GetPublicPerson_NoPage(t *testing.T) {
	t.Parallel()

	repo, fx := detailRepository(t)
	person, err := repo.GetPublicPerson(t.Context(), fx.noPage.Slug)
	require.NoError(t, err)

	assert.Equal(t, domain.Person{
		PersonSummary:   domain.PersonSummary{Slug: "hollis-fenn", Name: hollisName, Kind: domain.PersonKindCommunityBard, IsStub: true},
		UpdatedAt:       approvedAt,
		Status:          domain.PersonStatusPrefersNoPage,
		StatusDate:      new(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)),
		StatusSourceURL: new("https://example.org/requests/12"),
	}, person)
}

// TestRepository_GetPublicPerson_Caps checks that works and performances stop at the cap, with totals that
// count every one, and that performances come newest year first.
func TestRepository_GetPublicPerson_Caps(t *testing.T) {
	t.Parallel()

	repo, fx := detailRepository(t)
	person, err := repo.GetPublicPerson(t.Context(), fx.prolific.Slug)
	require.NoError(t, err)

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
		"ALTER TABLE pieces MODIFY kind ENUM('song', 'poem', 'story', 'dance_music', 'instrumental', 'comedy', " +
			"'ballad') NOT NULL",
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

// TestRepository_GetPublicPiece_RestrictedKeepsLinks pins that a restricted piece's public page still serves its
// visible links: no read filters links by conservation status, and how they are shown is the client's policy.
func TestRepository_GetPublicPiece_RestrictedKeepsLinks(t *testing.T) {
	t.Parallel()

	db := catalogtest.Writable(t)
	c := catalogtest.New(t, db)
	source := new("https://example.org/restriction")
	const linkURL = "https://example.org/hush-song/recording"
	c.Piece(catalogtest.Piece{Slug: "hush-song", Doc: domain.PieceDocument{
		Title:        "The Hush Song",
		Conservation: domain.DocConservation{Status: domain.ConservationStatusRestricted, SourceURL: source},
		Links: []domain.DocPieceLink{{DocLink: domain.DocLink{URL: linkURL,
			Roles: []domain.LinkRole{domain.LinkRolePerformance}}}},
	}, ConservationHistory: []catalogtest.ConservationEntry{{Status: domain.ConservationStatusRestricted,
		Reason: "The family asked that it not be spread.", SourceURL: source}}})

	piece, err := mysql.New(db).GetPublicPiece(t.Context(), "hush-song")
	require.NoError(t, err)
	assert.Equal(t, domain.ConservationStatusRestricted, piece.ConservationStatus)
	require.Len(t, piece.Links, 1)
	assert.Equal(t, linkURL, piece.Links[0].URL)
	require.Len(t, piece.ConservationHistory, 1)
	assert.Equal(t, source, piece.ConservationHistory[0].SourceURL, "the newest restricted entry holds the source")
}

// TestRepository_GetPublicPiece_UpdatedAt checks that a piece was updated when its current revision was approved,
// whatever later revisions are pending.
func TestRepository_GetPublicPiece_UpdatedAt(t *testing.T) {
	t.Parallel()

	repo, fx := detailRepository(t)
	piece, err := repo.GetPublicPiece(t.Context(), fx.kettle.Slug)
	require.NoError(t, err)
	assert.Equal(t, approvedAt, piece.UpdatedAt)
}

// TestRepository_GetPublicPerson_UpdatedAt checks the same of persons, including one who prefers no page.
func TestRepository_GetPublicPerson_UpdatedAt(t *testing.T) {
	t.Parallel()

	repo, fx := detailRepository(t)
	for _, ref := range []catalogtest.Ref{fx.wren, fx.noPage} {
		person, err := repo.GetPublicPerson(t.Context(), ref.Slug)
		require.NoError(t, err)
		assert.Equal(t, approvedAt, person.UpdatedAt, ref.Slug)
	}
}

// TestRepository_GetPublicPiece_StaleLicenceVersionHidden checks that a version stored beside a licence that has
// none, as a licence change by an earlier release leaves it, is not shown.
func TestRepository_GetPublicPiece_StaleLicenceVersionHidden(t *testing.T) {
	t.Parallel()

	repo, fx := detailRepository(t)
	piece, err := repo.GetPublicPiece(t.Context(), fx.staleVersion.Slug)
	require.NoError(t, err)
	assert.Equal(t, new(domain.LicenceARR), piece.Licence)
	assert.Nil(t, piece.LicenceVersion)
}

// TestRepository_PerformanceLinkLiveness checks that the link of a performance on a person's page carries what the
// link checker found, and that the archive of a hidden link, whose performance is left out, is shown nowhere.
func TestRepository_PerformanceLinkLiveness(t *testing.T) {
	t.Parallel()

	repo, fx := detailRepository(t)
	person, err := repo.GetPublicPerson(t.Context(), fx.wren.Slug)
	require.NoError(t, err)

	require.Len(t, person.Profile.Performances, 2)
	assert.Equal(t, domain.PerformanceLink{URL: kettleVideoURL, Title: new("At the revel"), Platform: new("example.org"),
		Liveness: domain.LivenessOK, LastCheckedAt: &kettleCheckedAt, ArchivedURL: new(kettleVideoArchive)},
		person.Profile.Performances[0].Link)
	assert.Equal(t, domain.PerformanceLink{URL: wrenURL, Title: new("Wren's songs"), Liveness: domain.LivenessBlocked,
		ArchivedURL: new(wrenPageArchive)}, person.Profile.Performances[1].Link)

	piece, err := repo.GetPublicPiece(t.Context(), fx.kettle.Slug)
	require.NoError(t, err)
	for _, read := range []any{person, piece} {
		encoded, err := json.Marshal(read)
		require.NoError(t, err)
		assert.NotContains(t, string(encoded), hiddenArchive)
	}
}
