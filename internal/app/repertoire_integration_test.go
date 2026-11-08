//go:build integration

package app

import (
	"database/sql"
	"net/http"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/transport/web/controller"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// repertoireNotes are the private notes of every repertoire entry the leak matrices write, which only their owner's
// own reads may show.
const repertoireNotes = "leakmark-repertoire-note"

const myRepertoirePath = "/v1/me/repertoire"

// repertoireEntryPath is the path of the caller's repertoire entry for the piece with slug.
func repertoireEntryPath(slug string) string {
	return myRepertoirePath + "/" + slug
}

// publicRepertoirePath is the path of the public repertoire of the user with publicID.
func publicRepertoirePath(publicID string) string {
	return "/v1/users/" + publicID + "/repertoire"
}

// unknownPublicRepertoirePath is the public repertoire of a public ID no user has.
var unknownPublicRepertoirePath = publicRepertoirePath("aaaaaaaaaaaaaaaa")

// repertoireEntryBody is the body of a PUT of a repertoire entry with repertoireNotes.
func repertoireEntryBody(visibility domain.RepertoireVisibility) string {
	return `{"notes":"` + repertoireNotes + `","visibility":"` + string(visibility) + `"}`
}

// The users whose repertoires the leak fixture holds. Their IDs are marked: no public response may show them.
const (
	repertoireShownID   = "google-oauth2|leakmark-repertoire-shown"
	repertoirePrivateID = "google-oauth2|leakmark-repertoire-private"
	repertoireBlockedID = "google-oauth2|leakmark-repertoire-blocked"
)

// leakRepertoires are the public IDs of the leak fixture's repertoire owners: shown has a public entry of a public
// piece, and a private one, both with notes, and a public entry of an unapproved piece; allPrivate has only a
// private entry; blocked had a public entry when they were blocked.
type leakRepertoires struct {
	shown, allPrivate, blocked string
	// privateSlug is the slug of shown's private entry's piece.
	privateSlug string
}

// seedLeakRepertoires has the repertoire owners write their entries through the production handler: shown's of
// public and hidden, allPrivate's and blocked's of public, each with repertoireNotes; it then adds shown an entry of
// unapproved, which no write path allows, and blocks blocked.
func seedLeakRepertoires(c *catalogtest.Catalog, public, hidden, unapproved catalogtest.Ref) leakRepertoires {
	t, ok := c.T().(*testing.T)
	require.True(t, ok, "the leak fixture is seeded by a test")
	w := revisionWorldOn(t, c.DB(), nil)
	put := func(userID, slug string, visibility domain.RepertoireVisibility) string {
		authorization := w.auth0(t, userID)
		sendOK[jsonObject](t, w.e, http.MethodPut, repertoireEntryPath(slug), authorization,
			repertoireEntryBody(visibility), http.StatusCreated)
		return sendOK[controller.User](t, w.e, http.MethodGet, mePath, authorization, "", http.StatusOK).PublicID
	}

	r := leakRepertoires{
		shown:       put(repertoireShownID, public.Slug, domain.RepertoireVisibilityPublic),
		allPrivate:  put(repertoirePrivateID, public.Slug, domain.RepertoireVisibilityPrivate),
		blocked:     put(repertoireBlockedID, public.Slug, domain.RepertoireVisibilityPublic),
		privateSlug: hidden.Slug,
	}
	put(repertoireShownID, hidden.Slug, domain.RepertoireVisibilityPrivate)
	sendRejected(t, w.e, http.MethodPut, repertoireEntryPath(unapproved.Slug), w.auth0(t, repertoireShownID),
		repertoireEntryBody(domain.RepertoireVisibilityPublic), http.StatusNotFound, httpjson.CodeNotFound)
	c.Exec("INSERT INTO repertoire (user_id, piece_id, notes, visibility, added_at, updated_at) "+
		"VALUES (?, ?, ?, 'public', NOW(), NOW())", repertoireShownID, unapproved.ID, repertoireNotes)
	c.Exec("UPDATE users SET blocked_at = NOW() WHERE id = ?", repertoireBlockedID)

	return r
}

// requireLeakRepertoiresStored checks that the leak fixture's repertoires are stored as seeded, so that the matrix
// cannot pass because an entry or its notes went missing.
func requireLeakRepertoiresStored(t *testing.T, db *sql.DB, fx leakFixture) {
	t.Helper()
	for _, check := range []struct {
		userID     string
		visibility domain.RepertoireVisibility
		want       int
	}{
		{repertoireShownID, domain.RepertoireVisibilityPublic, 2},
		{repertoireShownID, domain.RepertoireVisibilityPrivate, 1},
		{repertoirePrivateID, domain.RepertoireVisibilityPrivate, 1},
		{repertoireBlockedID, domain.RepertoireVisibilityPublic, 1},
	} {
		var count int
		require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM repertoire WHERE user_id = ? "+
			"AND visibility = ? AND notes = ?", check.userID, check.visibility, repertoireNotes).Scan(&count))
		require.Equal(t, check.want, count, "%s %s", check.userID, check.visibility)
	}
	var blocked int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM users WHERE id = ? AND public_id = ? "+
		"AND blocked_at IS NOT NULL", repertoireBlockedID, fx.repertoires.blocked).Scan(&blocked))
	require.Equal(t, 1, blocked)
}

// publicRepertoireRows are the leak matrix's requests of the public repertoires: shown's lists only its public entry
// of a public piece, without notes, owner ID or the private entry's piece; every page of it too; allPrivate's,
// blocked's and a public ID no user has are one 404, byte for byte. A user ID in place of a public ID breaks the
// spec, so its 400 is a contract test of the controller's.
func publicRepertoireRows(fx leakFixture) []leakRequest {
	shown := publicRepertoirePath(fx.repertoires.shown)
	absent := []string{fx.repertoires.privateSlug, fx.unapprovedPiece.Slug, repertoireShownID, "notes"}
	requests := []leakRequest{
		{path: unknownPublicRepertoirePath, status: http.StatusNotFound},
		{path: publicRepertoirePath(fx.repertoires.allPrivate), status: http.StatusNotFound,
			sameBodyAs: unknownPublicRepertoirePath},
		{path: publicRepertoirePath(fx.repertoires.blocked), status: http.StatusNotFound,
			sameBodyAs: unknownPublicRepertoirePath},
		{path: shown + "?offset=5", status: http.StatusOK, total: new(1)},
	}
	for _, page := range pagesOf(shown, 1) {
		page.absent, page.total = absent, new(1)
		requests = append(requests, page)
	}

	return requests
}

// TestHTTP_RepertoireEndToEnd walks one user's repertoire through the production handler: a piece added privately
// is in their own list with its notes and not in their public repertoire, which is not found; made public, it is
// listed to anyone without its notes; and made private again, the public repertoire is not found once more. Another
// user never sees the entry, and the owner's API token reads the same repertoire.
func TestHTTP_RepertoireEndToEnd(t *testing.T) {
	t.Parallel()

	w := newRevisionWorld(t, nil, "google-oauth2|repertoire-moderator")
	c := catalogtest.New(t, w.db)
	piece := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: catalogueKettleTitle}})
	owner, other := w.auth0(t, "google-oauth2|repertoire-owner"), w.auth0(t, "google-oauth2|repertoire-other")
	me := sendOK[controller.User](t, w.e, http.MethodGet, mePath, owner, "", http.StatusOK)
	require.True(t, domain.IsPublicID(me.PublicID), me.PublicID)
	token := w.apiToken(t, owner)
	public := publicRepertoirePath(me.PublicID)

	added := sendOK[controller.RepertoireEntry](t, w.e, http.MethodPut, repertoireEntryPath(piece.Slug), owner,
		`{"notes":"Capo 2.\r\nSlow.","visibility":"private"}`, http.StatusCreated)
	assert.Equal(t, new("Capo 2.\nSlow."), added.Notes, "notes are stored with LF line breaks")
	assert.Equal(t, domain.RepertoireVisibilityPrivate, added.Visibility)
	own := sendOK[controller.RepertoireList](t, w.e, http.MethodGet, myRepertoirePath, token, "", http.StatusOK)
	assert.Equal(t, controller.RepertoireList{Data: []controller.RepertoireEntry{added}, Total: 1}, own)
	sendRejected(t, w.e, http.MethodGet, public, "", "", http.StatusNotFound, httpjson.CodeNotFound)
	sendRejected(t, w.e, http.MethodGet, repertoireEntryPath(piece.Slug), other, "", http.StatusNotFound,
		httpjson.CodeNotFound)
	assert.Equal(t, controller.RepertoireList{Data: []controller.RepertoireEntry{}},
		sendOK[controller.RepertoireList](t, w.e, http.MethodGet, myRepertoirePath, other, "", http.StatusOK))

	shown := sendOK[controller.RepertoireEntry](t, w.e, http.MethodPut, repertoireEntryPath(piece.Slug), owner,
		`{"notes":"Capo 2.","visibility":"public"}`, http.StatusOK)
	assert.Equal(t, added.AddedAt, shown.AddedAt, "an update keeps when the piece was added")
	rec := w.e.send(t, http.MethodGet, public, "", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "Capo", "a public repertoire never shows notes")
	assert.NotContains(t, rec.Body.String(), "repertoire-owner", "a public repertoire never shows the user ID")
	listed := sendOK[controller.PublicRepertoire](t, w.e, http.MethodGet, public, other, "", http.StatusOK)
	assert.Equal(t, controller.PublicRepertoire{
		Owner: controller.RepertoireOwner{DisplayName: me.DisplayName, Role: domain.RoleContributor},
		Data:  []controller.PublicRepertoireEntry{{Piece: shown.Piece, AddedAt: shown.AddedAt}},
		Total: 1,
	}, listed)

	sendOK[controller.RepertoireEntry](t, w.e, http.MethodPut, repertoireEntryPath(piece.Slug), token,
		`{"visibility":"private"}`, http.StatusOK)
	sendRejected(t, w.e, http.MethodGet, public, "", "", http.StatusNotFound, httpjson.CodeNotFound)
	rec = w.e.send(t, http.MethodDelete, repertoireEntryPath(piece.Slug), owner, "")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	sendRejected(t, w.e, http.MethodDelete, repertoireEntryPath(piece.Slug), owner, "", http.StatusNotFound,
		httpjson.CodeNotFound)
}
