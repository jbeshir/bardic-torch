package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// place is a live link of place type placeType with role.
func place(placeType LinkPlaceType, role LinkRole) SuggestionLink {
	return SuggestionLink{PlaceType: &placeType, Roles: []LinkRole{role}, Liveness: LivenessOK}
}

func TestSuggestConservation(t *testing.T) {
	t.Parallel()

	suggestion := func(status ConservationStatus, places ...LinkPlaceType) *ConservationSuggestion {
		if places == nil {
			places = []LinkPlaceType{}
		}
		return &ConservationSuggestion{Status: status, PlaceCount: len(places), Places: places}
	}
	dead := place(LinkPlaceTypeStreaming, LinkRolePerformance)
	dead.Liveness = LivenessDead
	unknown := place(LinkPlaceTypeBook, LinkRoleText)
	unknown.Liveness = LivenessUnknown
	blocked := place(LinkPlaceTypeVendor, LinkRoleSheetMusic)
	blocked.Liveness = LivenessBlocked
	tests := []struct {
		name   string
		status ConservationStatus
		links  []SuggestionLink
		want   *ConservationSuggestion
	}{
		{name: "no places is critically endangered", status: ConservationStatusUnassessed,
			want: suggestion(ConservationStatusCriticallyEndangered)},
		{name: "one place is endangered", status: ConservationStatusLowRisk,
			links: []SuggestionLink{place(LinkPlaceTypeAuthorSite, LinkRoleLyrics)},
			want:  suggestion(ConservationStatusEndangered, LinkPlaceTypeAuthorSite)},
		{name: "two places are endangered", status: ConservationStatusUnassessed,
			links: []SuggestionLink{place(LinkPlaceTypeBook, LinkRoleText), place(LinkPlaceTypeStreaming,
				LinkRolePerformance)},
			want: suggestion(ConservationStatusEndangered, LinkPlaceTypeStreaming, LinkPlaceTypeBook)},
		{name: "exactly three places are endangered", status: ConservationStatusUnassessed,
			links: []SuggestionLink{place(LinkPlaceTypeBook, LinkRoleText), place(LinkPlaceTypeStreaming,
				LinkRolePerformance), place(LinkPlaceTypeOralTradition, LinkRoleOfficial)},
			want: suggestion(ConservationStatusEndangered, LinkPlaceTypeStreaming, LinkPlaceTypeBook,
				LinkPlaceTypeOralTradition)},
		{name: "four places are low risk", status: ConservationStatusCriticallyEndangered,
			links: []SuggestionLink{place(LinkPlaceTypeBook, LinkRoleText), place(LinkPlaceTypeStreaming,
				LinkRolePerformance), place(LinkPlaceTypeOralTradition, LinkRoleOfficial),
				place(LinkPlaceTypeCommunityArchive, LinkRoleDanceInstructions)},
			want: suggestion(ConservationStatusLowRisk, LinkPlaceTypeStreaming, LinkPlaceTypeCommunityArchive,
				LinkPlaceTypeBook, LinkPlaceTypeOralTradition)},
		{name: "a place type twice counts once", status: ConservationStatusUnassessed,
			links: []SuggestionLink{place(LinkPlaceTypeBook, LinkRoleText), place(LinkPlaceTypeBook,
				LinkRoleTranslation), place(LinkPlaceTypeBook, LinkRoleTextSource)},
			want: suggestion(ConservationStatusEndangered, LinkPlaceTypeBook)},
		{name: "a dead link is no place", status: ConservationStatusUnassessed, links: []SuggestionLink{dead},
			want: suggestion(ConservationStatusCriticallyEndangered)},
		{name: "unknown and blocked links count as live", status: ConservationStatusUnassessed,
			links: []SuggestionLink{unknown, blocked},
			want:  suggestion(ConservationStatusEndangered, LinkPlaceTypeBook, LinkPlaceTypeVendor)},
		{name: "a link without a place type is no place", status: ConservationStatusUnassessed,
			links: []SuggestionLink{{Roles: []LinkRole{LinkRoleLyrics}, Liveness: LivenessOK}},
			want:  suggestion(ConservationStatusCriticallyEndangered)},
		{name: "links with only licensing source, website, external profile or other roles are no place",
			status: ConservationStatusUnassessed, links: []SuggestionLink{
				place(LinkPlaceTypeAuthorSite, LinkRoleLicensingSource), place(LinkPlaceTypeAuthorContact,
					LinkRoleWebsite), place(LinkPlaceTypeOther, LinkRoleOther),
				place(LinkPlaceTypeRecordingPlatform, LinkRoleExternalProfile),
			},
			want: suggestion(ConservationStatusCriticallyEndangered)},
		{name: "a content role among others makes a place", status: ConservationStatusUnassessed,
			links: []SuggestionLink{{PlaceType: new(LinkPlaceTypeAuthorSite),
				Roles: []LinkRole{LinkRoleWebsite, LinkRoleLyrics}, Liveness: LivenessOK}},
			want: suggestion(ConservationStatusEndangered, LinkPlaceTypeAuthorSite)},
		{name: "restricted has none", status: ConservationStatusRestricted,
			links: []SuggestionLink{place(LinkPlaceTypeBook, LinkRoleText)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, SuggestConservation(tt.status, tt.links))
		})
	}
}

func TestDocumentSuggestionLinks(t *testing.T) {
	t.Parallel()

	const hidden, stored, unstored = "https://example.org/hidden", "https://example.org/stored",
		"https://example.org/unstored"
	link := func(url string) DocPieceLink {
		return DocPieceLink{DocLink: DocLink{URL: url, PlaceType: new(LinkPlaceTypeBook),
			Roles: []LinkRole{LinkRoleText}}}
	}
	d := validPiece()
	d.Links = []DocPieceLink{link(hidden), link(stored), link(unstored)}
	got := DocumentSuggestionLinks(d, []LinkState{
		{URL: hidden, LinkID: new(int64(1)), IsHidden: true, Liveness: new(LivenessOK)},
		{URL: stored, LinkID: new(int64(2)), Liveness: new(LivenessDead)},
		{URL: unstored},
	})
	assert.Equal(t, []SuggestionLink{
		{PlaceType: new(LinkPlaceTypeBook), Roles: []LinkRole{LinkRoleText}, Liveness: LivenessDead},
		{PlaceType: new(LinkPlaceTypeBook), Roles: []LinkRole{LinkRoleText}, Liveness: LivenessUnknown},
	}, got, "the hidden link is left out, and a link not stored yet is of unknown liveness")
}
