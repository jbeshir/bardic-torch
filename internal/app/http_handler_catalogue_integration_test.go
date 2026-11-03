//go:build integration

package app

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/jbeshir/bardic-torch/internal/transport/web/controller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	catalogueKettleTitle = "The Copper Kettle"
	catalogueNotes       = "Sung round the fire at the end of the night."
)

// catalogueSlugs returns the slugs of the pieces of a list, in order.
func catalogueSlugs(pieces []controller.PieceSummary) []string {
	slugs := make([]string, len(pieces))
	for i, p := range pieces {
		slugs[i] = p.Slug
	}
	return slugs
}

// catalogueTermCounts maps each term slug of a piece search's facets to its count.
func catalogueTermCounts(list controller.PieceList) map[string]int64 {
	counts := make(map[string]int64, len(list.Facets.Terms))
	for _, c := range list.Facets.Terms {
		counts[c.Slug] = c.Count
	}
	return counts
}

// TestHTTP_CatalogueEndToEnd walks the public catalogue through the production handler over a real repository
// holding a small invented catalogue: list, facet filter, search, details, history, revision and needs-work. The
// reads are public, so no credentials are sent. Every response goes through the contract helper.
func TestHTTP_CatalogueEndToEnd(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	c := catalogtest.New(t, db)

	war := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic, Name: "War"}})
	swords := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic, Name: "Swords", ParentID: &war.ID}})
	warlike := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetMood, Name: "Warlike"}})
	gaelic := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetCulture, Name: "Gaelic"}})

	wren := c.Person(catalogtest.Person{Doc: domain.PersonDocument{
		Name:            "Wren of the Vale",
		LicensingStatus: domain.LicensingStatusKnown,
		Aliases:         []domain.DocPersonAlias{{Name: "Jo Marker", Kind: domain.PersonAliasKindLegal}},
	}})

	known := func(doc domain.PieceDocument) domain.PieceDocument {
		doc.Kind = domain.PieceKindSong
		doc.LicensingStatus = domain.LicensingStatusKnown
		doc.Conservation.Status = domain.ConservationStatusLowRisk
		return doc
	}
	kettleDoc := known(domain.PieceDocument{
		Title: catalogueKettleTitle,
		Terms: []domain.DocPieceTerm{
			{TermID: swords.ID, IsPrimary: true},
			{TermID: warlike.ID},
		},
		Credits: []domain.DocCredit{{
			PersonID: &wren.ID, Role: domain.CreditRoleLyrics,
			Attribution: domain.AttributionNamed, Certainty: domain.CertaintyCertain,
		}},
	})
	kettle := c.Piece(catalogtest.Piece{Doc: kettleDoc, SortTitle: "Copper Kettle"})
	harbour := c.Piece(catalogtest.Piece{Doc: known(domain.PieceDocument{
		Title: "Harbour Lights",
		Terms: []domain.DocPieceTerm{{TermID: war.ID, IsPrimary: true}, {TermID: gaelic.ID}},
	})})
	lanterns := c.Piece(catalogtest.Piece{Doc: known(domain.PieceDocument{
		Title: "Lanterns of Dawn",
		Terms: []domain.DocPieceTerm{{TermID: gaelic.ID, IsPrimary: true}},
	})})
	zephyr := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Zephyr Reel", IsStub: true}})
	c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Unsung Draft"}, Unapproved: true})

	// A second approved revision of the kettle, which changes only its notes. Its document is spelled out in full,
	// because catalogtest applies defaults only to the entity rows it seeds.
	notes := catalogueNotes
	editedDoc := kettleDoc
	editedDoc.SchemaVersion = domain.DocumentSchemaVersion
	editedDoc.NotesMD = &notes
	editedID := c.Revision(catalogtest.Revision{
		EntityType:     domain.EntityTypePiece,
		EntityID:       kettle.ID,
		Snapshot:       editedDoc,
		Status:         domain.RevisionStatusApproved,
		Source:         domain.RevisionSourceUser,
		BaseRevisionID: kettle.RevisionID,
		Summary:        "Add notes",
		CreatedAt:      time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	})

	repository := mysql.New(db)
	e := endToEnd{
		handler:  NewHTTPHandler(repositoryHTTPDependencies(repository, nil, passingTurnstile{})),
		contract: openapitest.New(t),
	}

	// List: every public piece, by title, with facet counts. The unapproved draft is absent.
	all := sendOK[controller.PieceList](t, e, http.MethodGet, "/v1/pieces", "", "", http.StatusOK)
	assert.EqualValues(t, 4, all.Total)
	assert.Equal(t, []string{kettle.Slug, harbour.Slug, lanterns.Slug, zephyr.Slug}, catalogueSlugs(all.Data))
	assert.Equal(t, map[string]int64{
		war.Slug: 2, swords.Slug: 1, warlike.Slug: 1, gaelic.Slug: 2,
	}, catalogueTermCounts(all))

	// Facet filter: War matches the pieces tagged with it or its child Swords. Counts ignore their own facet's
	// selection, so War and Swords keep their unfiltered counts, while the other facets count within the match.
	war2 := sendOK[controller.PieceList](t, e, http.MethodGet, "/v1/pieces?term="+war.Slug, "", "", http.StatusOK)
	assert.EqualValues(t, 2, war2.Total)
	assert.Equal(t, []string{kettle.Slug, harbour.Slug}, catalogueSlugs(war2.Data))
	assert.Equal(t, map[string]int64{
		war.Slug: 2, swords.Slug: 1, warlike.Slug: 1, gaelic.Slug: 1,
	}, catalogueTermCounts(war2))

	// Across facets the terms AND: War and Gaelic leave only the harbour piece.
	both := sendOK[controller.PieceList](t, e, http.MethodGet, "/v1/pieces?term="+war.Slug+"&term="+gaelic.Slug, "", "",
		http.StatusOK)
	assert.Equal(t, []string{harbour.Slug}, catalogueSlugs(both.Data))

	// An unknown term is a 400, like a non-public one.
	sendOK[errorResponse](t, e, http.MethodGet, "/v1/pieces?term=no-such-term", "", "", http.StatusBadRequest)

	// Search: a title word finds the piece.
	found := sendOK[controller.PieceList](t, e, http.MethodGet, "/v1/pieces?q="+url.QueryEscape("kettle"), "", "",
		http.StatusOK)
	assert.Equal(t, []string{kettle.Slug}, catalogueSlugs(found.Data))

	// Piece detail.
	piece := sendOK[controller.Piece](t, e, http.MethodGet, "/v1/pieces/"+kettle.Slug, "", "", http.StatusOK)
	assert.Equal(t, catalogueKettleTitle, piece.Title)
	require.Len(t, piece.Terms, 2)
	require.Len(t, piece.Credits, 1)
	require.NotNil(t, piece.Credits[0].Person)
	assert.Equal(t, wren.Slug, piece.Credits[0].Person.Slug)

	// Term detail: War counts its descendants' pieces and lists them.
	term := sendOK[controller.Term](t, e, http.MethodGet, "/v1/terms/"+war.Slug, "", "", http.StatusOK)
	assert.EqualValues(t, 2, term.PieceCount)
	assert.Equal(t, []string{kettle.Slug, harbour.Slug}, catalogueSlugs(term.Pieces))
	require.Len(t, term.Children, 1)
	assert.Equal(t, swords.Slug, term.Children[0].Term.Slug)

	// Person detail: the work is listed, and the hidden legal alias is not.
	person := sendOK[controller.Person](t, e, http.MethodGet, "/v1/persons/"+wren.Slug, "", "", http.StatusOK)
	require.NotNil(t, person.Profile)
	require.Len(t, person.Profile.Works, 1)
	assert.Equal(t, kettle.Slug, person.Profile.Works[0].Piece.Slug)
	assert.Empty(t, person.Profile.Aliases)

	// History: the newest approved revision first.
	history := sendOK[controller.RevisionHistory](t, e, http.MethodGet, "/v1/pieces/"+kettle.Slug+"/history", "", "",
		http.StatusOK)
	assert.EqualValues(t, 2, history.Total)
	require.Len(t, history.Data, 2)
	assert.Equal(t, editedID, history.Data[0].ID)
	assert.Equal(t, kettle.RevisionID, history.Data[1].ID)
	assert.Equal(t, domain.RevisionSourceImport, history.Data[1].Source)

	// Revision detail: the edit shows its base and that only the notes changed.
	revision := sendOK[controller.EntityRevision[controller.PieceSummary, map[string]any]](
		t, e, http.MethodGet, "/v1/revisions/"+strconv.FormatInt(editedID, 10), "", "", http.StatusOK)
	assert.Equal(t, domain.EntityTypePiece, revision.EntityType)
	assert.Equal(t, kettle.Slug, revision.Entity.Slug)
	require.NotNil(t, revision.BaseRevisionID)
	assert.Equal(t, kettle.RevisionID, *revision.BaseRevisionID)
	require.NotNil(t, revision.BaseSnapshot)
	assert.Equal(t, []string{"notes_md"}, revision.ChangedFields)
	assert.Equal(t, catalogueNotes, revision.Snapshot["notes_md"])

	// Needs-work: the stub, unassessed piece of unknown licensing, with a count per reason.
	work := sendOK[controller.NeedsWorkList](t, e, http.MethodGet, "/v1/needs-work", "", "", http.StatusOK)
	assert.EqualValues(t, 1, work.Total)
	require.Len(t, work.Data, 1)
	assert.Equal(t, zephyr.Slug, work.Data[0].Slug)
	assert.Equal(t, []domain.NeedsWorkReason{
		domain.NeedsWorkReasonStub, domain.NeedsWorkReasonUnassessed, domain.NeedsWorkReasonMissingLicensing,
	}, work.Data[0].Reasons)
}

// TestHTTP_RevisionWithNonASCIILinkURL checks that a revision whose links hold URLs with non-ASCII characters
// answers 200 through the production handler and still shows only the visible link's URL, dropping the hidden
// one's.
func TestHTTP_RevisionWithNonASCIILinkURL(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	c := catalogtest.New(t, db)
	const (
		visibleRaw = "https://example.org/café-songs"
		hiddenRaw  = "https://example.org/fjörd-songs"
	)
	links := func(urls ...string) []domain.DocPieceLink {
		out := []domain.DocPieceLink{}
		for _, u := range urls {
			out = append(out, domain.DocPieceLink{DocLink: domain.DocLink{URL: u,
				Roles: []domain.LinkRole{domain.LinkRoleLyrics}}})
		}
		return out
	}
	// catalogtest stores each link's normalized URL, which percent-encodes it, as the importer does.
	piece := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Songs of the Inn",
		Links: links(visibleRaw, hiddenRaw)}})
	c.SetLinkState(hiddenRaw, catalogtest.LinkState{IsHidden: true})
	later := c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece, EntityID: piece.ID,
		Snapshot: domain.PieceDocument{SchemaVersion: domain.DocumentSchemaVersion, Title: "Songs of the Inn",
			Kind: domain.PieceKindSong, Conservation: domain.DocConservation{Status: domain.ConservationStatusUnassessed},
			LicensingStatus: domain.LicensingStatusUnknown, Links: links(visibleRaw, hiddenRaw)},
		BaseRevisionID: piece.RevisionID})

	e := endToEnd{
		handler:  NewHTTPHandler(repositoryHTTPDependencies(mysql.New(db), nil, passingTurnstile{})),
		contract: openapitest.New(t),
	}
	revision := sendOK[controller.EntityRevision[controller.PieceSummary, controller.PieceSnapshot]](
		t, e, http.MethodGet, "/v1/revisions/"+strconv.FormatInt(later, 10), "", "", http.StatusOK)
	var urls []string
	for _, link := range revision.Snapshot.Links {
		urls = append(urls, link.URL)
	}
	assert.Equal(t, []string{visibleRaw}, urls)
}
