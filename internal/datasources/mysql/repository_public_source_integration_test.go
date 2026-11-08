//go:build integration

package mysql_test

import (
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/domaintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The links of the hidden-source tests: a hidden link, and the same URL respelled (scheme and host case, a
// tracking parameter), which normalizes to it.
const (
	sourceHiddenURL    = "https://example.org/private-request"
	sourceRespelledURL = "HTTPS://EXAMPLE.org/private-request?utm_source=x"
	sourceVisibleURL   = "https://example.org/stated-preferences"
)

// hiddenSourceLink is the hidden link of the hidden-source tests, attached to a piece or person as a website.
var hiddenSourceLink = domain.DocLink{URL: sourceHiddenURL, Roles: []domain.LinkRole{domain.LinkRoleWebsite}}

// TestRepository_GetPublicPiece_Preferences reads a piece's preferences in dimension order, whatever order they
// were written in, without those whose source is a hidden link in any spelling or does not normalize, and the
// conservation history without a hidden source.
func TestRepository_GetPublicPiece_Preferences(t *testing.T) {
	t.Parallel()

	db := catalogtest.Writable(t)
	c := catalogtest.New(t, db)
	recording := domaintest.Preference(domain.PreferenceDimensionRecording, sourceVisibleURL,
		domain.PreferenceChoiceOralOnly)
	recording.Value.Notes = new("Ask at the event.")
	permission := domaintest.Preference(domain.PreferenceDimensionPerformancePermission, sourceVisibleURL,
		domain.PreferenceChoiceAskFirst, domain.PreferenceChoiceInformWelcome)
	c.Piece(catalogtest.Piece{Slug: "the-quiet-air", Doc: domain.PieceDocument{
		Title: "The Quiet Air",
		Links: []domain.DocPieceLink{{DocLink: hiddenSourceLink}},
		Preferences: []domain.DocPreference{
			recording,
			domaintest.Preference(domain.PreferenceDimensionAttribution, sourceRespelledURL,
				domain.PreferenceChoiceSCAName),
			permission,
			domaintest.Preference(domain.PreferenceDimensionAdaptations, sourceHiddenURL,
				domain.PreferenceChoiceAllowed),
			domaintest.Preference(domain.PreferenceDimensionCommercialUse, "https://exa mple.org/prefs",
				domain.PreferenceChoiceNotAllowed),
		},
	}, ConservationHistory: []catalogtest.ConservationEntry{
		{Status: domain.ConservationStatusEndangered, Reason: "Hidden source.", SourceURL: new(sourceRespelledURL)},
		{Status: domain.ConservationStatusLowRisk, Reason: "Visible source.", SourceURL: new(sourceVisibleURL)},
	}})
	c.SetLinkState(sourceHiddenURL, catalogtest.LinkState{IsHidden: true})

	piece, err := mysql.New(db).GetPublicPiece(t.Context(), "the-quiet-air")
	require.NoError(t, err)
	assert.Equal(t, []domain.DocPreference{permission, recording}, piece.Preferences)
	require.Len(t, piece.ConservationHistory, 2)
	assert.Nil(t, piece.ConservationHistory[0].SourceURL, "a hidden source is served as null")
	assert.Equal(t, new(sourceVisibleURL), piece.ConservationHistory[1].SourceURL)
	assert.Empty(t, piece.Links, "the hidden link itself is not shown")
}

// TestRepository_GetPublicPerson_NoPageHasNoPreferences checks that a person who prefers no page shows no
// preference, as they show no profile, while a person with a page shows theirs.
func TestRepository_GetPublicPerson_NoPageHasNoPreferences(t *testing.T) {
	t.Parallel()

	db := catalogtest.Writable(t)
	c := catalogtest.New(t, db)
	pref := domaintest.Preference(domain.PreferenceDimensionLicensing, sourceVisibleURL, domain.PreferenceChoiceCCBY)
	quiet := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Quiet Example",
		Status: domain.PersonStatusPrefersNoPage, StatusSourceURL: new(sourceVisibleURL),
		Preferences: []domain.DocPreference{pref}}})
	open := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Open Example",
		Preferences: []domain.DocPreference{pref}}})

	repo := mysql.New(db)
	person, err := repo.GetPublicPerson(t.Context(), quiet.Slug)
	require.NoError(t, err)
	assert.Nil(t, person.Profile, "no profile, so no preferences")
	assert.Equal(t, new(sourceVisibleURL), person.StatusSourceURL)

	person, err = repo.GetPublicPerson(t.Context(), open.Slug)
	require.NoError(t, err)
	require.NotNil(t, person.Profile)
	assert.Equal(t, []domain.DocPreference{pref}, person.Profile.Preferences)
}

// TestRepository_GetPublicPerson_HiddenStatusSourceIsNull checks that a status source that is a hidden link, in
// any spelling, or does not normalize, is served as null, with and without a page, and that the person's
// preference with such a source is left out.
func TestRepository_GetPublicPerson_HiddenStatusSourceIsNull(t *testing.T) {
	t.Parallel()

	db := catalogtest.Writable(t)
	c := catalogtest.New(t, db)
	tests := []struct {
		name   string
		status domain.PersonStatus
		source string
	}{
		{name: "Hidden Source Example", status: domain.PersonStatusDeceased, source: sourceHiddenURL},
		{name: "Respelled Source Example", status: domain.PersonStatusDeceased, source: sourceRespelledURL},
		{name: "Unparsed Source Example", status: domain.PersonStatusDeceased, source: "https://exa mple.org/x"},
		{name: "No Page Source Example", status: domain.PersonStatusPrefersNoPage, source: sourceRespelledURL},
	}
	refs := make([]catalogtest.Ref, 0, len(tests))
	for _, tt := range tests {
		refs = append(refs, c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: tt.name, Status: tt.status,
			StatusSourceURL: new(tt.source),
			Links:           []domain.DocPersonLink{{DocLink: hiddenSourceLink}},
			Preferences: []domain.DocPreference{domaintest.Preference(domain.PreferenceDimensionRecording,
				tt.source, domain.PreferenceChoiceOralOnly)},
		}}))
	}
	c.SetLinkState(sourceHiddenURL, catalogtest.LinkState{IsHidden: true})

	repo := mysql.New(db)
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			person, err := repo.GetPublicPerson(t.Context(), refs[i].Slug)
			require.NoError(t, err)
			assert.Nil(t, person.StatusSourceURL)
			if person.Profile != nil {
				assert.Empty(t, person.Profile.Preferences)
			}
		})
	}
}

// TestRepository_GetPublicPiece_SuggestionIgnoresHiddenAndDeadLinks checks that the conservation suggestion
// counts the distinct place types of the visible, not dead links with a content role only, so that it is no
// oracle for hidden links.
func TestRepository_GetPublicPiece_SuggestionIgnoresHiddenAndDeadLinks(t *testing.T) {
	t.Parallel()

	db := catalogtest.Writable(t)
	c := catalogtest.New(t, db)
	link := func(url string, placeType domain.LinkPlaceType, role domain.LinkRole) domain.DocPieceLink {
		return domain.DocPieceLink{DocLink: domain.DocLink{URL: url, PlaceType: &placeType,
			Roles: []domain.LinkRole{role}}}
	}
	c.Piece(catalogtest.Piece{Slug: "the-counted-song", Doc: domain.PieceDocument{
		Title: "The Counted Song",
		Links: []domain.DocPieceLink{
			link("https://example.org/counted/book", domain.LinkPlaceTypeBook, domain.LinkRoleText),
			link("https://example.org/counted/hidden-archive", domain.LinkPlaceTypeCommunityArchive,
				domain.LinkRoleLyrics),
			link("https://example.org/counted/dead-stream", domain.LinkPlaceTypeStreaming, domain.LinkRolePerformance),
			link("https://example.org/counted/blocked-video", domain.LinkPlaceTypeRecordingPlatform,
				domain.LinkRolePerformance),
			link("https://example.org/counted/site", domain.LinkPlaceTypeAuthorSite, domain.LinkRoleWebsite),
			link("https://example.org/counted/second-book", domain.LinkPlaceTypeBook, domain.LinkRoleTranslation),
		},
	}})
	c.SetLinkState("https://example.org/counted/hidden-archive", catalogtest.LinkState{IsHidden: true,
		Liveness: domain.LivenessOK})
	c.SetLinkState("https://example.org/counted/dead-stream", catalogtest.LinkState{Liveness: domain.LivenessDead})
	c.SetLinkState("https://example.org/counted/blocked-video", catalogtest.LinkState{Liveness: domain.LivenessBlocked})

	piece, err := mysql.New(db).GetPublicPiece(t.Context(), "the-counted-song")
	require.NoError(t, err)
	assert.Equal(t, &domain.ConservationSuggestion{Status: domain.ConservationStatusEndangered, PlaceCount: 2,
		Places: []domain.LinkPlaceType{domain.LinkPlaceTypeRecordingPlatform, domain.LinkPlaceTypeBook}},
		piece.ConservationSuggestion)
}

// TestModerationRevision_ConservationSuggestionFromRevisionLinks checks that a moderator's view of a piece
// revision suggests a status from the revision document's links by their current state: a hidden or dead stored
// link is no place, and a link not stored yet counts as live. A revision of another entity type has none.
func TestModerationRevision_ConservationSuggestionFromRevisionLinks(t *testing.T) {
	t.Parallel()

	f := newEditingFixture(t)
	doc := songDocument(f)
	placed := func(url string, placeType domain.LinkPlaceType) domain.DocPieceLink {
		return domain.DocPieceLink{DocLink: domain.DocLink{URL: url, PlaceType: &placeType,
			Roles: []domain.LinkRole{domain.LinkRoleLyrics}}}
	}
	doc.Links = []domain.DocPieceLink{
		placed(songHiddenURL, domain.LinkPlaceTypeCommunityArchive),
		placed("https://example.org/revision/unstored", domain.LinkPlaceTypeVendor),
		placed("https://example.org/revision/unstored-book", domain.LinkPlaceTypeBook),
	}
	revision := f.moderationRevision(t, f.c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece,
		EntityID: f.song.ID, Snapshot: doc}))
	assert.Equal(t, &domain.ConservationSuggestion{Status: domain.ConservationStatusEndangered, PlaceCount: 2,
		Places: []domain.LinkPlaceType{domain.LinkPlaceTypeBook, domain.LinkPlaceTypeVendor}},
		revision.ConservationSuggestion)

	doc.Conservation.Status = domain.ConservationStatusRestricted
	doc.Conservation.SourceURL = new("https://example.org/restricted")
	restricted := f.moderationRevision(t, f.c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece,
		EntityID: f.song.ID, Snapshot: doc}))
	assert.Nil(t, restricted.ConservationSuggestion, "restricted overrides everything")

	person := f.moderationRevision(t, f.c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePerson,
		EntityID: f.morwen.ID, Snapshot: morwenDocument(f)}))
	assert.Nil(t, person.ConservationSuggestion)
}
