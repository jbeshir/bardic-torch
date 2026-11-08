//go:build integration

package mysql_test

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	kestrelTitle  = "Kestrel Lament"
	theirsNotes   = "Notes from the other edit."
	conservedURL  = "https://example.org/wren/endangered"
	approvalNote  = "Thank you."
	resolvedTitle = "The Wren's Song, Resolved"
)

// approve approves the revision id as Rowan, a moderator, with opts.
func (f *editingFixture) approve(t *testing.T, id int64, opts domain.ApprovalOptions) error {
	t.Helper()

	return f.repo.ApproveRevision(t.Context(), datasources.ApproveRequest{Actor: actorRowan, RevisionID: id,
		Options: opts, Duplicates: f.approvalChecks(t, id), Now: f.tick()})
}

// mustApprove approves the revision id, failing the test on an error.
func (f *editingFixture) mustApprove(t *testing.T, id int64) {
	t.Helper()
	require.NoError(t, f.approve(t, id, domain.ApprovalOptions{}))
}

// reviewed is the review columns of a revisions row.
type reviewed struct {
	reviewer, note, summary string
	createdAt, reviewedAt   sql.NullTime
}

func (f *editingFixture) reviewed(t *testing.T, id int64) reviewed {
	t.Helper()
	var r reviewed
	var reviewer, note sql.NullString
	var createdAt time.Time
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT reviewer_user_id, review_note, summary, created_at, "+
		"reviewed_at FROM revisions WHERE id = ?", id).Scan(&reviewer, &note, &r.summary, &createdAt, &r.reviewedAt))
	r.reviewer, r.note, r.createdAt = reviewer.String, note.String, sql.NullTime{Time: createdAt, Valid: true}

	return r
}

// currentRevision is the current revision of the row id of table.
func (f *editingFixture) currentRevision(t *testing.T, table string, id int64) int64 {
	t.Helper()

	return int64(f.count(t, "SELECT COALESCE(current_revision_id, 0) FROM "+table+" WHERE id = ?", id))
}

func (f *editingFixture) publicPiece(t *testing.T, slug string) domain.Piece {
	t.Helper()
	piece, err := f.repo.GetPublicPiece(t.Context(), slug)
	require.NoError(t, err)

	return piece
}

func (f *editingFixture) storedPiece(t *testing.T, id int64) domain.PieceDocument {
	t.Helper()
	doc, err := domain.DecodeDocument[domain.PieceDocument]([]byte(f.stored(t, id).snapshot))
	require.NoError(t, err)

	return doc
}

func TestRepository_ApproveRevision_NotStale(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	result := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.NotesMD = new(editedNotes)
	})

	require.NoError(t, f.approve(t, result.RevisionID, domain.ApprovalOptions{ReviewNote: approvalNote}))
	approvedAt := f.now

	stored := f.stored(t, result.RevisionID)
	assert.Equal(t, statusApproved, stored.status)
	assert.Equal(t, f.song.RevisionID, stored.base.Int64, "not stale, approved as is")
	want := songDocument(f)
	want.NotesMD = new(editedNotes)
	assert.Equal(t, want, f.storedPiece(t, result.RevisionID), "the full document: withheld elements restored")
	r := f.reviewed(t, result.RevisionID)
	assert.Equal(t, rowanUserID, r.reviewer)
	assert.Equal(t, approvalNote, r.note)
	assert.Equal(t, approvedAt, r.reviewedAt.Time.UTC())
	assert.Equal(t, approvedAt, r.createdAt.Time.UTC(), "dated by its approval")
	assert.Equal(t, result.RevisionID, f.currentRevision(t, "pieces", f.song.ID))
	assert.Equal(t, new(editedNotes), f.publicPiece(t, f.song.Slug).NotesMD)
	assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM links WHERE is_hidden AND normalized_url LIKE ?",
		"%wren/hidden%"), "the hidden link stays hidden")
	assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM piece_preferences WHERE piece_id = ?", f.song.ID),
		"the preferences, withheld from the contributor, are kept")

	require.ErrorIs(t, f.approve(t, result.RevisionID, domain.ApprovalOptions{}), domain.ErrNotPending,
		"a second approval")
	require.ErrorIs(t, f.approve(t, 999999, domain.ApprovalOptions{}), datasources.ErrNotFound)
	draft := f.saveDraft(t, actorWren, nil, newPieceDocument(unsungTitle))
	require.ErrorIs(t, f.approve(t, draft.Draft.ID, domain.ApprovalOptions{}), datasources.ErrNotFound,
		"drafts are private")
	err := f.repo.ApproveRevision(t.Context(), datasources.ApproveRequest{Actor: actorRowanToken,
		RevisionID: result.RevisionID, Now: f.tick()})
	require.ErrorIs(t, err, domain.ErrModeratorRequired, "an API token has contributor powers only")
}

// TestRepository_ApproveRevision_Stale covers a clean merge after another approval of the entity, a conflict,
// and a base that is the import revision, as every fixture entity's is.
func TestRepository_ApproveRevision_Stale(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	ours := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.NotesMD = new(editedNotes) })
	theirs := editAndSubmit(t, f, actorAsh, key, func(d *domain.PieceDocument) { d.IsStub = true })
	conflicting := editAndSubmit(t, f, actorRowanToken, key, func(d *domain.PieceDocument) {
		d.NotesMD = new(theirsNotes)
	})
	f.mustApprove(t, theirs.RevisionID)

	f.mustApprove(t, ours.RevisionID)
	stored := f.stored(t, ours.RevisionID)
	assert.Equal(t, theirs.RevisionID, stored.base.Int64, "rebased onto the current revision")
	assert.Equal(t, f.song.RevisionID, stored.submittedBase.Int64, "the base the author edited from is kept")
	merged := f.storedPiece(t, ours.RevisionID)
	assert.Equal(t, new(editedNotes), merged.NotesMD)
	assert.True(t, merged.IsStub, "the other approval's change is kept")
	assert.Equal(t, songDocument(f).Preferences, merged.Preferences)

	require.ErrorIs(t, f.approve(t, conflicting.RevisionID, domain.ApprovalOptions{}), domain.ErrRebaseRequired)
	assert.Equal(t, statusPending, f.stored(t, conflicting.RevisionID).status, "nothing changes")
	assert.Equal(t, ours.RevisionID, f.currentRevision(t, "pieces", f.song.ID))
}

// TestRepository_ApproveRevision_EntityGone covers: the revision's own placeholder is gone.
func TestRepository_ApproveRevision_EntityGone(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	pending := f.newPending(t, actorWren, newPieceDocument(unsungTitle))
	f.c.Exec("DELETE FROM pieces WHERE id = ?", pending.Entity.ID)

	require.ErrorIs(t, f.approve(t, pending.ID, domain.ApprovalOptions{}), datasources.ErrEntityGone)
	assert.Equal(t, statusPending, f.stored(t, pending.ID).status)
}

// TestRepository_ApproveRevision_DeletedPendingReference covers: the placeholder of a term the revision
// references was deleted by its creating revision's rejection.
func TestRepository_ApproveRevision_DeletedPendingReference(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	term := f.newPending(t, actorWren, newTermDocument("Laments"))
	piece := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.Terms = append(slices.Clone(d.Terms), domain.DocPieceTerm{TermID: term.Entity.ID})
	})
	require.NoError(t, f.repo.RejectRevision(t.Context(), datasources.RejectRequest{Actor: actorRowan,
		RevisionID: term.ID, Now: f.tick()}))

	err := f.approve(t, piece.RevisionID, domain.ApprovalOptions{})
	var problems *domain.DocumentError
	require.ErrorAs(t, err, &problems)
	assert.Contains(t, problems.Problems, domain.DocumentProblem{Path: pathSecondTerm,
		Code: domain.ProblemUnknownReference})
}

// TestRepository_ApproveRevision_PendingReferences covers: pending new terms, a parent chain, and a
// pending new person, approved alongside the piece referencing them, parents first, with the closure rebuilt.
func TestRepository_ApproveRevision_PendingReferences(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	parent := f.newPending(t, actorWren, newTermDocument("Laments"))
	childDoc := newTermDocument("Bird Laments")
	childDoc.ParentID = &parent.Entity.ID
	child := f.newPending(t, actorWren, childDoc)
	person := f.newPending(t, actorWren, newPersonDocument("Linnet Placeholder"))
	piece := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.Terms = append(slices.Clone(d.Terms), domain.DocPieceTerm{TermID: child.Entity.ID})
		d.Credits = append(slices.Clone(d.Credits), domain.DocCredit{PersonID: &person.Entity.ID,
			Role: domain.CreditRoleTune, Attribution: domain.AttributionNamed, Certainty: domain.CertaintyCertain})
	})
	assert.Contains(t, f.stored(t, piece.RevisionID).flags, string(domain.FlagReferencesPending))

	f.mustApprove(t, piece.RevisionID)

	for _, id := range []int64{parent.ID, child.ID, person.ID, piece.RevisionID} {
		assert.Equal(t, statusApproved, f.stored(t, id).status, id)
		assert.Equal(t, rowanUserID, f.reviewed(t, id).reviewer)
	}
	assert.Equal(t, parent.ID, f.currentRevision(t, "terms", parent.Entity.ID))
	assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM term_ancestors WHERE ancestor_id = ? AND descendant_id = ? "+
		"AND depth = 1", parent.Entity.ID, child.Entity.ID), "the closure is rebuilt")
	got := f.publicPiece(t, f.song.Slug)
	var terms []string
	for _, term := range got.Terms {
		terms = append(terms, term.Term.DisplayName)
	}
	assert.Contains(t, terms, "Bird Laments")
	var credited []string
	for _, credit := range got.Credits {
		if credit.Person != nil {
			credited = append(credited, credit.Person.Name)
		}
	}
	assert.Contains(t, credited, "Linnet Placeholder")
	term, err := f.repo.GetPublicTerm(t.Context(), "bird-laments", domain.Page{Limit: 10})
	require.NoError(t, err, "the new term has its slug")
	assert.Equal(t, "Bird Laments", term.DisplayName)
}

// TestRepository_ApproveRevision_FlaggedPendingReference covers: a pending term flagged on its own refuses the
// approval of a piece referencing it, which writes nothing, until the term is approved on its own.
func TestRepository_ApproveRevision_FlaggedPendingReference(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	termDraft, err := f.repo.SaveDraft(t.Context(), datasources.SaveDraftRequest{Actor: actorWren,
		EntityType: domain.EntityTypeTerm, Document: newTermDocument("Laments"),
		Summary: "As " + morwenLegal + " sang them.", Now: f.tick()})
	require.NoError(t, err)
	term := f.submit(t, actorWren, termDraft.Draft.ID)
	termRevision, err := f.repo.GetMyRevision(t.Context(), actorWren, term.RevisionID)
	require.NoError(t, err)
	piece := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.Terms = append(slices.Clone(d.Terms), domain.DocPieceTerm{TermID: termRevision.Entity.ID})
	})

	err = f.approve(t, piece.RevisionID, domain.ApprovalOptions{})
	var flagged *domain.PendingReferenceFlaggedError
	require.ErrorAs(t, err, &flagged)
	assert.Equal(t, term.RevisionID, flagged.RevisionID)
	assert.Equal(t, statusPending, f.stored(t, piece.RevisionID).status, "nothing was written")
	assert.Equal(t, statusPending, f.stored(t, term.RevisionID).status)

	require.NoError(t, f.approve(t, term.RevisionID, domain.ApprovalOptions{
		AcknowledgedFlags: []domain.FlagCode{domain.FlagHiddenAliasInSummary}, Summary: new("Laments.")}))
	assert.Equal(t, "Laments.", f.reviewed(t, term.RevisionID).summary, "the override is published")
	f.mustApprove(t, piece.RevisionID)
}

// TestRepository_ApproveRevision_NewPiece covers the slug allocated from the title, after a collision, and the
// sort keys.
func TestRepository_ApproveRevision_NewPiece(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	require.Equal(t, "the-wren-s-song", f.song.Slug)
	pending := f.newPending(t, actorWren, newPieceDocument(songTitle))
	// The title is the song's, so the moderator acknowledges the possible duplicate.
	require.NoError(t, f.approve(t, pending.ID, domain.ApprovalOptions{AcknowledgedFlags: []domain.FlagCode{domain.FlagPossibleDuplicate}}))

	var slug, sortTitle, letter string
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT slug, sort_title, sort_letter FROM pieces "+
		"WHERE id = ?", pending.Entity.ID).Scan(&slug, &sortTitle, &letter))
	assert.Equal(t, "the-wren-s-song-2", slug)
	assert.Equal(t, "Wren's Song", sortTitle)
	assert.Equal(t, "W", letter)
	assert.Equal(t, songTitle, f.publicPiece(t, slug).Title)

	edited := editAndSubmit(t, f, actorWren, *pieceKey(slug), func(d *domain.PieceDocument) { d.Title = "A Tale" })
	f.mustApprove(t, edited.RevisionID)
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT slug, sort_title, sort_letter FROM pieces "+
		"WHERE id = ?", pending.Entity.ID).Scan(&slug, &sortTitle, &letter))
	assert.Equal(t, "the-wren-s-song-2", slug, "slugs are fixed at the first approval")
	assert.Equal(t, "Tale", sortTitle)
	assert.Equal(t, "T", letter)
}

// TestRepository_ApproveRevision_HistoryOrder covers: a revision drafted before another but approved after it
// is dated by its approval, and listed first.
func TestRepository_ApproveRevision_HistoryOrder(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	early := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.NotesMD = new(editedNotes) })
	late := editAndSubmit(t, f, actorAsh, key, func(d *domain.PieceDocument) { d.IsStub = true })
	f.mustApprove(t, late.RevisionID)
	f.mustApprove(t, early.RevisionID)

	history, err := f.repo.ListPublicHistory(t.Context(), domain.SlugEntityTypePiece, f.song.Slug,
		domain.Page{Limit: 10})
	require.NoError(t, err)
	var ids []int64
	for _, r := range history.Revisions {
		ids = append(ids, r.ID)
	}
	assert.Equal(t, []int64{early.RevisionID, late.RevisionID, f.song.RevisionID}, ids)
	assert.Equal(t, early.RevisionID, f.currentRevision(t, "pieces", f.song.ID), "the current revision is first")
}

// TestRepository_ApproveRevision_Conservation checks that a status change appends conservation history, with the
// summary as its reason, and that an approval leaving the status unchanged does not.
func TestRepository_ApproveRevision_Conservation(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	key := *pieceKey(f.song.Slug)
	draftDoc := f.editable(t, actorWren, key).Document.(domain.PieceDocument)
	draftDoc.Conservation = domain.DocConservation{Status: domain.ConservationStatusEndangered,
		SourceURL: new(conservedURL)}
	draft := f.saveDraft(t, actorWren, &key, draftDoc)
	changed := f.submit(t, actorWren, draft.Draft.ID)
	f.mustApprove(t, changed.RevisionID)

	var status, reason, source string
	var revision int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT status, reason, source_url, revision_id "+
		"FROM conservation_history WHERE piece_id = ?", f.song.ID).Scan(&status, &reason, &source, &revision))
	assert.Equal(t, []string{"endangered", draftSummary, conservedURL}, []string{status, reason, source})
	assert.Equal(t, changed.RevisionID, revision)

	unchanged := editAndSubmit(t, f, actorWren, key, func(d *domain.PieceDocument) { d.NotesMD = new(editedNotes) })
	f.mustApprove(t, unchanged.RevisionID)
	assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM conservation_history WHERE piece_id = ?", f.song.ID))
}

// TestRepository_ApproveRevision_Links checks the link upsert by normalized URL: a respelled URL reuses its row and
// respells it, a hidden row stays hidden, a row is never deleted, and the link write rule for shared fields.
func TestRepository_ApproveRevision_Links(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	links := f.count(t, "SELECT COUNT(*) FROM links")
	respelled := "https://EXAMPLE.org/wren/lyrics"
	edit := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.Links[0].URL = respelled
		d.Links[0].Title = new("Lyrics")
	})
	f.mustApprove(t, edit.RevisionID)
	assert.Equal(t, links, f.count(t, "SELECT COUNT(*) FROM links"), "the respelled URL reuses its row")
	var url, title string
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT url, title FROM links WHERE normalized_url = ?",
		"https://example.org/wren/lyrics").Scan(&url, &title))
	assert.Equal(t, []string{respelled, "Lyrics"}, []string{url, title}, "the editor changed both")
	assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM links WHERE url = ? AND is_hidden", songHiddenURL))

	// Morwen's document attaches the lyrics link with another title: the row keeps the piece's title, and gets the
	// description it had none of.
	person := editAndSubmit(t, f, actorWren, *personKey(f.morwen.Slug), func(d *domain.PersonDocument) {
		d.Links = append(slices.Clone(d.Links), domain.DocPersonLink{DocLink: domain.DocLink{URL: songLyricsURL,
			Title: new("Morwen's lyrics"), Description: new("Her words."),
			Roles: []domain.LinkRole{domain.LinkRoleLyrics}}})
	})
	f.mustApprove(t, person.RevisionID)
	var description string
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT title, description FROM links "+
		"WHERE normalized_url = ?", "https://example.org/wren/lyrics").Scan(&title, &description))
	assert.Equal(t, []string{"Lyrics", "Her words."}, []string{title, description})

	// The piece drops the link: its row and Morwen's attachment stay.
	dropped := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.Links = d.Links[1:]
	})
	f.mustApprove(t, dropped.RevisionID)
	assert.Equal(t, links, f.count(t, "SELECT COUNT(*) FROM links"), "links rows are never deleted")
	assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM link_attachments a JOIN links l ON l.id = a.link_id "+
		"WHERE l.normalized_url = ? AND a.person_id = ?", "https://example.org/wren/lyrics", f.morwen.ID))
	assert.Equal(t, 0, f.count(t, "SELECT COUNT(*) FROM link_attachments a JOIN links l ON l.id = a.link_id "+
		"WHERE l.normalized_url = ? AND a.piece_id = ?", "https://example.org/wren/lyrics", f.song.ID))
}

// TestRepository_ApproveRevision_Performances checks that an approval replaces exactly the performances its
// entity's document owns.
func TestRepository_ApproveRevision_Performances(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	person := editAndSubmit(t, f, actorWren, *personKey(f.ash.Slug), func(d *domain.PersonDocument) {
		d.Links = append(slices.Clone(d.Links), domain.DocPersonLink{
			DocLink: domain.DocLink{URL: songLyricsURL, Roles: []domain.LinkRole{domain.LinkRolePerformance}},
			Performances: []domain.DocPersonPerformance{{PieceID: &f.song.ID,
				DocPerformance: domain.DocPerformance{DescriptionVerbatim: "Ash at the fair",
					PerformerIDs: []int64{f.ash.ID}}}},
		})
	})
	f.mustApprove(t, person.RevisionID)
	// An interactive moderator, who sees the performer withheld from contributors: a contributor's removal of that
	// performance would be flagged hidden_data_conflict.
	piece := editAndSubmit(t, f, actorRowan, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.Links[0].Performances = []domain.DocPerformance{{DescriptionVerbatim: "Sung again",
			PerformerIDs: []int64{f.morwen.ID}}}
	})
	require.Equal(t, domain.RevisionStatusApproved, piece.Status)

	rows := func(query string, id int64) []string {
		r, err := f.db.QueryContext(t.Context(), query, id)
		require.NoError(t, err)
		defer func() { _ = r.Close() }()
		var out []string
		for r.Next() {
			var s string
			require.NoError(t, r.Scan(&s))
			out = append(out, s)
		}
		require.NoError(t, r.Err())
		return out
	}
	assert.Equal(t, []string{"Sung again"}, rows("SELECT description_verbatim FROM performances WHERE owner_piece_id = ? ORDER BY id", f.song.ID), "the piece's own, replaced")
	assert.Equal(t, []string{"Ash at the fair"}, rows("SELECT description_verbatim FROM performances WHERE owner_person_id = ? ORDER BY id",
		f.ash.ID), "Ash's, untouched")
	assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM performance_performers pp JOIN performances p "+
		"ON p.id = pp.performance_id WHERE p.owner_piece_id = ?", f.song.ID), "performers replaced with them")
}

// TestRepository_ApproveRevision_Flags covers the flags hidden_alias_collision, hidden_alias_in_summary
// (acknowledged without an override) and the review note gate.
func TestRepository_ApproveRevision_Flags(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	collided := editAndSubmit(t, f, actorWren, *personKey(f.morwen.Slug), func(d *domain.PersonDocument) {
		d.NotesMD = new(editedNotes)
	})
	// The flag a restore collision stores at submit, which only the submission can find.
	f.c.Exec("UPDATE revisions SET flags_json = ? WHERE id = ?",
		`[{"code": "hidden_alias_collision", "path": "/aliases/2"}]`, collided.RevisionID)
	require.ErrorIs(t, f.approve(t, collided.RevisionID, domain.ApprovalOptions{}), domain.ErrHiddenDataConflict)

	draft, err := f.repo.SaveDraft(t.Context(), datasources.SaveDraftRequest{Actor: actorAsh,
		EntityType: domain.EntityTypePiece, Entity: pieceKey(f.song.Slug),
		Document: f.editable(t, actorAsh, *pieceKey(f.song.Slug)).Document, Summary: "For " + morwenLegal,
		Now: f.tick()})
	require.NoError(t, err)
	summarised := f.submit(t, actorAsh, draft.Draft.ID)
	ack := []domain.FlagCode{domain.FlagHiddenAliasInSummary}
	var unacknowledged *domain.UnacknowledgedFlagsError
	require.ErrorAs(t, f.approve(t, summarised.RevisionID, domain.ApprovalOptions{AcknowledgedFlags: ack}),
		&unacknowledged, "no override")
	require.ErrorAs(t, f.approve(t, summarised.RevisionID, domain.ApprovalOptions{AcknowledgedFlags: ack,
		Summary: new(cleanSummaryText), ReviewNote: "Mind " + morwenLegal}), &unacknowledged, "the review note")
	assert.Equal(t, []domain.Flag{{Code: domain.FlagHiddenAliasInReviewNote, Path: "/review_note"}},
		unacknowledged.Flags)
	require.NoError(t, f.approve(t, summarised.RevisionID, domain.ApprovalOptions{AcknowledgedFlags: ack,
		Summary: new(cleanSummaryText)}))
	history, err := f.repo.ListPublicHistory(t.Context(), domain.SlugEntityTypePiece, f.song.Slug,
		domain.Page{Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, cleanSummaryText, history.Revisions[0].Summary, "the public history shows the override")
}

const cleanSummaryText = "A clean summary."

// TestRepository_ApproveRevision_NoPageCredit covers: a credit to a person who has since come to prefer no page
// stays when another field is approved, and reads leave it out.
func TestRepository_ApproveRevision_NoPageCredit(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	result := editAndSubmit(t, f, actorRowanToken, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.NotesMD = new(editedNotes)
	})
	f.mustApprove(t, result.RevisionID)
	doc := f.storedPiece(t, result.RevisionID)
	assert.Equal(t, songDocument(f).Credits, doc.Credits, "the credit to Quiet Harper stays")
	for _, credit := range f.publicPiece(t, f.song.Slug).Credits {
		if credit.Person != nil {
			assert.NotEqual(t, quietHarper, credit.Person.Name)
		}
	}
}

// TestRepository_ApproveRevision_SearchAfterCommit checks that a piece approved is found by a search as soon as the
// approval returns (FULLTEXT sees committed rows only).
func TestRepository_ApproveRevision_SearchAfterCommit(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	pending := f.newPending(t, actorWren, newPieceDocument(kestrelTitle))
	f.mustApprove(t, pending.ID)
	search, err := domain.ParseSearchQuery("kestrel")
	require.NoError(t, err)

	list, err := f.repo.ListPublicPieces(t.Context(), datasources.PieceQuery{Search: &search,
		Sort: domain.PieceSortRelevance, Page: domain.Page{Limit: 10}})
	require.NoError(t, err)
	require.Len(t, list.Pieces, 1)
	assert.Equal(t, kestrelTitle, list.Pieces[0].Title)
}

// TestRepository_ApproveRevision_HiddenDataConflict covers that a revision flagged hidden_data_conflict at submit
// is refused.
func TestRepository_ApproveRevision_HiddenDataConflict(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	result := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.NotesMD = new(editedNotes)
	})
	f.c.Exec("UPDATE revisions SET flags_json = ? WHERE id = ?",
		`[{"code": "hidden_data_conflict", "path": "/credits/1"}]`, result.RevisionID)

	require.ErrorIs(t, f.approve(t, result.RevisionID, domain.ApprovalOptions{}), domain.ErrHiddenDataConflict)
}

// TestRepository_ApproveRevision_TermNameTaken covers: a new term whose name a term of its facet approved
// since its submission has is refused.
func TestRepository_ApproveRevision_TermNameTaken(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	pending := f.newPending(t, actorWren, newTermDocument("Laments"))
	// Each term may duplicate the other, which the moderator acknowledges.
	draft := f.saveDraft(t, actorRowan, nil, newTermDocument("Laments"))
	req := f.submitRequest(t, actorRowan, draft.Draft.ID)
	req.AcknowledgedFlags = []domain.FlagCode{domain.FlagPossibleDuplicate}
	_, err := f.repo.SubmitDraft(t.Context(), req)
	require.NoError(t, err)

	err = f.approve(t, pending.ID, domain.ApprovalOptions{AcknowledgedFlags: []domain.FlagCode{domain.FlagPossibleDuplicate}})
	var problems *domain.DocumentError
	require.ErrorAs(t, err, &problems)
	assert.Contains(t, problems.Problems, domain.DocumentProblem{Path: "/name", Code: domain.ProblemDuplicate})
	assert.Equal(t, statusPending, f.stored(t, pending.ID).status)
}

// creditTo returns a change crediting the piece's tune to the person id.
func creditTo(id int64) func(d *domain.PieceDocument) {
	return func(d *domain.PieceDocument) {
		d.Credits = append(slices.Clone(d.Credits), domain.DocCredit{PersonID: &id, Role: domain.CreditRoleTune,
			Attribution: domain.AttributionNamed, Certainty: domain.CertaintyCertain})
	}
}

// tagWith returns a change adding the term id to the piece's terms.
func tagWith(id int64) func(d *domain.PieceDocument) {
	return func(d *domain.PieceDocument) {
		d.Terms = append(slices.Clone(d.Terms), domain.DocPieceTerm{TermID: id})
	}
}

// TestRepository_ApproveRevision_PendingReferenceDuplicate requires a pending new person approved alongside a piece
// to pass the moderators' duplicate check too: a person of the same name made public since its submission refuses
// the approval with pending_reference_flagged, possible_duplicate among its flags, and publishes nothing.
func TestRepository_ApproveRevision_PendingReferenceDuplicate(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	person := f.newPending(t, actorWren, newPersonDocument("Ash Example"))
	piece := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), creditTo(person.Entity.ID))
	f.c.Person(catalogtest.Person{Doc: newPersonDocument("Ash Example")})

	err := f.approve(t, piece.RevisionID, domain.ApprovalOptions{})
	var flagged *domain.PendingReferenceFlaggedError
	require.ErrorAs(t, err, &flagged)
	assert.Equal(t, person.ID, flagged.RevisionID)
	assert.True(t, slices.ContainsFunc(flagged.Flags, func(f domain.Flag) bool {
		return f.Code == domain.FlagPossibleDuplicate
	}), "the refusal names possible_duplicate: %v", flagged.Flags)
	assert.Equal(t, statusPending, f.stored(t, piece.RevisionID).status)
	assert.Equal(t, statusPending, f.stored(t, person.ID).status)
	assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM persons WHERE id = ? AND current_revision_id IS NULL",
		person.Entity.ID), "the person is not published")

	// Approved on its own first, the moderator acknowledging the duplicate, the piece is then approved.
	require.NoError(t, f.approve(t, person.ID,
		domain.ApprovalOptions{AcknowledgedFlags: []domain.FlagCode{domain.FlagPossibleDuplicate}}))
	f.mustApprove(t, piece.RevisionID)
}

// TestRepository_ApproveRevision_RebasedSinceDuplicates requires an approval whose duplicate check was made before
// a rebase renamed the new entity to be refused with datasources.ErrDocumentChanged, approving nothing; checked
// again, the new name's match is a possible duplicate.
func TestRepository_ApproveRevision_RebasedSinceDuplicates(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	pending := f.newPending(t, actorWren, newPersonDocument("Linnet Placeholder"))
	checks := f.approvalChecks(t, pending.ID)
	renamed := f.moderationRevision(t, pending.ID).Document.(domain.PersonDocument)
	renamed.Name = ashFiddler
	require.NoError(t, f.rebase(t, pending.ID, nil, renamed, nil))

	err := f.repo.ApproveRevision(t.Context(), datasources.ApproveRequest{Actor: actorRowan, RevisionID: pending.ID,
		Duplicates: checks, Now: f.tick()})
	require.ErrorIs(t, err, datasources.ErrDocumentChanged)
	assert.Equal(t, statusPending, f.stored(t, pending.ID).status)

	var unacknowledged *domain.UnacknowledgedFlagsError
	require.ErrorAs(t, f.approve(t, pending.ID, domain.ApprovalOptions{}), &unacknowledged)
	assert.Equal(t, []domain.Flag{{Code: domain.FlagPossibleDuplicate}}, unacknowledged.Flags)
}

// TestRepository_ApproveRevision_ReferenceRejectedWhileWaiting forces the rejection of a pending term to commit while
// the approval of a piece referencing it waits for the term's revision: the approval sees the term rejected and its
// placeholder gone, and is refused, publishing nothing.
func TestRepository_ApproveRevision_ReferenceRejectedWhileWaiting(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	term := f.newPending(t, actorWren, newTermDocument("Laments"))
	piece := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), tagWith(term.Entity.ID))
	req := datasources.ApproveRequest{Actor: actorRowan, RevisionID: piece.RevisionID,
		Duplicates: f.approvalChecks(t, piece.RevisionID), Now: f.tick()}

	holder := holdRows(t, f.db, "SELECT id FROM revisions WHERE id = ? FOR UPDATE", term.ID)
	require.NoError(t, f.repo.RejectRevisionInTx(t.Context(), holder.tx, datasources.RejectRequest{Actor: actorRowan,
		RevisionID: term.ID, ReviewNote: "Not a topic.", Now: f.tick()}))
	done := started(t.Context(), func(ctx context.Context) error { return f.repo.ApproveRevision(ctx, req) })
	waitUntilBlocked(t, f.db, 1)
	holder.commit()
	err := <-done

	var problems *domain.DocumentError
	if !errors.Is(err, datasources.ErrEntityGone) {
		require.ErrorAs(t, err, &problems)
		assert.Contains(t, problems.Problems, domain.DocumentProblem{Path: "/terms/1/term",
			Code: domain.ProblemUnknownReference})
	}
	assert.Equal(t, statusPending, f.stored(t, piece.RevisionID).status)
	assert.Equal(t, statusRejected, f.stored(t, term.ID).status)
	assert.Equal(t, f.song.RevisionID, f.currentRevision(t, "pieces", f.song.ID))
}

// TestRepository_ApproveRevision_RebasedWhileWaiting forces a rebase to commit while an approval of the revision
// waits for its lock: the approval approves the rebased document.
func TestRepository_ApproveRevision_RebasedWhileWaiting(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	pending := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.NotesMD = new(editedNotes)
	})
	req := datasources.ApproveRequest{Actor: actorRowan, RevisionID: pending.RevisionID, Now: f.tick()}
	resolved := f.moderationRevision(t, pending.RevisionID).Document.(domain.PieceDocument)
	resolved.NotesMD = new("Notes as the moderator rebased them.")

	holder := holdRows(t, f.db, "SELECT id FROM revisions WHERE id = ? FOR UPDATE", pending.RevisionID)
	require.NoError(t, f.repo.RebaseRevisionInTx(t.Context(), holder.tx, datasources.RebaseRequest{Actor: actorRowan,
		RevisionID: pending.RevisionID, CurrentRevisionID: &f.song.RevisionID, Document: resolved, Now: f.tick()}))
	done := started(t.Context(), func(ctx context.Context) error { return f.repo.ApproveRevision(ctx, req) })
	waitUntilBlocked(t, f.db, 1)
	holder.commit()
	require.NoError(t, <-done)

	assert.Equal(t, pending.RevisionID, f.currentRevision(t, "pieces", f.song.ID))
	assert.Equal(t, "Notes as the moderator rebased them.", *f.storedPiece(t, pending.RevisionID).NotesMD)
}

// TestRepository_ApproveRevision_ReferenceSupersededWhileLocking forces the author's superseding of a pending term
// to commit while the approval of a piece referencing it waits for the superseded revision: the approval finds the
// superseding revision, rather than refusing the piece's reference as unknown. Its duplicate check was of the
// superseded revision, so the approval is refused with datasources.ErrDocumentChanged; checked again, it approves
// the superseding revision alongside.
func TestRepository_ApproveRevision_ReferenceSupersededWhileLocking(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	first := f.newPending(t, actorWren, newTermDocument("Laments"))
	piece := editAndSubmit(t, f, actorWren, *pieceKey(f.song.Slug), tagWith(first.Entity.ID))
	req := datasources.ApproveRequest{Actor: actorRowan, RevisionID: piece.RevisionID,
		Duplicates: f.approvalChecks(t, piece.RevisionID), Now: f.tick()}
	termKey := domain.EntityKey{Type: domain.EntityTypeTerm, Slug: first.Entity.Slug}
	edited := f.editable(t, actorWren, termKey).Document.(domain.TermDocument)
	edited.DisplayName = "Bird Laments"
	superseding := f.submitRequest(t, actorWren, f.saveDraft(t, actorWren, &termKey, edited).Draft.ID)

	holder := holdRows(t, f.db, "SELECT id FROM revisions WHERE id = ? FOR UPDATE", first.ID)
	second, err := f.repo.SubmitDraftInTx(t.Context(), holder.tx, superseding)
	require.NoError(t, err)
	done := started(t.Context(), func(ctx context.Context) error { return f.repo.ApproveRevision(ctx, req) })
	waitUntilBlocked(t, f.db, 1)
	holder.commit()
	require.ErrorIs(t, <-done, datasources.ErrDocumentChanged)
	assert.Equal(t, statusPending, f.stored(t, piece.RevisionID).status)

	f.mustApprove(t, piece.RevisionID)
	assert.Equal(t, statusWithdrawn, f.stored(t, first.ID).status)
	assert.Equal(t, statusApproved, f.stored(t, second.RevisionID).status)
	assert.Equal(t, second.RevisionID, f.currentRevision(t, "terms", first.Entity.ID))
}

// TestRepository_BackfillPerformanceOwners checks that a performance with no owner, as a release that does not set
// owners stores it, is given its owner, and so is replaced by an approval of its entity instead of staying beside
// the copy the approval writes.
func TestRepository_BackfillPerformanceOwners(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	owned := editAndSubmit(t, f, actorRowan, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.Links[0].Performances = []domain.DocPerformance{{DescriptionVerbatim: "Sung before"}}
	})
	require.Equal(t, domain.RevisionStatusApproved, owned.Status)
	_, err := f.db.ExecContext(t.Context(), "UPDATE performances SET owner_piece_id = NULL WHERE owner_piece_id = ?",
		f.song.ID)
	require.NoError(t, err)
	perfCount := func() int {
		return f.count(t, "SELECT COUNT(*) FROM performances WHERE piece_id = ?", f.song.ID)
	}
	require.Equal(t, 1, perfCount())

	backfilled, err := f.repo.BackfillPerformanceOwners(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(1), backfilled)
	assert.Equal(t, 1, f.count(t, "SELECT COUNT(*) FROM performances WHERE owner_piece_id = ?", f.song.ID))
	again, err := f.repo.BackfillPerformanceOwners(t.Context())
	require.NoError(t, err)
	assert.Zero(t, again, "idempotent")

	replaced := editAndSubmit(t, f, actorRowan, *pieceKey(f.song.Slug), func(d *domain.PieceDocument) {
		d.Links[0].Performances = []domain.DocPerformance{{DescriptionVerbatim: "Sung after"}}
	})
	require.Equal(t, domain.RevisionStatusApproved, replaced.Status)
	assert.Equal(t, 1, perfCount(), "the approval replaced the performance instead of adding a second copy")
}
