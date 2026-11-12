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

// linkMark is in everything stored about the hidden link.
const linkMark = "linkmark"

// TestHTTP_PrivateDataReachesNoPublicRead seeds a hidden link with credits, performances and citations, requests
// every public catalogue read that could show it, and checks that no response holds it.
func TestHTTP_PrivateDataReachesNoPublicRead(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	c := catalogtest.New(t, db)

	archive := c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: "Lantern Archive"}})
	moot := c.Event(catalogtest.Event{Doc: domain.EventDocument{Name: "Lantern Moot"}})
	hiddenURL := "https://example.org/" + linkMark

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
		Title:   "The Lantern Song",
		Credits: []domain.DocCredit{{PersonID: &rowan.ID, Role: domain.CreditRoleLyrics}},
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
					DescriptionVerbatim: "Performance together", PerformerIDs: []int64{rowan.ID}}},
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

	reads := []string{
		"/v1/pieces/" + piece.Slug, historyPath("pieces", piece.Slug), revision(piece.RevisionID),
		piecesPath, piecesPath + "?q=" + linkMark,
		"/v1/persons/" + rowan.Slug, historyPath("persons", rowan.Slug), revision(rowan.RevisionID),
		"/v1/events/" + moot.Slug, historyPath("events", moot.Slug),
		"/v1/collections/" + strconv.FormatInt(archive.ID, 10),
		needsWorkPath, needsWorkPath + "?reason=missing_licensing", needsWorkPath + "?reason=dead_links",
	}

	bodies := map[string]string{}
	for _, path := range reads {
		body := get(path)
		bodies[path] = body
		assert.NotContains(t, body, linkMark, "%s shows the hidden link", path)
	}
	// The only dead link of the piece and of Rowan is hidden.
	dead := bodies[needsWorkPath+"?reason=dead_links"]
	assert.NotContains(t, dead, piece.Slug)
	assert.NotContains(t, dead, rowan.Slug)

	// The reads do show what is public, so the checks above are not vacuous.
	assert.Contains(t, bodies["/v1/pieces/"+piece.Slug], "https://example.org/lantern")
	assert.Contains(t, bodies["/v1/events/"+moot.Slug], "performance together")
}

// historyPath is the path of the history of the entity of collection with address.
func historyPath(collection, address string) string {
	return "/v1/" + collection + "/" + address + "/history"
}
