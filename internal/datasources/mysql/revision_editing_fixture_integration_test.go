//go:build integration

package mysql_test

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/domaintest"
	"github.com/stretchr/testify/require"
)

// Fixture strings of the editing tests: invented names and example.org URLs only.
const (
	wrenUserID       = "google-oauth2|wren"
	ashUserID        = "google-oauth2|ash"
	rowanUserID      = "google-oauth2|rowan"
	songTitle        = "The Wren's Song"
	morwenName       = "Morwen Placeholder"
	morwenStage      = "Morwen"
	morwenLegal      = "Morwen Legalname"
	quietHarper      = "Quiet Harper"
	quietNotes       = "Asked not to have a page"
	ashFiddler       = "Ash Fiddler"
	balladsTerm      = "Ballads"
	guildTerm        = "Bards Guild"
	wrenArchive      = "Wren Archive"
	fairEvent        = "Midsummer Fair"
	songLyricsURL    = "https://example.org/wren/lyrics"
	songHiddenURL    = "https://example.org/wren/hidden"
	morwenSiteURL    = "https://example.org/morwen"
	morwenHidden     = "https://example.org/morwen/hidden"
	songNotes        = "A song about a wren."
	editedNotes      = "A song about a wren, edited."
	fairPerformed    = "Sung at the fair"
	draftSummary     = "Edit the notes"
	preferencesURL   = "https://example.org/wren/preferences"
	unsungTitle      = "Unsung"
	wrenDisplay      = "Wren Contributor"
	unknownPieceSlug = "unknown-piece-slug"
	unknownTermSlug  = "unknown-term-slug"
)

// editingFixture is a catalogue holding everything an editable document withholds from a contributor: a person's
// hidden legal alias and hidden link, a person who prefers no page credited and performing, a piece's hidden
// link and preferences. Wren and Ash are contributors, Rowan a moderator.
type editingFixture struct {
	db                 *sql.DB
	repo               *mysql.Repository
	c                  *catalogtest.Catalog
	ballads, guild     catalogtest.Ref
	morwen, quiet, ash catalogtest.Ref
	song               catalogtest.Ref
	archive            catalogtest.Ref
	fair               catalogtest.Ref
	now                time.Time
}

var (
	actorWren       = domain.Actor{UserID: wrenUserID, Role: domain.RoleContributor}
	actorAsh        = domain.Actor{UserID: ashUserID, Role: domain.RoleContributor}
	actorRowan      = domain.Actor{UserID: rowanUserID, Role: domain.RoleModerator}
	actorRowanToken = domain.Actor{UserID: rowanUserID, Role: domain.RoleContributor}
)

func newEditingFixture(t *testing.T) *editingFixture {
	t.Helper()
	db := catalogtest.Writable(t)
	c := catalogtest.New(t, db)
	for _, u := range []struct{ id, name, role string }{
		{wrenUserID, wrenDisplay, "contributor"}, {ashUserID, "Ash", "contributor"}, {rowanUserID, "Rowan", "moderator"},
	} {
		c.Exec("INSERT INTO users (id, display_name, role, created_at) VALUES (?, ?, ?, ?)", u.id, u.name, u.role,
			time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	}
	f := &editingFixture{db: db, repo: mysql.New(db), c: c, now: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}
	f.ballads = c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic, Name: balladsTerm}})
	f.guild = c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetAffiliation, Name: guildTerm}})
	f.archive = c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: wrenArchive}})
	f.fair = c.Event(catalogtest.Event{Doc: domain.EventDocument{Name: fairEvent}})
	f.quiet = c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: quietHarper,
		Status: domain.PersonStatusPrefersNoPage, StatusSourceURL: new("https://example.org/quiet/request"),
		NotesMD: new(quietNotes)}})
	f.ash = c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: ashFiddler}})
	f.morwen = c.Person(catalogtest.Person{Doc: morwenDocument(f)})
	c.SetLinkState(morwenHidden, catalogtest.LinkState{IsHidden: true})
	f.song = c.Piece(catalogtest.Piece{Doc: songDocument(f)})
	c.SetLinkState(songHiddenURL, catalogtest.LinkState{IsHidden: true})

	return f
}

func morwenDocument(f *editingFixture) domain.PersonDocument {
	return domain.PersonDocument{
		SchemaVersion: domain.DocumentSchemaVersion, Name: morwenName, Kind: domain.PersonKindCommunityBard,
		Status: domain.PersonStatusActive, LicensingStatus: domain.LicensingStatusUnknown,
		NotesMD: new("A singer."),
		Aliases: []domain.DocPersonAlias{
			{Name: morwenStage, Kind: domain.PersonAliasKindStage, IsPublic: true},
			{Name: morwenLegal, Kind: domain.PersonAliasKindLegal},
		},
		Roles:          []domain.PersonRole{domain.PersonRoleSinger},
		AffiliationIDs: []int64{f.guild.ID},
		Links: []domain.DocPersonLink{
			{DocLink: domain.DocLink{URL: morwenSiteURL, Roles: []domain.LinkRole{domain.LinkRoleWebsite}}},
			{DocLink: domain.DocLink{URL: morwenHidden, Roles: []domain.LinkRole{domain.LinkRoleWebsite}}},
		},
	}
}

func songDocument(f *editingFixture) domain.PieceDocument {
	return domain.PieceDocument{
		SchemaVersion: domain.DocumentSchemaVersion, Title: songTitle, Kind: domain.PieceKindSong,
		Conservation:    domain.DocConservation{Status: domain.ConservationStatusUnassessed},
		LicensingStatus: domain.LicensingStatusUnknown, NotesMD: new(songNotes),
		Terms: []domain.DocPieceTerm{{TermID: f.ballads.ID, IsPrimary: true}},
		Credits: []domain.DocCredit{
			{PersonID: &f.morwen.ID, Role: domain.CreditRoleLyrics, Attribution: domain.AttributionNamed,
				Certainty: domain.CertaintyCertain},
			{PersonID: &f.quiet.ID, Role: domain.CreditRoleTune, Attribution: domain.AttributionNamed,
				Certainty: domain.CertaintyCertain},
		},
		Links: []domain.DocPieceLink{
			{
				DocLink: domain.DocLink{URL: songLyricsURL, CollectionID: &f.archive.ID,
					Roles: []domain.LinkRole{domain.LinkRoleLyrics}},
				Performances: []domain.DocPerformance{{EventID: &f.fair.ID, DescriptionVerbatim: fairPerformed,
					PerformerIDs: []int64{f.morwen.ID, f.quiet.ID}}},
			},
			{DocLink: domain.DocLink{URL: songHiddenURL, Roles: []domain.LinkRole{domain.LinkRolePerformance}}},
		},
		Preferences: []domain.DocPreference{domaintest.Preference(domain.PreferenceDimensionRecording, preferencesURL, domain.PreferenceChoiceNoRecording)},
	}
}

// tick returns the fixture's clock, a minute later each time.
func (f *editingFixture) tick() time.Time {
	f.now = f.now.Add(time.Minute)
	return f.now
}

func pieceKey(slug string) *domain.EntityKey {
	return &domain.EntityKey{Type: domain.EntityTypePiece, Slug: slug}
}

func personKey(slug string) *domain.EntityKey {
	return &domain.EntityKey{Type: domain.EntityTypePerson, Slug: slug}
}

// saveDraft saves doc as a draft of the entity key (nil for a new entity), failing the test on an error.
func (f *editingFixture) saveDraft(
	t *testing.T, actor domain.Actor, key *domain.EntityKey, doc domain.AnyDocument,
) datasources.SavedDraft {
	t.Helper()
	saved, err := f.repo.SaveDraft(t.Context(), datasources.SaveDraftRequest{Actor: actor,
		EntityType: doc.EntityType(), Entity: key, Document: doc, Sent: domain.AllEditorFieldsSent,
		Summary: draftSummary, Now: f.tick()})
	require.NoError(t, err)

	return saved
}

// submit submits the draft id as SubmitRevision does, failing the test on an error.
func (f *editingFixture) submit(t *testing.T, actor domain.Actor, id int64) datasources.SubmitResult {
	t.Helper()
	result, err := f.submitChecked(t, f.submitRequest(t, actor, id))
	require.NoError(t, err)

	return result
}

// submitChecked submits req as SubmitRevision does: a submission refused for lacking the duplicate checks of the
// pending references an inline approval approves alongside is checked and submitted again.
func (f *editingFixture) submitChecked(t *testing.T, req datasources.SubmitRequest) (datasources.SubmitResult, error) {
	t.Helper()
	result, err := f.repo.SubmitDraft(t.Context(), req)
	var unchecked *datasources.UncheckedPendingReferencesError
	if !errors.As(err, &unchecked) {
		return result, err
	}
	req.PendingReferenceDuplicates = f.pendingReferenceChecks(t, unchecked)

	return f.repo.SubmitDraft(t.Context(), req)
}

// pendingReferenceChecks returns the duplicate checks SubmitRevision makes of the pending references unchecked
// names: as moderators check them, each one's own placeholder left out.
func (f *editingFixture) pendingReferenceChecks(
	t *testing.T, unchecked *datasources.UncheckedPendingReferencesError,
) map[int64]datasources.DuplicateCheck {
	t.Helper()
	checks := map[int64]datasources.DuplicateCheck{}
	for id, ref := range unchecked.References {
		matches, err := f.repo.FindDuplicates(t.Context(), ref.Candidate, true)
		require.NoError(t, err)
		checks[id] = datasources.DuplicateCheck{Candidate: ref.Candidate,
			Matches: domain.MatchesOtherThan(matches, ref.Entity)}
	}

	return checks
}

// submitRequest is the actor's submission of the draft id now, with the duplicate check SubmitRevision makes.
func (f *editingFixture) submitRequest(t *testing.T, actor domain.Actor, id int64) datasources.SubmitRequest {
	t.Helper()

	return datasources.SubmitRequest{Keys: domaintest.Keys(), Actor: actor, DraftID: id, Duplicates: f.duplicateCheck(t, actor, id),
		Now: f.tick()}
}

// duplicateCheck returns the duplicate check SubmitRevision makes of the actor's draft id: for a draft of a new
// entity the entities it may duplicate, as the actor's role compares them, and none for any other.
func (f *editingFixture) duplicateCheck(t *testing.T, actor domain.Actor, id int64) datasources.DuplicateCheck {
	t.Helper()
	draft, err := f.repo.GetDraft(t.Context(), actor, id)
	if errors.Is(err, domain.ErrNotDraft) || errors.Is(err, datasources.ErrNotFound) || err == nil &&
		draft.Entity != nil {
		return datasources.DuplicateCheck{}
	}
	require.NoError(t, err)
	candidate := domain.NewDuplicateCandidate(draft.Document)
	matches, err := f.repo.FindDuplicates(t.Context(), candidate, actor.Role == domain.RoleModerator)
	require.NoError(t, err)

	return datasources.DuplicateCheck{Candidate: candidate, Matches: matches}
}

// approvalChecks returns the duplicate checks ApproveRevision makes for an approval of the revision id: of its own
// new entity, as the moderation view shows them, and of each pending reference, its own placeholder left out. A
// revision moderators may not see has none.
func (f *editingFixture) approvalChecks(t *testing.T, id int64) map[int64]datasources.DuplicateCheck {
	t.Helper()
	checks := map[int64]datasources.DuplicateCheck{}
	revision, err := f.repo.GetModerationRevision(t.Context(), id)
	if errors.Is(err, datasources.ErrNotFound) {
		return checks
	}
	require.NoError(t, err)
	if revision.Entity != nil && revision.Entity.IsNew {
		checks[id] = datasources.DuplicateCheck{Candidate: domain.NewDuplicateCandidate(revision.Document),
			Matches: revision.Duplicates}
	}
	for _, ref := range revision.PendingReferences {
		candidate := domain.NewDuplicateCandidate(ref.Document)
		matches, err := f.repo.FindDuplicates(t.Context(), candidate, true)
		require.NoError(t, err)
		checks[ref.RevisionID] = datasources.DuplicateCheck{Candidate: candidate,
			Matches: domain.MatchesOtherThan(matches, ref.Entity)}
	}

	return checks
}

// editable returns the actor's editable document of the entity key, failing the test on an error.
func (f *editingFixture) editable(t *testing.T, actor domain.Actor, key domain.EntityKey) domain.Editable {
	t.Helper()
	editable, err := f.repo.GetEditable(t.Context(), actor, key)
	require.NoError(t, err)

	return editable
}

// editAndSubmit saves the actor's editable document of the entity key changed by change as a draft, and submits it.
func editAndSubmit[D domain.Document](
	t *testing.T, f *editingFixture, actor domain.Actor, key domain.EntityKey, change func(*D),
) datasources.SubmitResult {
	t.Helper()
	doc := f.editable(t, actor, key).Document.(D)
	change(&doc)
	draft := f.saveDraft(t, actor, &key, any(doc).(domain.AnyDocument))

	return f.submit(t, actor, draft.Draft.ID)
}

// newPending submits doc as a new entity of the actor's, returning the pending revision and its placeholder.
func (f *editingFixture) newPending(t *testing.T, actor domain.Actor, doc domain.AnyDocument) domain.MyRevision {
	t.Helper()
	draft := f.saveDraft(t, actor, nil, doc)
	result := f.submit(t, actor, draft.Draft.ID)
	revision, err := f.repo.GetMyRevision(t.Context(), actor, result.RevisionID)
	require.NoError(t, err)
	require.NotNil(t, revision.Entity)

	return revision
}

// storedRevision is the columns of a revisions row the tests check directly.
type storedRevision struct {
	status, snapshot, submitted, flags, receivedKeys string
	base, submittedBase, entityID                    sql.NullInt64
}

func (f *editingFixture) stored(t *testing.T, id int64) storedRevision {
	t.Helper()
	var r storedRevision
	var submitted, flags, keys sql.NullString
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT status, snapshot_json, submitted_snapshot_json, "+
		"flags_json, received_keys_json, base_revision_id, submitted_base_revision_id, entity_id FROM revisions "+
		"WHERE id = ?", id).Scan(&r.status, &r.snapshot, &submitted, &flags, &keys, &r.base, &r.submittedBase,
		&r.entityID))
	r.submitted, r.flags, r.receivedKeys = submitted.String, flags.String, keys.String

	return r
}

// count returns the single number query gives.
func (f *editingFixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, f.db.QueryRowContext(t.Context(), query, args...).Scan(&n))

	return n
}

func newTermDocument(name string) domain.TermDocument {
	return domain.TermDocument{SchemaVersion: domain.DocumentSchemaVersion, Facet: domain.FacetTopic, Name: name,
		DisplayName: name}
}

func newPersonDocument(name string) domain.PersonDocument {
	return domain.PersonDocument{SchemaVersion: domain.DocumentSchemaVersion, Name: name,
		Kind: domain.PersonKindCommunityBard, Status: domain.PersonStatusActive,
		LicensingStatus: domain.LicensingStatusUnknown}
}

func newPieceDocument(title string) domain.PieceDocument {
	return domain.PieceDocument{SchemaVersion: domain.DocumentSchemaVersion, Title: title, Kind: domain.PieceKindSong,
		Conservation:    domain.DocConservation{Status: domain.ConservationStatusUnassessed},
		LicensingStatus: domain.LicensingStatusUnknown}
}

func newEventDocument(name string) domain.EventDocument {
	return domain.EventDocument{SchemaVersion: domain.DocumentSchemaVersion, Name: name, Kind: domain.EventKindEvent}
}

// The statuses of revisions rows, and paths the tests' documents' problems name.
const (
	statusDraft     = "draft"
	statusPending   = "pending"
	statusApproved  = "approved"
	statusRejected  = "rejected"
	statusWithdrawn = "withdrawn"
	pathSecondTerm  = "/terms/1/term"
	shantiesAlias   = "Shanties"
)
