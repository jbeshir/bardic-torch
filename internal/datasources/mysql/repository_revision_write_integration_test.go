//go:build integration

package mysql_test

import (
	"database/sql"
	"slices"
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

const (
	wrenID       = "google-oauth2|wren"
	lanternTitle = "The Brass Lantern"
	heronID      = "google-oauth2|heron"
)

var submittedAt = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// writeFixture is a fresh catalogue with a contributor and a moderator.
type writeFixture struct {
	db   *sql.DB
	c    *catalogtest.Catalog
	repo *mysql.Repository
}

func newWriteFixture(t *testing.T) writeFixture {
	t.Helper()
	db := mysqltest.NewDatabase(t)
	c := catalogtest.New(t, db)
	c.Exec("INSERT INTO users (id, display_name, role, created_at) VALUES (?, 'Linnet', 'contributor', ?), "+
		"(?, 'Heron', 'moderator', ?)", wrenID, submittedAt, heronID, submittedAt)

	return writeFixture{db: db, c: c, repo: mysql.New(db)}
}

// submit submits doc against base (0 for a new entity), as the moderator if byModerator, failing the test on an
// error.
func (fx writeFixture) submit(t *testing.T, base int64, doc any, byModerator bool) int64 {
	t.Helper()
	id, err := fx.repo.SubmitRevision(t.Context(), fx.revision(base, doc, byModerator))
	require.NoError(t, err)

	return id
}

func (fx writeFixture) revision(base int64, doc any, byModerator bool) datasources.NewRevision {
	rev := datasources.NewRevision{AuthorUserID: wrenID, Summary: "An edit", Document: doc, At: submittedAt}
	if base != 0 {
		rev.BaseRevisionID = &base
	}
	if byModerator {
		rev.AuthorUserID, rev.ByModerator = heronID, true
	}

	return rev
}

// count returns the result of a COUNT query.
func (fx writeFixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, fx.db.QueryRowContext(t.Context(), query, args...).Scan(&n))

	return n
}

// pieceDoc is a valid piece document with title.
func pieceDoc(title string) domain.PieceDocument {
	return domain.PieceDocument{SchemaVersion: domain.DocumentSchemaVersion, Title: title, Kind: domain.PieceKindSong,
		Conservation:    domain.DocConservation{Status: domain.ConservationStatusUnassessed},
		LicensingStatus: domain.LicensingStatusUnknown}
}

func pieceLink(url string, roles ...domain.LinkRole) domain.DocPieceLink {
	return domain.DocPieceLink{DocLink: domain.DocLink{URL: url, Roles: roles}}
}

// Approving a moderator's new piece creates its row with a free slug, its sort keys and its current revision, in
// the same call, and the piece reads back as public.
func TestSubmit_ModeratorNewPieceIsApprovedWithFreeSlug(t *testing.T) {
	fx := newWriteFixture(t)
	fx.c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: lanternTitle}})
	ballads := fx.c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetForm, Name: "Ballads"}})
	doc := pieceDoc(lanternTitle)
	doc.Terms = []domain.DocPieceTerm{{TermID: ballads.ID, IsPrimary: true}}

	id := fx.submit(t, 0, doc, true)

	piece, err := fx.repo.GetPublicPiece(t.Context(), "the-brass-lantern-2")
	require.NoError(t, err)
	assert.Equal(t, lanternTitle, piece.Title)
	require.Len(t, piece.Terms, 1)
	assert.Equal(t, "ballads", piece.Terms[0].Term.Slug)
	var sortTitle, sortLetter string
	var current int64
	require.NoError(t, fx.db.QueryRowContext(t.Context(), "SELECT sort_title, sort_letter, current_revision_id "+
		"FROM pieces WHERE slug = 'the-brass-lantern-2'").Scan(&sortTitle, &sortLetter, &current))
	assert.Equal(t, []any{"Brass Lantern", "B", id}, []any{sortTitle, sortLetter, current})
	assert.Equal(t, 1, fx.count(t, "SELECT COUNT(*) FROM revisions r JOIN pieces p ON p.id = r.entity_id "+
		"WHERE r.id = ? AND r.status = 'approved' AND r.reviewer_user_id = ? AND p.slug = 'the-brass-lantern-2'",
		id, heronID))
}

// Approving an edit rewrites the piece's children and upserts its links by normalized URL: a shared link's editor
// fields change for every piece attaching it, and its liveness and the other piece's attachment stay.
func TestApprove_EditRewritesChildrenAndUpsertsLinks(t *testing.T) {
	fx := newWriteFixture(t)
	const shared = "https://example.org/lantern"
	old := pieceDoc(lanternTitle)
	old.Aliases = []domain.DocPieceAlias{{Text: "Old Lantern", Kind: domain.PieceAliasKindAltTitle}}
	old.Links = []domain.DocPieceLink{pieceLink(shared, domain.LinkRoleLyrics)}
	lantern := fx.c.Piece(catalogtest.Piece{Doc: old})
	other := fx.c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "The Tin Cup",
		Links: []domain.DocPieceLink{pieceLink(shared, domain.LinkRoleText)}}})
	fx.c.SetLinkState(shared, catalogtest.LinkState{Liveness: domain.LivenessDead})

	edit := pieceDoc(lanternTitle)
	edit.Aliases = []domain.DocPieceAlias{{Text: "New Lantern", Kind: domain.PieceAliasKindAltTitle}}
	sharedLink := pieceLink(shared, domain.LinkRoleLyrics, domain.LinkRoleText)
	sharedLink.Title = new("Lantern lyrics")
	edit.Links = []domain.DocPieceLink{sharedLink}
	id := fx.submit(t, lantern.RevisionID, edit, false)
	require.NoError(t, fx.repo.ApproveRevision(t.Context(), id, heronID, false, submittedAt))

	piece, err := fx.repo.GetPublicPiece(t.Context(), lantern.Slug)
	require.NoError(t, err)
	require.Len(t, piece.Aliases, 1)
	assert.Equal(t, "New Lantern", piece.Aliases[0].Text)
	require.Len(t, piece.Links, 1)
	assert.Equal(t, []domain.LinkRole{domain.LinkRoleLyrics, domain.LinkRoleText}, piece.Links[0].Roles)
	assert.Equal(t, "Lantern lyrics", *piece.Links[0].Title)
	assert.Equal(t, domain.LivenessDead, piece.Links[0].Liveness)
	assert.Equal(t, 1, fx.count(t, "SELECT COUNT(*) FROM links"), "links are shared by normalized URL")
	assert.Equal(t, 1, fx.count(t, "SELECT COUNT(*) FROM link_attachments WHERE piece_id = ?", other.ID))
}

// A conservation history row is appended when an approval changes the status, with the summary as the reason and
// the document's source, and not when it keeps it.
func TestApprove_ConservationHistoryOnStatusChange(t *testing.T) {
	fx := newWriteFixture(t)
	lantern := fx.c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: lanternTitle}})
	doc := pieceDoc(lanternTitle)
	doc.Conservation = domain.DocConservation{Status: domain.ConservationStatusEndangered,
		SourceURL: new("https://example.org/assessment")}
	first := fx.submit(t, lantern.RevisionID, doc, true)
	doc.NotesMD = new("Sung at every feast.")
	fx.submit(t, first, doc, true)

	piece, err := fx.repo.GetPublicPiece(t.Context(), lantern.Slug)
	require.NoError(t, err)
	require.Len(t, piece.ConservationHistory, 1)
	change := piece.ConservationHistory[0]
	assert.Equal(t, domain.ConservationStatusEndangered, change.Status)
	assert.Equal(t, "An edit", change.Reason)
	assert.Equal(t, "https://example.org/assessment", *change.SourceURL)
}

// Approving a term rebuilds the whole closure: a term moved under another root takes its pieces with it.
func TestApprove_TermRebuildsClosure(t *testing.T) {
	fx := newWriteFixture(t)
	war := fx.c.Term(catalogtest.Term{Doc: domain.TermDocument{Name: "War"}})
	sea := fx.c.Term(catalogtest.Term{Doc: domain.TermDocument{Name: "Sea"}})
	battles := fx.c.Term(catalogtest.Term{Doc: domain.TermDocument{Name: "Battles", ParentID: &war.ID}})
	fx.c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "The Broadside",
		Terms: []domain.DocPieceTerm{{TermID: battles.ID, IsPrimary: true}}}})

	fx.submit(t, battles.RevisionID, domain.TermDocument{SchemaVersion: domain.DocumentSchemaVersion,
		Facet: domain.FacetTopic, Name: "Battles", DisplayName: "Sea battles", ParentID: &sea.ID}, true)

	for slug, pieces := range map[string]int64{war.Slug: 0, sea.Slug: 1} {
		term, err := fx.repo.GetPublicTerm(t.Context(), slug, domain.Page{Limit: 10})
		require.NoError(t, err)
		assert.Equal(t, pieces, term.PieceCount, slug)
	}
	assert.Equal(t, 4, fx.count(t, "SELECT COUNT(*) FROM term_ancestors"))
}

// A piece and a person each own the performances their documents record on a shared link: approving the piece's
// edit replaces its own and leaves the person's.
func TestApprove_PerformancesOwnedByTheirDocument(t *testing.T) {
	fx := newWriteFixture(t)
	const url = "https://example.org/feast-recording"
	lanternDoc := pieceDoc(lanternTitle)
	link := pieceLink(url, domain.LinkRolePerformance)
	link.Performances = []domain.DocPerformance{{DescriptionVerbatim: "At the feast"}}
	lanternDoc.Links = []domain.DocPieceLink{link}
	lantern := fx.c.Piece(catalogtest.Piece{Doc: lanternDoc})
	fx.c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Linnet of the Vale",
		Links: []domain.DocPersonLink{{DocLink: domain.DocLink{URL: url, Roles: []domain.LinkRole{
			domain.LinkRolePerformance}}, Performances: []domain.DocPersonPerformance{{PieceID: &lantern.ID,
			DocPerformance: domain.DocPerformance{DescriptionVerbatim: "Linnet sings it"}}}}}}})

	link.Performances = []domain.DocPerformance{{DescriptionVerbatim: "At the second feast"}}
	lanternDoc.Links = []domain.DocPieceLink{link}
	fx.submit(t, lantern.RevisionID, lanternDoc, true)

	assert.Equal(t, 1, fx.count(t, "SELECT COUNT(*) FROM performances WHERE owner_piece_id = ? AND "+
		"description_verbatim = 'At the second feast'", lantern.ID))
	assert.Equal(t, 1, fx.count(t, "SELECT COUNT(*) FROM performances WHERE owner_person_id IS NOT NULL AND "+
		"piece_id = ? AND description_verbatim = 'Linnet sings it'", lantern.ID))
	assert.Equal(t, 2, fx.count(t, "SELECT COUNT(*) FROM performances"))
}

// A revision whose base is no longer current is refused unless the moderator acknowledges it; the moderator view
// shows the current revision it is stale against.
func TestApprove_StaleNeedsAcknowledgement(t *testing.T) {
	fx := newWriteFixture(t)
	lantern := fx.c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: lanternTitle}})
	contributed := fx.submit(t, lantern.RevisionID, pieceDoc("The Brass Lantern (contributed)"), false)
	moderated := fx.submit(t, lantern.RevisionID, pieceDoc("The Brass Lantern (moderated)"), true)

	view, err := fx.repo.GetModerationRevision(t.Context(), contributed)
	require.NoError(t, err)
	pieceView := view.(domain.PieceRevisionView)
	require.NotNil(t, pieceView.Current)
	assert.Equal(t, moderated, pieceView.Current.ID)
	assert.Equal(t, wrenID, *pieceView.AuthorUserID)

	err = fx.repo.ApproveRevision(t.Context(), contributed, heronID, false, submittedAt)
	require.ErrorIs(t, err, datasources.ErrStale)
	require.NoError(t, fx.repo.ApproveRevision(t.Context(), contributed, heronID, true, submittedAt))
	piece, err := fx.repo.GetPublicPiece(t.Context(), lantern.Slug)
	require.NoError(t, err)
	assert.Equal(t, "The Brass Lantern (contributed)", piece.Title)
}

// A contributor's next submission of an entity withdraws their earlier pending one, and with
// MaxPendingRevisions pending the next is refused.
func TestSubmit_ContributorPendingRevisions(t *testing.T) {
	fx := newWriteFixture(t)
	lantern := fx.c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: lanternTitle}})
	first := fx.submit(t, lantern.RevisionID, pieceDoc("Lantern, first try"), false)
	second := fx.submit(t, lantern.RevisionID, pieceDoc("Lantern, second try"), false)

	view, err := fx.repo.GetMyRevision(t.Context(), wrenID, first)
	require.NoError(t, err)
	assert.Equal(t, domain.RevisionStatusWithdrawn, view.(domain.PieceRevisionView).Status)
	_, err = fx.repo.GetMyRevision(t.Context(), heronID, second)
	require.ErrorIs(t, err, datasources.ErrNotFound, "another user's revision")
	require.ErrorIs(t, fx.repo.WithdrawRevision(t.Context(), second, heronID), datasources.ErrNotFound)
	for range datasources.MaxPendingRevisions - 1 {
		fx.submit(t, 0, pieceDoc("A new piece"), false)
	}
	_, err = fx.repo.SubmitRevision(t.Context(), fx.revision(0, pieceDoc("One too many"), false))
	require.ErrorIs(t, err, datasources.ErrPendingLimit)

	pending := domain.RevisionStatusPending
	mine, err := fx.repo.ListMyRevisions(t.Context(), wrenID, &pending, domain.Page{Limit: 5})
	require.NoError(t, err)
	assert.Equal(t, int64(datasources.MaxPendingRevisions), mine.Total)
	queue, err := fx.repo.ListModerationQueue(t.Context(), domain.Page{Limit: 1})
	require.NoError(t, err)
	assert.Equal(t, int64(datasources.MaxPendingRevisions), queue.Total)
	require.Len(t, queue.Revisions, 1)
	oldest := queue.Revisions[0]
	assert.Equal(t, second, oldest.ID)
	assert.Equal(t, &domain.RevisionAuthorAccount{UserID: wrenID, DisplayName: "Linnet"}, oldest.Author)
	assert.Equal(t, lantern.Slug, *oldest.Entity.Slug)

	newPiece, err := fx.repo.GetMyRevision(t.Context(), wrenID, second+1)
	require.NoError(t, err)
	assert.Nil(t, newPiece.(domain.PieceRevisionView).Entity, "a new entity's revision has no entity")
}

// The editable document is the caller's own pending revision if they have one, and otherwise the current one, in
// the editor format without hidden links. A credit to a person who prefers no page keeps their slug.
func TestGetEditable_OwnPendingOrCurrent(t *testing.T) {
	fx := newWriteFixture(t)
	const visible, takenDown = "https://example.org/lantern", "https://example.org/lantern/taken-down"
	quiet := fx.c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Quiet Harper",
		Status: domain.PersonStatusPrefersNoPage}})
	doc := pieceDoc(lanternTitle)
	doc.Credits = []domain.DocCredit{{PersonID: &quiet.ID, Role: domain.CreditRoleMusic,
		Attribution: domain.AttributionNamed, Certainty: domain.CertaintyCertain}}
	doc.Links = []domain.DocPieceLink{pieceLink(visible, domain.LinkRoleLyrics),
		pieceLink(takenDown, domain.LinkRoleText)}
	lantern := fx.c.Piece(catalogtest.Piece{Doc: doc})
	fx.c.SetLinkState(takenDown, catalogtest.LinkState{IsHidden: true})

	current, err := fx.repo.GetEditable(t.Context(), domain.EntityTypePiece, lantern.Slug, wrenID)
	require.NoError(t, err)
	assert.Equal(t, lantern.RevisionID, current.BaseRevisionID)
	assert.Nil(t, current.PendingRevisionID)
	editor := current.Document.(domain.EditorPieceDocument)
	require.Len(t, editor.Links, 1)
	assert.Equal(t, visible, editor.Links[0].URL)
	assert.Equal(t, quiet.Slug, *editor.Credits[0].PersonSlug)

	pending := fx.submit(t, lantern.RevisionID, pieceDoc("The Brass Lantern, edited"), false)
	own, err := fx.repo.GetEditable(t.Context(), domain.EntityTypePiece, lantern.Slug, wrenID)
	require.NoError(t, err)
	assert.Equal(t, &pending, own.PendingRevisionID)
	assert.Equal(t, lantern.RevisionID, own.BaseRevisionID)
	assert.Equal(t, "The Brass Lantern, edited", own.Document.(domain.EditorPieceDocument).Title)
	others, err := fx.repo.GetEditable(t.Context(), domain.EntityTypePiece, lantern.Slug, heronID)
	require.NoError(t, err)
	assert.Equal(t, lanternTitle, others.Document.(domain.EditorPieceDocument).Title)
}

// TestDuplicateCandidates_PrefersNoPage checks that a person who prefers no page is a duplicate candidate marked
// so, which blocks a new person matching them.
func TestDuplicateCandidates_PrefersNoPage(t *testing.T) {
	fx := newWriteFixture(t)
	quiet := fx.c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Quiet Harper",
		Status: domain.PersonStatusPrefersNoPage}})

	candidates, err := fx.repo.DuplicateCandidates(t.Context(), domain.EntityTypePerson, "")
	require.NoError(t, err)
	i := slices.IndexFunc(candidates, func(c domain.DuplicateCandidate) bool { return *c.Slug == quiet.Slug })
	require.GreaterOrEqual(t, i, 0)
	assert.True(t, candidates[i].PrefersNoPage)
}
