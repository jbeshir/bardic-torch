package domain

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// publicVisibility is the public visibility of the fixtures' views: the editor fixtures' entities, the fixture
// person and its public aliases, and the visible links.
func publicVisibility(t *testing.T) SnapshotVisibility {
	t.Helper()
	vis := editorVisibility(t, RoleContributor).SnapshotVisibility
	vis.Persons = withEntries(vis.Persons, map[int64]PersonSummary{personID: {Slug: "morwen", Name: validPerson().Name}})
	vis.PublicAliases = map[int64]map[string]bool{personID: {secondAliasName: true, morwenTheBard: true}}
	vis.VisibleLinks = map[string]string{}
	for _, url := range []string{validPiece().Links[0].URL, songbookPageURL, validPerson().Links[0].URL, linkA} {
		normalized, err := NormalizeURL(url)
		require.NoError(t, err)
		vis.VisibleLinks[normalized] = url
	}

	return vis
}

// personView is the author's view of the submitted person document, given the projection's received keys and the
// editor visibility now.
func personView(t *testing.T, id int64, submitted PersonDocument, received ElementKeySet, vis EditorVisibility,
	public SnapshotVisibility,
) PersonSnapshot {
	t.Helper()
	doc := AuthorDocument(id, submitted, received, vis)
	view, err := NewPersonSnapshot(id, doc, AuthorVisibility(doc, received, public, vis.OwnPending, id))
	require.NoError(t, err)

	return view
}

func pieceView(submitted PieceDocument, received ElementKeySet, vis EditorVisibility,
	public SnapshotVisibility,
) PieceSnapshot {
	doc := AuthorDocument(pieceID, submitted, received, vis)

	return NewPieceSnapshot(doc, AuthorVisibility(doc, received, public, vis.OwnPending, pieceID))
}

func viewAliasNames(view PersonSnapshot) []string {
	var names []string
	for _, a := range view.Profile.Aliases {
		names = append(names, a.Name)
	}

	return names
}

func viewLinkURLs(view PieceSnapshot) []string {
	var urls []string
	for _, l := range view.Links {
		urls = append(urls, l.URL)
	}

	return urls
}

func TestAuthorView(t *testing.T) {
	t.Parallel()

	vis := editorVisibility(t, RoleContributor)
	public := publicVisibility(t)
	person, personKeys := EditableDocument(personID, fullPerson(), vis)
	piece, pieceKeys := EditableDocument(pieceID, fullPiece(), vis)
	respelled := strings.Replace(hiddenLinkURL, "example", "EXAMPLE", 1)
	withAlias := func(a DocPersonAlias) PersonDocument {
		d := person
		d.Aliases = append(slices.Clone(d.Aliases), a)

		return d
	}
	withLink := func(url string) PieceDocument {
		d := piece
		d.Links = append(slices.Clone(d.Links), DocPieceLink{DocLink: DocLink{URL: url, Roles: []LinkRole{LinkRoleLyrics}}})

		return d
	}

	t.Run("W05_W33_TypedCollidingAliasShown", func(t *testing.T) {
		t.Parallel()
		typed := DocPersonAlias{Name: strings.ToLower(legalName), Kind: PersonAliasKindNickname, IsPublic: true}
		view := personView(t, personID, withAlias(typed), personKeys, vis, public)
		assert.Equal(t, []string{secondAliasName, morwenTheBard, typed.Name}, viewAliasNames(view))
	})
	t.Run("BaseHiddenAliasNeverShown", func(t *testing.T) {
		t.Parallel()
		view := personView(t, personID, fullPerson(), personKeys, vis, public)
		assert.NotContains(t, viewAliasNames(view), legalName)
	})
	t.Run("W07_TypedLegalAliasNeverShown", func(t *testing.T) {
		t.Parallel()
		typed := DocPersonAlias{Name: otherLegalName, Kind: PersonAliasKindLegal}
		view := personView(t, personID, withAlias(typed), personKeys, vis, public)
		assert.Equal(t, []string{secondAliasName, morwenTheBard}, viewAliasNames(view))
	})
	t.Run("W10_W32_TypedHiddenURLShown", func(t *testing.T) {
		t.Parallel()
		view := pieceView(withLink(respelled), pieceKeys, vis, public)
		assert.Contains(t, viewLinkURLs(view), respelled, "as typed")
		view = pieceView(withLink(hiddenLinkURL), pieceKeys, vis, public)
		assert.Contains(t, viewLinkURLs(view), hiddenLinkURL, "whether or not it was in the base")
	})
	t.Run("W11_NoPageCreditNeverShown", func(t *testing.T) {
		t.Parallel()
		received := withEntries(pieceKeys, ElementKeySet{creditKeys(fullPiece().Credits)[1]: true})
		submitted := piece
		submitted.Credits = fullPiece().Credits
		doc := AuthorDocument(pieceID, submitted, received, vis)
		assert.Equal(t, pieceProjection().Credits, doc.Credits, "received, and withheld now")
		for _, c := range pieceView(submitted, received, vis, public).Credits {
			assert.True(t, c.Person == nil || c.Person.Slug != "rowan")
		}
	})
	t.Run("W15_NoPageNowIdentityOnly", func(t *testing.T) {
		t.Parallel()
		sub, keys := EditableDocument(refNoPagePerson, noPagePerson(), vis)
		sub.Status, sub.StatusSourceURL = PersonStatusActive, ptr("https://example.org/opt-in")
		view := personView(t, refNoPagePerson, sub, keys, vis, public)
		assert.Nil(t, view.Profile)
		assert.Equal(t, PersonStatusActive, view.Status)
	})
	t.Run("W34_ReceivedLinkHiddenSince", func(t *testing.T) {
		t.Parallel()
		now := vis
		hidden, err := NormalizeURL(songbookPageURL)
		require.NoError(t, err)
		now.HiddenLinkURLs = withEntries(vis.HiddenLinkURLs, map[string]bool{hidden: true})
		doc := AuthorDocument(pieceID, piece, pieceKeys, now)
		assert.NotContains(t, linkURLs(doc), songbookPageURL, "the pending starting point drops it too")
	})
	t.Run("W35_PendingRename", func(t *testing.T) {
		t.Parallel()
		renamed := person
		renamed.Name = "Morwen Newname"
		view := personView(t, personID, renamed, personKeys, vis, public)
		assert.Equal(t, "Morwen Newname", view.Name)
	})
	t.Run("OwnPendingTermShownOthersDropped", func(t *testing.T) {
		t.Parallel()
		submitted := piece
		submitted.Terms = fullPiece().Terms
		var slugs []string
		for _, term := range pieceView(submitted, pieceKeys, vis, public).Terms {
			slugs = append(slugs, term.Term.Slug)
		}
		assert.Equal(t, []string{"merry", "period-origin", "pending-term-1"}, slugs)
	})
	t.Run("W36_OwnPendingPersonCredited", func(t *testing.T) {
		t.Parallel()
		view := pieceView(piece, pieceKeys, vis, public)
		assert.Equal(t, "pending-person-1", view.Credits[2].Person.Slug)
	})
	t.Run("W38_PreviewEqualsViewAfterSubmit", func(t *testing.T) {
		t.Parallel()
		draft := withLink(respelled)
		preview := pieceView(draft, pieceKeys, vis, public)
		base := fullPiece()
		restored, _ := RestoreDocument(pieceID, draft, &base, pieceKeys, vis)
		assert.Equal(t, preview, pieceView(draft, pieceKeys, vis, public), "one construction")
		assert.Contains(t, viewLinkURLs(preview), respelled)
		assert.NotContains(t, viewLinkURLs(NewPieceSnapshot(restored, public)), respelled, "the public view never")
	})
	t.Run("InputsUnchanged", func(t *testing.T) {
		t.Parallel()
		_ = AuthorVisibility(withLink(respelled), pieceKeys, public, vis.OwnPending, pieceID)
		assert.Equal(t, publicVisibility(t), public)
	})
}
