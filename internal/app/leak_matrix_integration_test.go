//go:build integration

package app

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// noPageMark is in everything stored about the person who prefers no page, except their name, which person
	// lists and their own page show.
	noPageMark = "nopagemark"
	// linkMark is in everything stored about the hidden link.
	linkMark   = "linkmark"
	noPageName = "Quiet Nopage"
)

// TestHTTP_PrivateDataReachesNoPublicRead seeds a person who prefers no page and a hidden link with credits,
// performances and citations, requests every public catalogue read that could show them, and checks that no
// response holds what they hide.
func TestHTTP_PrivateDataReachesNoPublicRead(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	c := catalogtest.New(t, db)

	archive := c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: "Lantern Archive"}})
	moot := c.Event(catalogtest.Event{Doc: domain.EventDocument{Name: "Lantern Moot"}})
	hiddenURL := "https://example.org/" + linkMark

	noPage := c.Person(catalogtest.Person{Doc: domain.PersonDocument{
		Name:             noPageName,
		Status:           domain.PersonStatusPrefersNoPage,
		Pronouns:         new(noPageMark + "/pronoun"),
		NotesMD:          new(noPageMark + " notes"),
		LicensingNotesMD: new(noPageMark + " licensing notes"),
		Aliases:          []domain.DocPersonAlias{{Name: "Alias " + noPageMark, Kind: domain.PersonAliasKindStage}},
		Links: []domain.DocPersonLink{{
			DocLink: domain.DocLink{URL: "https://example.org/" + noPageMark, Title: new("Link " + noPageMark),
				CollectionID: &archive.ID, Roles: []domain.LinkRole{domain.LinkRoleWebsite}},
			Performances: []domain.DocPersonPerformance{{DocPerformance: domain.DocPerformance{EventID: &moot.ID,
				DescriptionVerbatim: "Performance " + noPageMark}}},
		}},
	}})
	rowan := c.Person(catalogtest.Person{Doc: domain.PersonDocument{
		Name: "Rowan Ashby",
		Links: []domain.DocPersonLink{{
			DocLink: domain.DocLink{URL: hiddenURL, Title: new("Link " + linkMark),
				CollectionID: &archive.ID, Roles: []domain.LinkRole{domain.LinkRoleOther}},
			Performances: []domain.DocPersonPerformance{{DocPerformance: domain.DocPerformance{EventID: &moot.ID,
				DescriptionVerbatim: "Performance " + linkMark}}},
		}},
	}})
	piece := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{
		Title: "The Lantern Song",
		Credits: []domain.DocCredit{
			{PersonID: &rowan.ID, Role: domain.CreditRoleLyrics},
			{PersonID: &noPage.ID, Role: domain.CreditRoleTune, Note: new("Credit " + noPageMark)},
		},
		Links: []domain.DocPieceLink{
			{
				DocLink: domain.DocLink{URL: hiddenURL, Title: new("Link " + linkMark),
					CollectionID: &archive.ID, Roles: []domain.LinkRole{domain.LinkRoleLyrics}},
				Performances: []domain.DocPerformance{{EventID: &moot.ID,
					DescriptionVerbatim: "Performance " + linkMark, PerformerIDs: []int64{rowan.ID}}},
			},
			{
				DocLink: domain.DocLink{URL: "https://example.org/lantern", CollectionID: &archive.ID,
					Roles: []domain.LinkRole{domain.LinkRolePerformance}},
				Performances: []domain.DocPerformance{{EventID: &moot.ID,
					DescriptionVerbatim: "Performance together", PerformerIDs: []int64{rowan.ID, noPage.ID}}},
			},
		},
	}})
	// Hidden after the documents above were written, and dead, so that needs-work would list the piece and
	// Rowan if it counted hidden links.
	c.SetLinkState(hiddenURL, catalogtest.LinkState{Liveness: domain.LivenessDead, IsHidden: true})

	handler := NewHTTPHandler(repositoryHTTPDependencies(mysql.New(db), nil))
	contract := openapitest.New(t)
	get := func(path string) string {
		rec := contract.Serve(t, handler, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, rec.Code, path+": "+rec.Body.String())
		return strings.ToLower(rec.Body.String())
	}
	revision := func(id int64) string { return "/v1/revisions/" + strconv.FormatInt(id, 10) }

	// Reads that may show the no-page person's identity: their own page and the lists of persons.
	own := []string{
		"/v1/persons/" + noPage.Slug, historyPath("persons", noPage.Slug), revision(noPage.RevisionID),
		personsPath, personsPath + "?q=" + noPageMark,
	}
	// Reads where neither the person's name nor their slug may appear: they are credited, perform or are cited
	// in these.
	elsewhere := []string{
		"/v1/pieces/" + piece.Slug, historyPath("pieces", piece.Slug), revision(piece.RevisionID),
		piecesPath, piecesPath + "?q=" + noPageMark, piecesPath + "?q=" + linkMark,
		"/v1/persons/" + rowan.Slug, historyPath("persons", rowan.Slug), revision(rowan.RevisionID),
		"/v1/events/" + moot.Slug, historyPath("events", moot.Slug),
		"/v1/collections/" + strconv.FormatInt(archive.ID, 10),
		needsWorkPath, needsWorkPath + "?reason=missing_licensing", needsWorkPath + "?reason=dead_links",
	}

	bodies := map[string]string{}
	for _, path := range append(own, elsewhere...) {
		body := get(path)
		bodies[path] = body
		assert.NotContains(t, body, noPageMark, "%s shows what the person who prefers no page hides", path)
		assert.NotContains(t, body, linkMark, "%s shows the hidden link", path)
	}
	for _, path := range elsewhere {
		body := bodies[path]
		assert.NotContains(t, body, strings.ToLower(noPageName), "%s names the person who prefers no page", path)
		assert.NotContains(t, body, noPage.Slug, "%s addresses the person who prefers no page", path)
	}
	// The only dead link of the piece and of Rowan is hidden.
	dead := bodies[needsWorkPath+"?reason=dead_links"]
	assert.NotContains(t, dead, piece.Slug)
	assert.NotContains(t, dead, rowan.Slug)

	// The reads do show what is public, so the checks above are not vacuous.
	assert.Contains(t, bodies["/v1/pieces/"+piece.Slug], "https://example.org/lantern")
	assert.Contains(t, bodies["/v1/pieces/"+piece.Slug], "rowan")
	assert.Contains(t, bodies[personsPath], noPage.Slug)
}

// historyPath is the path of the history of the entity of collection with address.
func historyPath(collection, address string) string {
	return "/v1/" + collection + "/" + address + "/history"
}
