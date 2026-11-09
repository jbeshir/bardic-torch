//go:build integration

package app

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/domaintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unknownSourceURL is a source URL no link has, the control of typedSourceOracle.
const unknownSourceURL = "https://example.org/unknown-source"

// typedSourceOracle is the typed-source row of the revision leak matrix (docs/PLAN.md §5.1, "Validation is never
// an oracle for hidden data"): A's draft whose sources are typedLinkURL, the hidden link in another spelling, gets
// the same response and preview, byte for byte once the typed URL and IDs are masked, as the same draft with an
// unknown URL. The piece's draft types a preference and the conservation source; wren's draft edits the received
// recording preference's source to the URL (received, then edited) and types the status source. The drafts are
// discarded afterwards.
func (w revisionLeakWorld) typedSourceOracle(t *testing.T, fx revisionLeakFixture) {
	t.Helper()

	cases := []struct {
		name       string
		collection string
		slug       string
		entityType domain.EntityType
		// sourced returns the editable document with every source the case sets set to url.
		sourced func(t *testing.T, document jsonObject, url string) jsonObject
	}{
		{name: "typed preference and conservation source", collection: "pieces", slug: fx.piece.Slug,
			entityType: domain.EntityTypePiece, sourced: typedPieceSources},
		{name: "received preference source edited, typed status source", collection: "persons", slug: fx.wren.Slug,
			entityType: domain.EntityTypePerson, sourced: editedPersonSources},
	}
	for _, c := range cases {
		a := w.viewers[viewerAuthor]
		start := sendOK[jsonObject](t, w.e, http.MethodGet, editablePath(c.collection, c.slug), a, "", http.StatusOK)
		draftBodies := func(url string) []string {
			body := draftRequest(t, c.entityType, entitySlug(c.slug), start["base_revision_id"],
				c.sourced(t, editedDocument(t, start, nil), url), "Add the sources.")
			saved := w.e.send(t, http.MethodPost, draftsPath, a, body)
			require.Equal(t, http.StatusCreated, saved.Code, saved.Body.String())
			var draft jsonObject
			require.NoError(t, json.Unmarshal(saved.Body.Bytes(), &draft))
			id := objectID(t, draft, "id")
			read := w.e.send(t, http.MethodGet, idPath("/v1/drafts/", id), a, "")
			require.Equal(t, http.StatusOK, read.Code, read.Body.String())
			discarded := w.e.send(t, http.MethodDelete, idPath("/v1/drafts/", id), a, "")
			require.Equal(t, http.StatusNoContent, discarded.Code, discarded.Body.String())

			return []string{saved.Body.String(), read.Body.String()}
		}

		hidden, unknown := draftBodies(typedLinkURL), draftBodies(unknownSourceURL)
		for i := range hidden {
			assert.Equal(t, maskedBody(t, unknown[i], unknownSourceURL), maskedBody(t, hidden[i], typedLinkURL),
				"%s: a draft whose sources are a hidden link's is told apart from one with an unknown URL", c.name)
			assert.Contains(t, hidden[i], `"source_url":"`+typedLinkURL+`"`, "%s: the typed source is not shown",
				c.name)
		}
	}
}

// typedPieceSources is the piece's editable document with a recording preference and the conservation source
// typed, both url.
func typedPieceSources(t *testing.T, document jsonObject, url string) jsonObject {
	t.Helper()

	prefs, ok := document["preferences"].([]any)
	require.True(t, ok, "the document has preferences")
	for _, p := range prefs {
		pref, ok := p.(jsonObject)
		require.True(t, ok, "a preference is an object")
		require.NotEqual(t, string(domain.PreferenceDimensionRecording), pref["dimension"],
			"the piece already has a recording preference")
	}
	var typed jsonObject
	require.NoError(t, json.Unmarshal([]byte(encodeJSON(t, domaintest.Preference(domain.PreferenceDimensionRecording,
		url, domain.PreferenceChoiceAskBeforeRecording))), &typed))
	conservation, ok := document["conservation"].(jsonObject)
	require.True(t, ok, "the document has a conservation")
	require.Nil(t, conservation["source_url"], "the hidden conservation source is withheld from A")
	edited := copyObject(conservation)
	edited["source_url"] = url

	return editedDocument(t, jsonObject{documentField: document},
		jsonObject{"preferences": append(append([]any{}, prefs...), typed), "conservation": edited})
}

// editedPersonSources is wren's editable document with the received recording preference's source edited to url,
// and the status source, withheld from A, typed as url.
func editedPersonSources(t *testing.T, document jsonObject, url string) jsonObject {
	t.Helper()

	prefs, ok := document["preferences"].([]any)
	require.True(t, ok, "the document has preferences")
	require.Nil(t, document["status_source_url"], "the hidden status source is withheld from A")
	edited := make([]any, 0, len(prefs))
	found := false
	for _, p := range prefs {
		object, ok := p.(jsonObject)
		require.True(t, ok, "a preference is an object")
		pref := copyObject(object)
		if pref["dimension"] == string(domain.PreferenceDimensionRecording) {
			require.Equal(t, wrenPreferencesURL, pref["source_url"], "A received wren's recording preference")
			pref["source_url"] = url
			found = true
		}
		edited = append(edited, pref)
	}
	require.True(t, found, "A received wren's recording preference")

	return editedDocument(t, jsonObject{documentField: document},
		jsonObject{"preferences": edited, "status_source_url": url})
}

// copyObject is a shallow copy of o.
func copyObject(o jsonObject) jsonObject {
	out := make(jsonObject, len(o))
	for k, v := range o {
		out[k] = v
	}

	return out
}
