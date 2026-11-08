package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	linnetTitle    = "The Linnet's Song"
	linnetBook     = "https://example.org/linnet-book"
	linnetAlias    = "Linnet"
	linnetRevel    = "Linnet Revel"
	tamsinName     = "Tamsin Placeholder"
	tamsinStage    = "Tamsin"
	hushHarper     = "Hush Harper"
	linnetBookName = "Linnet Book"
)

func TestNewDuplicateCandidate(t *testing.T) {
	t.Parallel()
	person := NewDuplicateCandidate(PersonDocument{Name: tamsinName, Aliases: []DocPersonAlias{
		{Name: tamsinStage, IsPublic: true}, {Name: "Tamsin Legalname", Kind: PersonAliasKindLegal},
	}})
	assert.Equal(t, DuplicateCandidate{EntityType: EntityTypePerson, Names: []string{tamsinName, tamsinStage}},
		person, "a hidden alias the new person is given is never compared")

	piece := NewDuplicateCandidate(PieceDocument{Title: linnetTitle, Aliases: []DocPieceAlias{{Text: linnetAlias}}})
	assert.Equal(t, []string{linnetTitle, linnetAlias}, piece.Names)

	term := NewDuplicateCandidate(TermDocument{Facet: FacetMood, Name: "sad", DisplayName: "Sad"})
	assert.Equal(t, DuplicateCandidate{EntityType: EntityTypeTerm, Names: []string{"sad", "Sad"}, Facet: FacetMood},
		term)

	collection := NewDuplicateCandidate(CollectionDocument{Name: linnetBookName, URL: new(linnetBook)})
	assert.Equal(t, linnetBook, *collection.URL)

	event := NewDuplicateCandidate(EventDocument{Name: linnetRevel})
	assert.Equal(t, []string{linnetRevel}, event.Names)
}

func TestFindDuplicates(t *testing.T) {
	t.Parallel()
	song := DuplicateMatch{EntityType: EntityTypePiece, Slug: "the-linnet-s-song", Name: linnetTitle,
		Matched: DuplicateFieldTitle}
	songAlias := song
	songAlias.Matched = DuplicateFieldAlias
	hush := DuplicateMatch{EntityType: EntityTypePerson, Slug: "hush", Name: hushHarper, Matched: DuplicateFieldName}
	hushBlocked := hush
	hushBlocked.Matched, hushBlocked.Blocked = DuplicateFieldAlias, true
	hushResult := hush
	hushResult.Blocked = true
	book := DuplicateMatch{EntityType: EntityTypeCollection, CollectionID: 3, Name: linnetBookName,
		Matched: DuplicateFieldURL}

	tests := []struct {
		name      string
		candidate DuplicateCandidate
		names     []DuplicateName
		want      []DuplicateMatch
	}{
		{
			name:      "folded title",
			candidate: DuplicateCandidate{EntityType: EntityTypePiece, Names: []string{"the linnets song"}},
			names:     []DuplicateName{{Match: song, Text: "The Linnet’s Song"}},
			want:      []DuplicateMatch{song},
		},
		{
			name:      "each entity once, by its first matching name",
			candidate: DuplicateCandidate{EntityType: EntityTypePiece, Names: []string{linnetTitle, linnetAlias}},
			names:     []DuplicateName{{Match: songAlias, Text: linnetAlias}, {Match: song, Text: linnetTitle}},
			want:      []DuplicateMatch{songAlias},
		},
		{
			name:      "blocked if any match is",
			candidate: DuplicateCandidate{EntityType: EntityTypePerson, Names: []string{hushHarper, "Hush"}},
			names:     []DuplicateName{{Match: hush, Text: hushHarper}, {Match: hushBlocked, Text: "Hush"}},
			want:      []DuplicateMatch{hushResult},
		},
		{
			name:      "an exact short name only",
			candidate: DuplicateCandidate{EntityType: EntityTypePiece, Names: []string{"Ai"}},
			names:     []DuplicateName{{Match: song, Text: "Aine"}, {Match: songAlias, Text: "AI"}},
			want:      []DuplicateMatch{songAlias},
		},
		{
			name: "a collection URL, normalized",
			candidate: DuplicateCandidate{EntityType: EntityTypeCollection, Names: []string{"Other"},
				URL: new("HTTPS://Example.org/linnet-book")},
			names: []DuplicateName{{Match: book, Text: linnetBook}},
			want:  []DuplicateMatch{book},
		},
		{
			name:      "a URL is not compared with names",
			candidate: DuplicateCandidate{EntityType: EntityTypeCollection, Names: []string{linnetBook}},
			names:     []DuplicateName{{Match: book, Text: linnetBook}},
			want:      []DuplicateMatch{},
		},
		{
			name:      "no match",
			candidate: DuplicateCandidate{EntityType: EntityTypePiece, Names: []string{"Quietude"}},
			names:     []DuplicateName{{Match: song, Text: linnetTitle}},
			want:      []DuplicateMatch{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, FindDuplicates(tt.candidate, tt.names))
		})
	}
}

func TestDuplicateCandidate_Equal(t *testing.T) {
	t.Parallel()
	base := DuplicateCandidate{EntityType: EntityTypeCollection, Names: []string{linnetBookName}, URL: new(linnetBook)}
	assert.True(t, base.Equal(DuplicateCandidate{EntityType: EntityTypeCollection, Names: []string{linnetBookName},
		URL: new(linnetBook)}), "equal names and an equal URL behind another pointer")
	for name, other := range map[string]DuplicateCandidate{
		"another name":  {EntityType: EntityTypeCollection, Names: []string{"Linnet Ledger"}, URL: new(linnetBook)},
		"a name more":   {EntityType: EntityTypeCollection, Names: []string{linnetBookName, linnetAlias}, URL: new(linnetBook)},
		"another URL":   {EntityType: EntityTypeCollection, Names: []string{linnetBookName}, URL: new(linnetBook + "/2")},
		"no URL":        {EntityType: EntityTypeCollection, Names: []string{linnetBookName}},
		"another type":  {EntityType: EntityTypeEvent, Names: []string{linnetBookName}, URL: new(linnetBook)},
		"another facet": {EntityType: EntityTypeCollection, Names: []string{linnetBookName}, URL: new(linnetBook), Facet: FacetMood},
	} {
		assert.False(t, base.Equal(other), name)
		assert.False(t, other.Equal(base), name)
	}
}

func TestMatchesOtherThan(t *testing.T) {
	t.Parallel()
	own := DuplicateMatch{EntityType: EntityTypePerson, Slug: "pending-person-ash", Name: tamsinName}
	other := DuplicateMatch{EntityType: EntityTypePerson, Slug: "tamsin-placeholder", Name: tamsinName}
	piece := DuplicateMatch{EntityType: EntityTypePiece, Slug: "pending-person-ash", Name: linnetTitle}
	matches := []DuplicateMatch{own, other, piece}

	got := MatchesOtherThan(matches, RevisionEntity{Type: EntityTypePerson, ID: 1, Slug: own.Slug, IsNew: true})
	assert.Equal(t, []DuplicateMatch{other, piece}, got)
	assert.Equal(t, []DuplicateMatch{own, other, piece}, matches, "the input is not changed")

	collections := []DuplicateMatch{{EntityType: EntityTypeCollection, CollectionID: 7}, {EntityType: EntityTypeCollection,
		CollectionID: 8}}
	assert.Equal(t, collections[1:], MatchesOtherThan(collections, RevisionEntity{Type: EntityTypeCollection, ID: 7}))
	assert.NotNil(t, MatchesOtherThan(nil, RevisionEntity{Type: EntityTypePerson}), "never nil")
}
