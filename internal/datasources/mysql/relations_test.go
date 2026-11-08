package mysql

import (
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The relation types and pieces of the relation tests.
const (
	linnetSlug        = "linnet"
	kettleSlug        = "kettle"
	variantOf         = "variant_of"
	sameAs            = "same_as"
	sameTextOtherForm = "same_text_other_form"
)

type relationRef struct {
	relationType string
	slug         string
}

func TestPieceRelations_SymmetricPairListedOnce(t *testing.T) {
	t.Parallel()

	row := func(r relationRef) queries.ListPublicPieceRelationsToRow {
		return queries.ListPublicPieceRelationsToRow{
			Type: queries.PieceRelationsType(r.relationType), Slug: r.slug, Title: r.slug,
			Kind: queries.PiecesKindSong, ConservationStatus: queries.PiecesConservationStatusUnassessed,
		}
	}
	tests := []struct {
		name     string
		outgoing []relationRef
		incoming []relationRef
		want     []domain.PieceRelation
	}{
		{name: "a symmetric relation stored both ways",
			outgoing: []relationRef{{sameTextOtherForm, kettleSlug}},
			incoming: []relationRef{{sameTextOtherForm, kettleSlug}},
			want:     []domain.PieceRelation{pieceRelation(sameTextOtherForm, kettleSlug, domain.RelationDirectionOutgoing)}},
		{name: "a directed relation stored both ways",
			outgoing: []relationRef{{variantOf, kettleSlug}},
			incoming: []relationRef{{variantOf, kettleSlug}},
			want: []domain.PieceRelation{
				pieceRelation(variantOf, kettleSlug, domain.RelationDirectionOutgoing),
				pieceRelation(variantOf, kettleSlug, domain.RelationDirectionIncoming),
			}},
		{name: "a symmetric relation stored one way, outgoing",
			outgoing: []relationRef{{sameTextOtherForm, kettleSlug}},
			want:     []domain.PieceRelation{pieceRelation(sameTextOtherForm, kettleSlug, domain.RelationDirectionOutgoing)}},
		{name: "a symmetric relation stored one way, incoming",
			incoming: []relationRef{{sameTextOtherForm, kettleSlug}},
			want:     []domain.PieceRelation{pieceRelation(sameTextOtherForm, kettleSlug, domain.RelationDirectionIncoming)}},
		{name: "a symmetric relation to another piece than the outgoing one",
			outgoing: []relationRef{{sameTextOtherForm, kettleSlug}},
			incoming: []relationRef{{sameTextOtherForm, "pot"}},
			want: []domain.PieceRelation{
				pieceRelation(sameTextOtherForm, kettleSlug, domain.RelationDirectionOutgoing),
				pieceRelation(sameTextOtherForm, "pot", domain.RelationDirectionIncoming),
			}},
		{name: "another type to the same piece is another relation",
			outgoing: []relationRef{{variantOf, kettleSlug}},
			incoming: []relationRef{{sameTextOtherForm, kettleSlug}},
			want: []domain.PieceRelation{
				pieceRelation(variantOf, kettleSlug, domain.RelationDirectionOutgoing),
				pieceRelation(sameTextOtherForm, kettleSlug, domain.RelationDirectionIncoming),
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &catalogueReader{}
			var to []queries.ListPublicPieceRelationsToRow
			for _, r := range tt.outgoing {
				to = append(to, row(r))
			}
			var from []queries.ListPublicPieceRelationsFromRow
			for _, r := range tt.incoming {
				from = append(from, queries.ListPublicPieceRelationsFromRow(row(r)))
			}

			got := c.pieceRelations(pieceDetails{}, to, from)

			require.NoError(t, c.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func pieceRelation(relationType, slug string, direction domain.RelationDirection) domain.PieceRelation {
	return domain.PieceRelation{
		Type: domain.PieceRelationType(relationType), Direction: direction,
		Piece: domain.PieceSummary{Slug: slug, Title: slug, Kind: domain.PieceKindSong,
			ConservationStatus: domain.ConservationStatusUnassessed},
	}
}

func TestPersonRelations_SymmetricPairListedOnce(t *testing.T) {
	t.Parallel()

	row := func(r relationRef) queries.ListPublicPersonRelationsToRow {
		return queries.ListPublicPersonRelationsToRow{
			Type: queries.PersonRelationsType(r.relationType), Slug: r.slug, Name: r.slug,
			Kind: queries.PersonsKindCommunityBard,
		}
	}
	tests := []struct {
		name     string
		outgoing []relationRef
		incoming []relationRef
		want     []domain.PersonRelation
	}{
		{name: "a symmetric relation stored both ways",
			outgoing: []relationRef{{sameAs, linnetSlug}},
			incoming: []relationRef{{sameAs, linnetSlug}},
			want:     []domain.PersonRelation{personRelation(sameAs, linnetSlug, domain.RelationDirectionOutgoing)}},
		{name: "a directed relation stored both ways",
			outgoing: []relationRef{{"member_of", "bardsguild"}},
			incoming: []relationRef{{"member_of", "bardsguild"}},
			want: []domain.PersonRelation{
				personRelation("member_of", "bardsguild", domain.RelationDirectionOutgoing),
				personRelation("member_of", "bardsguild", domain.RelationDirectionIncoming),
			}},
		{name: "a symmetric relation stored one way, incoming",
			incoming: []relationRef{{sameAs, linnetSlug}},
			want:     []domain.PersonRelation{personRelation(sameAs, linnetSlug, domain.RelationDirectionIncoming)}},
		{name: "a symmetric relation to another person than the outgoing one",
			outgoing: []relationRef{{sameAs, linnetSlug}},
			incoming: []relationRef{{sameAs, "merlin"}},
			want: []domain.PersonRelation{
				personRelation(sameAs, linnetSlug, domain.RelationDirectionOutgoing),
				personRelation(sameAs, "merlin", domain.RelationDirectionIncoming),
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &catalogueReader{}
			var to []queries.ListPublicPersonRelationsToRow
			for _, r := range tt.outgoing {
				to = append(to, row(r))
			}
			var from []queries.ListPublicPersonRelationsFromRow
			for _, r := range tt.incoming {
				from = append(from, queries.ListPublicPersonRelationsFromRow(row(r)))
			}

			got := c.personRelations(to, from)

			require.NoError(t, c.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func personRelation(relationType, slug string, direction domain.RelationDirection) domain.PersonRelation {
	return domain.PersonRelation{
		Type: domain.PersonRelationType(relationType), Direction: direction,
		Person: domain.PersonSummary{Slug: slug, Name: slug, Kind: domain.PersonKindCommunityBard},
	}
}
