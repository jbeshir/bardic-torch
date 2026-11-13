package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testParseEnum requires parse to accept each of valid and to reject its upper-case form, the value with
// surrounding space, the empty string and "unknown-value".
func testParseEnum[T ~string](t *testing.T, parse func(string) (T, error), valid ...T) {
	t.Helper()

	for _, want := range valid {
		t.Run(string(want), func(t *testing.T) {
			t.Parallel()

			got, err := parse(string(want))
			require.NoError(t, err)
			assert.Equal(t, want, got)

			for _, invalid := range []string{strings.ToUpper(string(want)), " " + string(want), string(want) + " "} {
				_, err := parse(invalid)
				assert.Error(t, err, "[%s]", invalid)
			}
		})
	}

	for _, invalid := range []string{"", "unknown-value"} {
		_, err := parse(invalid)
		assert.Error(t, err, "[%s]", invalid)
	}
}

func TestParseFacet(t *testing.T) {
	t.Parallel()

	testParseEnum(t, ParseFacet, FacetMood, FacetTopic, FacetCulture, FacetLanguage, FacetLegendaria, FacetOrigin,
		FacetOther, FacetAffiliation)

	_, err := ParseFacet("kind")
	assert.Error(t, err, "kinds are not a term facet")
}

func TestParseCollectionKind(t *testing.T) {
	t.Parallel()

	testParseEnum(t, ParseCollectionKind, CollectionKindIndexOrLinkList, CollectionKindSongbookPDF,
		CollectionKindSongbookWebsite, CollectionKindWebsite, CollectionKindWikiCategory, CollectionKindWiki,
		CollectionKindYouTubeChannel, CollectionKindYouTubePlaylist, CollectionKindGoogleDocSongbook,
		CollectionKindBlog, CollectionKindArchivedSongbook, CollectionKindHostedDocument)
}

func TestParsePersonKind(t *testing.T) {
	t.Parallel()

	testParseEnum(t, ParsePersonKind, PersonKindCommunityBard, PersonKindHistorical, PersonKindGroup)
}

func TestParsePersonRole(t *testing.T) {
	t.Parallel()

	testParseEnum(t, ParsePersonRole, PersonRoleSinger, PersonRoleSongwriter, PersonRoleMusician,
		PersonRoleInstrumentalist, PersonRoleStoryteller, PersonRolePoet, PersonRoleJester, PersonRoleDancer)
}

func TestParsePieceKind(t *testing.T) {
	t.Parallel()

	testParseEnum(t, ParsePieceKind, PieceKindSong, PieceKindPoem, PieceKindStory, PieceKindDanceMusic,
		PieceKindInstrumental)
}

func TestParseConservationStatus(t *testing.T) {
	t.Parallel()

	testParseEnum(t, ParseConservationStatus, ConservationStatusUnassessed, ConservationStatusLowRisk,
		ConservationStatusEndangered, ConservationStatusCriticallyEndangered, ConservationStatusRestricted)
}

func TestParseNeedsWorkReason(t *testing.T) {
	t.Parallel()

	testParseEnum(t, ParseNeedsWorkReason, NeedsWorkReasonStub, NeedsWorkReasonUnassessed,
		NeedsWorkReasonMissingLicensing, NeedsWorkReasonDeadLinks)
}

func TestParsePieceSort(t *testing.T) {
	t.Parallel()

	testParseEnum(t, ParsePieceSort, PieceSortTitle, PieceSortRelevance)

	_, err := ParsePieceSort(string(PersonSortName))
	assert.Error(t, err, "pieces sort by title, not name")
}

func TestParsePersonSort(t *testing.T) {
	t.Parallel()

	testParseEnum(t, ParsePersonSort, PersonSortName, PersonSortRelevance)

	_, err := ParsePersonSort(string(PieceSortTitle))
	assert.Error(t, err, "persons sort by name, not title")
}
