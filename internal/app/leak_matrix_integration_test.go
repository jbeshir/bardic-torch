//go:build integration

package app

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) { catalogtest.Main(m) }

// leakMarker is in every string the leak fixture stores where no public read may show it, so one
// case-insensitive search of a raw response body finds any of them, including strings a row does not name.
const leakMarker = "leakmark"

// leakFixture is what the leak matrix's requests address: public entities that refer to everything a public
// read must hide, and the hidden entities themselves.
type leakFixture struct {
	piece, group, person catalogtest.Ref
	// noPage prefers not to have a page: their own page shows their identity, which is marked, and nothing else.
	noPage catalogtest.Ref
	// noPageAdmirer prefers not to have a page either; person lists show their identity as a stub.
	noPageAdmirer                                     catalogtest.Ref
	unapprovedPiece, unapprovedPerson, unapprovedEcho catalogtest.Ref
	// lanterns is a public topic with an unapproved child and related affiliation, tagging unapproved pieces;
	// westmarch is a public affiliation of the no-page person and an unapproved person.
	lanterns, westmarch                              catalogtest.Ref
	unapprovedTerm, underUnapproved, unapprovedChild catalogtest.Ref
	unapprovedAffiliation                            catalogtest.Ref
	// archive is a public collection citing links of every hidden kind, under an unapproved collection.
	archive, unapprovedCollection catalogtest.Ref
	// moot is a public event at which every hidden kind of performance was recorded.
	moot, unapprovedEvent catalogtest.Ref
}

// noPageName is the no-page person's name, which only their own page shows.
const noPageName = "Leakmark Nopage"

// noPageAdmirerName is the name of another no-page person, which only person lists and their own page show.
const noPageAdmirerName = "Leakmark Nopage Admirer"

const hiddenLinkURL = "https://example.org/leakmark-hidden-link"

// leakCatalogue holds, each marked:
//   - a hidden legal alias of a public person, and of the no-page person;
//   - a prefers_no_page person with notes, pronouns, licensing notes, a public alias, a role, an affiliation,
//     relations in both directions, a credit, a performance as performer and a link with a performance;
//   - a hidden link attached to a piece and a person, each with a performance;
//   - an unapproved piece, person, term (and an approved term under it), affiliation, collection and event, each
//     referred to by public entities, and unapproved entities referring to public ones: an unapproved child and
//     related affiliation of a public term, unapproved pieces tagged with it, an unapproved parent, child and
//     region of a public collection, and unapproved and no-page persons affiliated with a public affiliation;
//   - in a public collection, and at a public event, links and performances of the hidden link, the no-page
//     person and unapproved pieces and persons;
//   - system fields that are never exposed: legacy titles, a claim by a user and normalized URLs.
var leakCatalogue = catalogtest.NewShared(func(c *catalogtest.Catalog) leakFixture {
	unapprovedAffiliation := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetAffiliation,
		Name: "Leakmark Unapproved Affiliation"}, Unapproved: true})
	lanterns := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic, Name: "Lanterns",
		RelatedAffiliationID: &unapprovedAffiliation.ID}})
	unapprovedChild := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic,
		Name: "Leakmark Unapproved Child", ParentID: &lanterns.ID}, Unapproved: true})
	unapprovedTerm := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic,
		Name: "Leakmark Unapproved Term"}, Unapproved: true})
	underUnapproved := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic,
		Name: "Leakmark Under Unapproved", ParentID: &unapprovedTerm.ID}})
	westmarch := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetAffiliation, Name: "Westmarch"}})
	unapprovedCollection := c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{
		Name: "Leakmark Unapproved Collection"}, Unapproved: true})
	// A public collection under an unapproved one, in an unapproved region, citing everything below.
	archive := c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: "Lantern Archive",
		ParentID: &unapprovedCollection.ID, RegionTermID: &unapprovedAffiliation.ID}})
	c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: "Lantern Annex", ParentID: &archive.ID}})
	c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: "Leakmark Unapproved Annex",
		ParentID: &archive.ID}, Unapproved: true})
	unapprovedEvent := c.Event(catalogtest.Event{Doc: domain.EventDocument{Name: "Leakmark Unapproved Event"},
		Unapproved: true})
	// A public event every performance below is at.
	moot := c.Event(catalogtest.Event{Doc: domain.EventDocument{Name: "Lantern Moot"}})

	group := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Lantern Band", Kind: domain.PersonKindGroup}})
	unapprovedPerson := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Leakmark Unapproved Person",
		AffiliationIDs: []int64{westmarch.ID},
		Relations:      []domain.DocPersonRelation{{ToPersonID: group.ID, Type: domain.PersonRelationTypeMemberOf}},
		Links: []domain.DocPersonLink{{
			DocLink: domain.DocLink{URL: "https://example.org/leakmark-unapproved-person-link",
				CollectionID: &archive.ID, Roles: []domain.LinkRole{domain.LinkRoleWebsite}},
			Performances: []domain.DocPersonPerformance{{DocPerformance: domain.DocPerformance{EventID: &moot.ID,
				DescriptionVerbatim: "Leakmark unapproved person performance"}}},
		}}},
		Unapproved: true})
	unapprovedPiece := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Leakmark Unapproved Piece",
		Terms: []domain.DocPieceTerm{{TermID: lanterns.ID}},
		Links: []domain.DocPieceLink{{
			DocLink: domain.DocLink{URL: "https://example.org/leakmark-unapproved-piece-link",
				CollectionID: &archive.ID, Roles: []domain.LinkRole{domain.LinkRoleLyrics}},
			Performances: []domain.DocPerformance{{EventID: &moot.ID,
				DescriptionVerbatim: "Leakmark unapproved piece moot performance"}},
		}}},
		Unapproved: true})
	noPage := c.Person(catalogtest.Person{Doc: domain.PersonDocument{
		Name:             noPageName,
		Status:           domain.PersonStatusPrefersNoPage,
		StatusSourceURL:  new("https://example.org/requests/9"),
		Pronouns:         new("leakmark/pronoun"),
		NotesMD:          new("Leakmark nopage notes"),
		LicensingStatus:  domain.LicensingStatusKnown,
		LicensingNotesMD: new("Leakmark nopage licensing notes"),
		Aliases: []domain.DocPersonAlias{
			{Name: "Leakmark Nopage Public Alias", Kind: domain.PersonAliasKindStage, IsPublic: true},
			{Name: "Leakmark Nopage Legal Alias", Kind: domain.PersonAliasKindLegal},
		},
		Roles:          []domain.PersonRole{domain.PersonRoleJester},
		AffiliationIDs: []int64{westmarch.ID},
		Relations:      []domain.DocPersonRelation{{ToPersonID: group.ID, Type: domain.PersonRelationTypeMemberOf}},
		Links: []domain.DocPersonLink{{
			DocLink: domain.DocLink{URL: "https://example.org/leakmark-nopage-link", Title: new("Leakmark Nopage Link"),
				CollectionID: &archive.ID, Roles: []domain.LinkRole{domain.LinkRoleWebsite}},
			Performances: []domain.DocPersonPerformance{{DocPerformance: domain.DocPerformance{EventID: &moot.ID,
				DescriptionVerbatim: "Leakmark nopage performance"}}},
		}},
	}})
	person := c.Person(catalogtest.Person{Doc: domain.PersonDocument{
		Name: "Rowan Ashby",
		Aliases: []domain.DocPersonAlias{
			{Name: "Rowan", Kind: domain.PersonAliasKindStage, IsPublic: true},
			{Name: "Leakmark Hidden Legal Alias", Kind: domain.PersonAliasKindLegal},
		},
		Roles:          []domain.PersonRole{domain.PersonRoleSinger},
		AffiliationIDs: []int64{westmarch.ID, unapprovedAffiliation.ID},
		Relations: []domain.DocPersonRelation{
			{ToPersonID: group.ID, Type: domain.PersonRelationTypeMemberOf},
			{ToPersonID: noPage.ID, Type: domain.PersonRelationTypeHeirOf},
			{ToPersonID: unapprovedPerson.ID, Type: domain.PersonRelationTypeSameAs},
		},
		Links: []domain.DocPersonLink{
			{
				DocLink: domain.DocLink{URL: hiddenLinkURL, Title: new("Leakmark Hidden Link"),
					CollectionID: &archive.ID, Roles: []domain.LinkRole{domain.LinkRoleOther}},
				Performances: []domain.DocPersonPerformance{{DocPerformance: domain.DocPerformance{EventID: &moot.ID,
					DescriptionVerbatim: "Leakmark hidden link performance"}}},
			},
			{
				DocLink: domain.DocLink{URL: "https://example.org/rowan", CollectionID: &unapprovedCollection.ID,
					Roles: []domain.LinkRole{domain.LinkRoleWebsite}},
				Performances: []domain.DocPersonPerformance{
					{PieceID: &unapprovedPiece.ID, DocPerformance: domain.DocPerformance{EventID: &moot.ID,
						DescriptionVerbatim: "Leakmark unapproved piece performance"}},
					{DocPerformance: domain.DocPerformance{EventID: &unapprovedEvent.ID,
						DescriptionVerbatim: "Rowan at a moot"}},
				},
			},
		},
	}})
	piece := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{
		Title: "The Lantern Song",
		Terms: []domain.DocPieceTerm{
			{TermID: lanterns.ID, IsPrimary: true}, {TermID: unapprovedTerm.ID}, {TermID: underUnapproved.ID},
		},
		Relations: []domain.DocPieceRelation{{ToPieceID: unapprovedPiece.ID, Type: domain.PieceRelationTypeVariantOf}},
		Credits: []domain.DocCredit{
			{PersonID: &person.ID, Role: domain.CreditRoleLyrics},
			{PersonID: &noPage.ID, Role: domain.CreditRoleTune, Note: new("Leakmark nopage credit note")},
			{PersonID: &unapprovedPerson.ID, Role: domain.CreditRoleMusic, Note: new("Leakmark unapproved credit note")},
		},
		Links: []domain.DocPieceLink{
			{
				DocLink: domain.DocLink{URL: "https://example.org/lantern", CollectionID: &unapprovedCollection.ID,
					Roles: []domain.LinkRole{domain.LinkRolePerformance}},
				Performances: []domain.DocPerformance{
					{EventID: &unapprovedEvent.ID, DescriptionVerbatim: "Sung by Rowan",
						PerformerIDs: []int64{person.ID, noPage.ID, unapprovedPerson.ID}},
					{EventID: &moot.ID, DescriptionVerbatim: "Sung at the moot",
						PerformerIDs: []int64{person.ID, noPage.ID, unapprovedPerson.ID}},
				},
			},
			{
				DocLink: domain.DocLink{URL: hiddenLinkURL, Roles: []domain.LinkRole{domain.LinkRoleLyrics}},
				Performances: []domain.DocPerformance{{EventID: &moot.ID,
					DescriptionVerbatim: "Leakmark hidden piece performance", PerformerIDs: []int64{person.ID}}},
			},
			{DocLink: domain.DocLink{URL: "https://example.org/lantern-archive", CollectionID: &archive.ID,
				Locator: new("shelf 3"), Roles: []domain.LinkRole{domain.LinkRoleText}}},
		},
	}})
	c.SetLinkState(hiddenLinkURL, catalogtest.LinkState{IsHidden: true})

	// Unapproved and no-page entities referring to the public ones, seeded after them.
	unapprovedEcho := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Leakmark Unapproved Echo",
		Terms:     []domain.DocPieceTerm{{TermID: lanterns.ID}},
		Relations: []domain.DocPieceRelation{{ToPieceID: piece.ID, Type: domain.PieceRelationTypeContrafactumOf}},
		Credits:   []domain.DocCredit{{PersonID: &person.ID, Role: domain.CreditRoleLyrics}}}, Unapproved: true})
	c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Leakmark Unapproved Admirer",
		Relations: []domain.DocPersonRelation{{ToPersonID: person.ID, Type: domain.PersonRelationTypeHeirOf}}},
		Unapproved: true})
	noPageAdmirer := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: noPageAdmirerName,
		Status:    domain.PersonStatusPrefersNoPage,
		Relations: []domain.DocPersonRelation{{ToPersonID: person.ID, Type: domain.PersonRelationTypeHeirOf}}}})

	// System fields no document holds and no read exposes.
	c.Exec("INSERT INTO users (id, display_name, role, created_at) VALUES (?, ?, 'contributor', ?)",
		"google-oauth2|leakmark-claimer", "Leakmark Claimer", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	c.Exec("UPDATE persons SET claimed_by_user_id = ?, legacy_title = ?, legacy_wiki_pageid = id",
		"google-oauth2|leakmark-claimer", "Leakmark Legacy Person")
	c.Exec("UPDATE pieces SET legacy_title = ?, legacy_wiki_pageid = id", "Leakmark Legacy Piece")
	c.Exec("UPDATE terms SET legacy_title = ?", "Leakmark Legacy Term")
	c.Exec("UPDATE events SET legacy_title = ?", "Leakmark Legacy Event")
	c.Exec("UPDATE links SET normalized_url = CONCAT('leakmark-normalized-', id)")

	return leakFixture{
		piece: piece, group: group, person: person, noPage: noPage, noPageAdmirer: noPageAdmirer,
		unapprovedPiece: unapprovedPiece, unapprovedPerson: unapprovedPerson, unapprovedEcho: unapprovedEcho,
		lanterns: lanterns, westmarch: westmarch, unapprovedTerm: unapprovedTerm, underUnapproved: underUnapproved,
		unapprovedChild: unapprovedChild, unapprovedAffiliation: unapprovedAffiliation,
		archive: archive, unapprovedCollection: unapprovedCollection, moot: moot, unapprovedEvent: unapprovedEvent,
	}
})

// leakRequest is a request the leak matrix sends, and its expected status.
type leakRequest struct {
	path   string
	status int
	// reveals are marked strings the response may show, such as the no-page person's identity on their own page.
	reveals []string
}

// leakMatrix has, for each public catalogue read in the spec, the requests to send it against the leak fixture:
// at least the public entities that refer to hidden ones, and the hidden entities themselves. TestHTTP_LeakMatrix
// fails for a public /v1 GET without a row, so each new catalogue endpoint adds one here, with requests that
// reach every list it shows.
var leakMatrix = map[openapitest.Operation]func(fx leakFixture) []leakRequest{
	{Method: http.MethodGet, Path: "/v1/pieces/{slug}"}: func(fx leakFixture) []leakRequest {
		return []leakRequest{
			{path: "/v1/pieces/" + fx.piece.Slug, status: http.StatusOK},
			{path: "/v1/pieces/" + fx.unapprovedPiece.Slug, status: http.StatusNotFound},
			{path: "/v1/pieces/" + fx.unapprovedEcho.Slug, status: http.StatusNotFound},
		}
	},
	{Method: http.MethodGet, Path: "/v1/persons/{slug}"}: func(fx leakFixture) []leakRequest {
		return []leakRequest{
			{path: "/v1/persons/" + fx.person.Slug, status: http.StatusOK},
			{path: "/v1/persons/" + fx.group.Slug, status: http.StatusOK},
			{path: "/v1/persons/" + fx.noPage.Slug, status: http.StatusOK, reveals: []string{fx.noPage.Slug, noPageName}},
			{path: "/v1/persons/" + fx.unapprovedPerson.Slug, status: http.StatusNotFound},
		}
	},
	{Method: http.MethodGet, Path: piecesPath}: func(fx leakFixture) []leakRequest {
		// An unknown term's 400 names the slug the request gave.
		unknownTerm := func(term catalogtest.Ref) leakRequest {
			return leakRequest{path: piecesPath + "?term=" + term.Slug, status: http.StatusBadRequest,
				reveals: []string{term.Slug}}
		}
		return []leakRequest{
			// Facet counts must not list unapproved terms, or terms under them, nor count unapproved pieces.
			{path: piecesPath, status: http.StatusOK},
			{path: piecesPath + "?term=" + fx.lanterns.Slug, status: http.StatusOK},
			{path: piecesPath + "?q=leakmark", status: http.StatusOK},
			{path: piecesPath + "?q=Le", status: http.StatusOK},
			unknownTerm(fx.unapprovedTerm),
			unknownTerm(fx.underUnapproved),
			unknownTerm(fx.unapprovedChild),
			unknownTerm(fx.westmarch),
		}
	},
	{Method: http.MethodGet, Path: personsPath}: func(fx leakFixture) []leakRequest {
		// Persons who prefer no page are listed as stubs, by name.
		stubs := []string{fx.noPage.Slug, noPageName, fx.noPageAdmirer.Slug, noPageAdmirerName}
		return []leakRequest{
			{path: personsPath, status: http.StatusOK, reveals: stubs},
			{path: personsPath + "?q=leakmark", status: http.StatusOK, reveals: stubs},
			{path: personsPath + "?q=Le", status: http.StatusOK, reveals: stubs},
			// The no-page person's role and affiliation, and the unapproved person's, must not match.
			{path: personsPath + "?role=jester", status: http.StatusOK},
			{path: personsPath + "?affiliation=" + fx.westmarch.Slug, status: http.StatusOK},
			{path: personsPath + "?affiliation=" + fx.unapprovedAffiliation.Slug, status: http.StatusBadRequest,
				reveals: []string{fx.unapprovedAffiliation.Slug}},
		}
	},
	{Method: http.MethodGet, Path: collectionsPath}: func(leakFixture) []leakRequest {
		return []leakRequest{
			{path: collectionsPath, status: http.StatusOK},
			{path: collectionsPath + "?kind=other&letter=L", status: http.StatusOK},
		}
	},
	{Method: http.MethodGet, Path: termsPath}: func(leakFixture) []leakRequest {
		return []leakRequest{
			{path: termsPath, status: http.StatusOK},
			{path: "/v1/terms?facet=topic", status: http.StatusOK},
			{path: "/v1/terms?facet=affiliation", status: http.StatusOK},
		}
	},
	{Method: http.MethodGet, Path: "/v1/terms/{slug}"}: func(fx leakFixture) []leakRequest {
		return []leakRequest{
			// Pieces, without the unapproved ones tagged with it; children and related affiliation, unapproved.
			{path: "/v1/terms/" + fx.lanterns.Slug, status: http.StatusOK},
			// Persons, without the no-page and unapproved ones affiliated with it.
			{path: "/v1/terms/" + fx.westmarch.Slug, status: http.StatusOK},
			{path: "/v1/terms/" + fx.unapprovedTerm.Slug, status: http.StatusNotFound},
			{path: "/v1/terms/" + fx.underUnapproved.Slug, status: http.StatusNotFound},
			{path: "/v1/terms/" + fx.unapprovedChild.Slug, status: http.StatusNotFound},
			{path: "/v1/terms/" + fx.unapprovedAffiliation.Slug, status: http.StatusNotFound},
		}
	},
	{Method: http.MethodGet, Path: "/v1/collections/{collection_id}"}: func(fx leakFixture) []leakRequest {
		return []leakRequest{
			// Citations of a public piece only; the parent, region and a child are unapproved.
			{path: "/v1/collections/" + strconv.FormatInt(fx.archive.ID, 10), status: http.StatusOK},
			{path: "/v1/collections/" + strconv.FormatInt(fx.unapprovedCollection.ID, 10), status: http.StatusNotFound},
		}
	},
	{Method: http.MethodGet, Path: eventsPath}: func(leakFixture) []leakRequest {
		return []leakRequest{
			{path: eventsPath, status: http.StatusOK},
			{path: "/v1/events?kind=event", status: http.StatusOK},
		}
	},
	{Method: http.MethodGet, Path: "/v1/events/{slug}"}: func(fx leakFixture) []leakRequest {
		return []leakRequest{
			// Performances of public pieces on visible links, with public performers only.
			{path: "/v1/events/" + fx.moot.Slug, status: http.StatusOK},
			{path: "/v1/events/" + fx.unapprovedEvent.Slug, status: http.StatusNotFound},
		}
	},
}

// Reasons an operation is in leakExempt.
const (
	exemptAccount = "the caller's own account"
	exemptAPIAuth = "the caller's API tokens"
)

// leakExempt are the operations that read no catalogue data, and why. A public /v1 GET cannot be exempt.
var leakExempt = map[openapitest.Operation]string{
	{Method: http.MethodGet, Path: healthzPath}:                   "health check",
	{Method: http.MethodGet, Path: readyzPath}:                    "health check",
	{Method: http.MethodGet, Path: mePath}:                        exemptAccount,
	{Method: http.MethodPatch, Path: mePath}:                      exemptAccount,
	{Method: http.MethodPut, Path: "/v1/users/{user_id}/blocked"}: "moderation of accounts",
	{Method: http.MethodPost, Path: tokensPath}:                   exemptAPIAuth,
	{Method: http.MethodGet, Path: tokensPath}:                    exemptAPIAuth,
	{Method: http.MethodDelete, Path: "/v1/tokens/{token_id}"}:    exemptAPIAuth,
}

// TestHTTP_LeakMatrix sends every leakMatrix request to the production handler over a real repository holding
// the leak fixture, and checks that no raw response body contains a marked string it does not reveal, and that
// every response honours the contract. Every operation in the spec needs a row or an exemption.
func TestHTTP_LeakMatrix(t *testing.T) {
	t.Parallel()

	db, fx := leakCatalogue.Get(t)
	requireLeakMarkersStored(t, db)
	handler := NewHTTPHandler(repositoryHTTPDependencies(mysql.New(db), nil))
	contract := openapitest.New(t)

	operations := contract.Operations()
	for op := range leakMatrix {
		assert.Contains(t, operations, op, "leakMatrix has a row for an operation the spec does not have")
		assert.True(t, op.Method == http.MethodGet && contract.Public(op), "%s %s is not a public read", op.Method,
			op.Path)
	}
	for op := range leakExempt {
		assert.Contains(t, operations, op, "leakExempt lists an operation the spec does not have")
	}

	for _, op := range operations {
		requests, hasRow := leakMatrix[op]
		_, exempt := leakExempt[op]
		switch {
		case hasRow && exempt:
			t.Errorf("%s %s has a row in leakMatrix and is in leakExempt", op.Method, op.Path)
			continue
		case exempt:
			assert.False(t, op.Method == http.MethodGet && contract.Public(op) && strings.HasPrefix(op.Path, "/v1/"),
				"%s %s is a public catalogue read, so it needs a row in leakMatrix rather than an exemption",
				op.Method, op.Path)
			continue
		case !hasRow:
			t.Errorf("%s %s has no row in leakMatrix and is not in leakExempt", op.Method, op.Path)
			continue
		}

		t.Run(op.Method+" "+op.Path, func(t *testing.T) {
			t.Parallel()

			for _, req := range requests(fx) {
				t.Run(req.path, func(t *testing.T) {
					t.Parallel()
					assertNoLeak(t, contract, handler, req)
				})
			}
		})
	}
}

func assertNoLeak(t *testing.T, contract *openapitest.Validator, handler http.Handler, req leakRequest) {
	t.Helper()

	rec := contract.Serve(t, handler, httptest.NewRequestWithContext(t.Context(), http.MethodGet, req.path, nil))
	require.Equal(t, req.status, rec.Code, rec.Body.String())

	body := strings.ToLower(rec.Body.String())
	for _, revealed := range req.reveals {
		body = strings.ReplaceAll(body, strings.ToLower(revealed), "")
	}
	assert.NotContains(t, body, leakMarker, "the response shows a marked string:\n%s", rec.Body.String())
}

// requireLeakMarkersStored checks that the fixture really stores a marker of each kind, so that the matrix
// cannot pass because a marker went missing from the fixture.
func requireLeakMarkersStored(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, query := range []string{
		"SELECT COUNT(*) FROM person_aliases WHERE NOT is_public AND kind = 'legal' AND name LIKE '%Leakmark%'",
		"SELECT COUNT(*) FROM persons WHERE status = 'prefers_no_page' AND notes_md LIKE '%Leakmark%'",
		"SELECT COUNT(*) FROM links WHERE is_hidden AND url LIKE '%leakmark%'",
		"SELECT COUNT(*) FROM pieces WHERE current_revision_id IS NULL AND title LIKE '%Leakmark%'",
		"SELECT COUNT(*) FROM persons WHERE current_revision_id IS NULL AND name LIKE '%Leakmark%'",
		"SELECT COUNT(*) FROM terms WHERE current_revision_id IS NULL AND name LIKE '%Leakmark%'",
		"SELECT COUNT(*) FROM persons WHERE claimed_by_user_id LIKE '%leakmark%'",
		"SELECT COUNT(*) FROM links WHERE normalized_url LIKE 'leakmark%'",
		"SELECT COUNT(*) FROM collections WHERE current_revision_id IS NULL AND name LIKE '%Leakmark%'",
		"SELECT COUNT(*) FROM events WHERE current_revision_id IS NULL AND name LIKE '%Leakmark%'",
		// Marked performances at a public event, and marked links in a public collection.
		"SELECT COUNT(*) FROM performances perf JOIN events e ON e.id = perf.event_id " +
			"WHERE e.current_revision_id IS NOT NULL AND perf.description_verbatim LIKE '%Leakmark%'",
		"SELECT COUNT(*) FROM links l JOIN collections c ON c.id = l.collection_id " +
			"WHERE c.current_revision_id IS NOT NULL AND l.url LIKE '%leakmark%'",
	} {
		var count int
		require.NoError(t, db.QueryRowContext(t.Context(), query).Scan(&count), query)
		require.Positive(t, count, query)
	}
}
