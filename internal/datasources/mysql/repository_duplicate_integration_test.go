//go:build integration

package mysql_test

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hushedPiper is the name of a person who prefers no page, with a public and a hidden alias.
const hushedPiper = "Hushed Piper"

func TestRepository_FindDuplicates(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)
	lucja := f.c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Łucja Placeholder"}})
	bjorn := f.c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Bjørn Placeholder"}})
	aelf := f.c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Ælfwyn Straße"}})
	ai := f.c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Ai",
		Aliases: []domain.DocPieceAlias{{Text: "The Lark's Air", Kind: domain.PieceAliasKindNickname}}}})
	collection := f.c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: "Songbook",
		URL: new("https://example.org/songbook")}})
	pending := f.newPending(t, actorWren, newPieceDocument("The Unsung Wren"))
	f.c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Unapproved Wren"}, Unapproved: true})
	hushed := f.c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: hushedPiper,
		Status: domain.PersonStatusPrefersNoPage, StatusSourceURL: new("https://example.org/hushed/request"),
		Aliases: []domain.DocPersonAlias{
			{Name: "Piper of the Fen", Kind: domain.PersonAliasKindStage, IsPublic: true},
			{Name: "Hushed Legalname", Kind: domain.PersonAliasKindLegal},
		}}})

	person := func(name string) domain.DuplicateCandidate {
		return domain.NewDuplicateCandidate(newPersonDocument(name))
	}
	piece := func(title string) domain.DuplicateCandidate {
		return domain.NewDuplicateCandidate(newPieceDocument(title))
	}
	match := func(entityType domain.EntityType, slug, name string, field domain.DuplicateField) domain.DuplicateMatch {
		return domain.DuplicateMatch{EntityType: entityType, Slug: slug, Name: name, Matched: field}
	}
	tests := []struct {
		name          string
		candidate     domain.DuplicateCandidate
		includeHidden bool
		want          []domain.DuplicateMatch
	}{
		{"a folded title (W26)", piece("The Wrens Song"), false,
			[]domain.DuplicateMatch{match(domain.EntityTypePiece, f.song.Slug, songTitle, domain.DuplicateFieldTitle)}},
		{"an alias", piece("the larks air"), false,
			[]domain.DuplicateMatch{match(domain.EntityTypePiece, ai.Slug, "Ai", domain.DuplicateFieldAlias)}},
		{"a short title, exactly", piece("AI"), false,
			[]domain.DuplicateMatch{match(domain.EntityTypePiece, ai.Slug, "Ai", domain.DuplicateFieldTitle)}},
		{"no partial match", piece("Aine"), false, []domain.DuplicateMatch{}},
		{"ł", person("Lucja Placeholder"), false, []domain.DuplicateMatch{
			match(domain.EntityTypePerson, lucja.Slug, "Łucja Placeholder", domain.DuplicateFieldName)}},
		{"ø", person("Bjorn Placeholder"), false, []domain.DuplicateMatch{
			match(domain.EntityTypePerson, bjorn.Slug, "Bjørn Placeholder", domain.DuplicateFieldName)}},
		{"æ and ß", person("aelfwyn strasse"), false, []domain.DuplicateMatch{
			match(domain.EntityTypePerson, aelf.Slug, "Ælfwyn Straße", domain.DuplicateFieldName)}},
		{"a public alias", person("morwen"), false, []domain.DuplicateMatch{
			match(domain.EntityTypePerson, f.morwen.Slug, morwenName, domain.DuplicateFieldAlias)}},
		{"never a hidden alias for a contributor", person(morwenLegal), false, []domain.DuplicateMatch{}},
		{"a hidden alias for a moderator", person(morwenLegal), true, []domain.DuplicateMatch{
			match(domain.EntityTypePerson, f.morwen.Slug, morwenName, domain.DuplicateFieldAlias)}},
		{"a person who prefers no page blocks", person(quietHarper), false, []domain.DuplicateMatch{{
			EntityType: domain.EntityTypePerson, Slug: f.quiet.Slug, Name: quietHarper,
			Matched: domain.DuplicateFieldName, Blocked: true}}},
		{"never an alias of a person who prefers no page for a contributor", person("piper of the fen"), false,
			[]domain.DuplicateMatch{}},
		{"nor their hidden alias", person("Hushed Legalname"), false, []domain.DuplicateMatch{}},
		{"their name for a contributor", person("hushed piper"), false, []domain.DuplicateMatch{{
			EntityType: domain.EntityTypePerson, Slug: hushed.Slug, Name: hushedPiper,
			Matched: domain.DuplicateFieldName, Blocked: true}}},
		{"their public alias for a moderator", person("piper of the fen"), true, []domain.DuplicateMatch{{
			EntityType: domain.EntityTypePerson, Slug: hushed.Slug, Name: hushedPiper,
			Matched: domain.DuplicateFieldAlias, Blocked: true}}},
		{"their hidden alias for a moderator", person("Hushed Legalname"), true, []domain.DuplicateMatch{{
			EntityType: domain.EntityTypePerson, Slug: hushed.Slug, Name: hushedPiper,
			Matched: domain.DuplicateFieldAlias, Blocked: true}}},
		{"no unapproved entity", person("Unapproved Wren"), true, []domain.DuplicateMatch{}},
		{"no pending new entity for a contributor", piece("The Unsung Wren"), false, []domain.DuplicateMatch{}},
		{"pending new entities for a moderator", piece("the unsung wren"), true, []domain.DuplicateMatch{
			match(domain.EntityTypePiece, pending.Entity.Slug, "The Unsung Wren", domain.DuplicateFieldTitle)}},
		{"a term of the same facet", domain.NewDuplicateCandidate(newTermDocument("BALLADS")), false,
			[]domain.DuplicateMatch{match(domain.EntityTypeTerm, f.ballads.Slug, balladsTerm, domain.DuplicateFieldName)}},
		{"not of another facet", domain.NewDuplicateCandidate(domain.TermDocument{Facet: domain.FacetMood,
			Name: balladsTerm, DisplayName: balladsTerm}), false, []domain.DuplicateMatch{}},
		{"an event", domain.NewDuplicateCandidate(newEventDocument("midsummer fair")), false,
			[]domain.DuplicateMatch{match(domain.EntityTypeEvent, f.fair.Slug, fairEvent, domain.DuplicateFieldName)}},
		{"a collection URL", domain.NewDuplicateCandidate(domain.CollectionDocument{Name: "Another",
			URL: new("https://EXAMPLE.org/songbook")}), false, []domain.DuplicateMatch{{
			EntityType: domain.EntityTypeCollection, CollectionID: collection.ID, Name: "Songbook",
			Matched: domain.DuplicateFieldURL}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := f.repo.FindDuplicates(t.Context(), tt.candidate, tt.includeHidden)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRepository_ListHiddenAliasNames(t *testing.T) {
	t.Parallel()
	f := newEditingFixture(t)

	names, err := f.repo.ListHiddenAliasNames(t.Context())

	require.NoError(t, err)
	assert.Equal(t, []string{morwenLegal}, names)
}

// TestRepository_FindDuplicates_NameLimit checks that a name set reaching the duplicate check's limit is noticed:
// a warning is logged. It sets the default logger, so it does not run in parallel.
func TestRepository_FindDuplicates_NameLimit(t *testing.T) {
	f := newEditingFixture(t)
	digits := "(SELECT 0 AS d UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4 " +
		"UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9)"
	f.c.Exec("INSERT INTO pieces (slug, title, sort_title, sort_letter, kind, current_revision_id) "+
		"SELECT CONCAT('bulk-', n), CONCAT('Bulk ', n), CONCAT('Bulk ', n), 'B', 'song', ? FROM ("+
		"SELECT a.d + 10 * b.d + 100 * c.d + 1000 * e.d + 10000 * g.d AS n FROM "+digits+" a, "+digits+" b, "+
		digits+" c, "+digits+" e, "+digits+" g) numbers WHERE n < 20001", f.song.RevisionID)
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	_, err := f.repo.FindDuplicates(t.Context(), domain.NewDuplicateCandidate(newPieceDocument(unsungTitle)), false)

	require.NoError(t, err)
	assert.Contains(t, logged.String(), "duplicate check reached its name limit")
	assert.Contains(t, logged.String(), "piece titles")
}
