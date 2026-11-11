package domain_test

import (
	"encoding/json"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const wrenSlug, ashSlug = "wren", "ash"

// editorSlugs are slugs of every entity the full documents reference.
func editorSlugs() domain.Slugs {
	return domain.Slugs{
		Pieces:  map[int64]string{1: "a-song", 9: "kettle-variant"},
		Persons: map[int64]string{5: wrenSlug, 6: ashSlug},
		Terms:   map[int64]string{1: "weapons", 2: "calontir", 3: "drinking-songs", 4: "merry", 11: "sca"},
		Events:  map[int64]string{7: "revel"},
	}
}

func invert(m map[int64]string) map[string]int64 {
	out := map[string]int64{}
	for id, slug := range m {
		out[slug] = id
	}

	return out
}

func editorIDs() domain.IDs {
	s := editorSlugs()

	return domain.IDs{Pieces: invert(s.Pieces), Persons: invert(s.Persons), Terms: invert(s.Terms),
		Events: invert(s.Events)}
}

func TestEditorDocuments_RoundTrip(t *testing.T) {
	s, ids := editorSlugs(), editorIDs()

	piece, problems := fullPieceDocument().ToEditor(s).FromEditor(ids)
	assert.Empty(t, problems)
	assert.Equal(t, fullPieceDocument(), piece)
	person, problems := fullPersonDocument().ToEditor(s).FromEditor(ids)
	assert.Empty(t, problems)
	assert.Equal(t, fullPersonDocument(), person)
	term, problems := fullTermDocument().ToEditor(s).FromEditor(ids)
	assert.Empty(t, problems)
	assert.Equal(t, fullTermDocument(), term)
	collection, problems := fullCollectionDocument().ToEditor(s).FromEditor(ids)
	assert.Empty(t, problems)
	assert.Equal(t, fullCollectionDocument(), collection)
	event, problems := fullEventDocument().ToEditor(s).FromEditor(ids)
	assert.Empty(t, problems)
	assert.Equal(t, fullEventDocument(), event)
}

func TestEditorPieceDocument_JSONAndReferences(t *testing.T) {
	e := fullPieceDocument().ToEditor(editorSlugs())
	data, err := json.Marshal(e.Credits[0])
	require.NoError(t, err)
	assert.JSONEq(t, `{"person_slug":"wren","role":"lyrics","attribution":"named","certainty":"probable",
		"part":"verses","note":"Verse 3 only"}`, string(data))
	assert.Equal(t, domain.SlugReferences{Pieces: []string{"kettle-variant"}, Persons: []string{wrenSlug, wrenSlug, ashSlug},
		Terms: []string{"drinking-songs", "merry"}, Events: []string{"revel"}}, e.References())
}

func TestToEditor_LeavesOutReferencesWithoutSlug(t *testing.T) {
	s := editorSlugs()
	delete(s.Persons, 5)
	delete(s.Events, 7)

	e := fullPieceDocument().ToEditor(s)
	assert.Len(t, e.Credits, 1, "the credit of person 5 is left out")
	assert.Nil(t, e.Links[0].Performances[0].EventSlug)
	assert.Equal(t, []string{ashSlug}, e.Links[0].Performances[0].PerformerSlugs)
}

func TestFromEditor_UnknownSlug(t *testing.T) {
	e := fullPieceDocument().ToEditor(editorSlugs())
	e.Terms[1].TermSlug = "no-such-term"
	e.Links[0].Performances[0].PerformerSlugs[0] = "nobody"

	_, problems := e.FromEditor(editorIDs())
	assert.Equal(t, []domain.Problem{problem("terms[1].term_slug", "unknown_reference"),
		problem("links[0].performances[0].performer_slugs[0]", "unknown_reference")}, problems)
}

func TestShowsUnlistedPerson(t *testing.T) {
	var refs domain.SnapshotReferences
	fullPieceDocument().AddReferences(&refs)

	assert.True(t, domain.ShowsUnlistedPerson(refs, map[int64]bool{6: true}), "a performer")
	assert.False(t, domain.ShowsUnlistedPerson(refs, map[int64]bool{8: true}))
}

func TestDuplicateKey(t *testing.T) {
	for _, s := range []string{"the wrens song", "The Wren's Song", "THE WREN’S  SONG!", "The Wrén´s Song"} {
		assert.Equal(t, "the wrens song", domain.DuplicateKey(s), s)
	}
	assert.Equal(t, "lukasz of the vale", domain.DuplicateKey("Łukasz-of-the Vale"))
}

func TestMatchDuplicates(t *testing.T) {
	wren := domain.DuplicateCandidate{EntityType: domain.EntityTypePerson, Slug: ptr(wrenSlug), Name: "Wren",
		Keys: []string{"wren of the vale", "wren"}}
	ash := domain.DuplicateCandidate{EntityType: domain.EntityTypePerson, Slug: ptr(ashSlug), Name: "Ash",
		Keys: []string{"ash"}}

	assert.Equal(t, []domain.DuplicateCandidate{wren},
		domain.MatchDuplicates(fullPersonDocument().DuplicateKeys(), []domain.DuplicateCandidate{wren, ash}))
}

func TestRevisionState(t *testing.T) {
	require.NoError(t, domain.CheckPending(domain.RevisionStatusPending))
	require.ErrorIs(t, domain.CheckPending(domain.RevisionStatusWithdrawn), domain.ErrRevisionNotPending)

	assert.False(t, domain.IsStale(nil, ptr[int64](3)), "a new entity's revision")
	assert.False(t, domain.IsStale(ptr[int64](3), ptr[int64](3)))
	assert.True(t, domain.IsStale(ptr[int64](3), ptr[int64](4)))
}
