package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShownLicenceVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		licence *Licence
		version *LicenceVersion
		want    *LicenceVersion
	}{
		{name: "creative commons", licence: new(LicenceCCBYNC), version: new(LicenceVersion25),
			want: new(LicenceVersion25)},
		{name: "creative commons without a version", licence: new(LicenceCCBY)},
		{name: "stale version beside public domain", licence: new(LicencePD), version: new(LicenceVersion40)},
		{name: "stale version beside all rights reserved", licence: new(LicenceARR), version: new(LicenceVersion40)},
		{name: "stale version beside another licence", licence: new(LicenceOther), version: new(LicenceVersion40)},
		{name: "stale version with no licence", version: new(LicenceVersion40)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ShownLicenceVersion(tt.licence, tt.version))
		})
	}
}

func TestLicence_IsCreativeCommons(t *testing.T) {
	t.Parallel()

	for _, l := range licenceValues {
		want := l != LicencePD && l != LicenceARR && l != LicenceOther
		assert.Equal(t, want, l.IsCreativeCommons(), l)
	}
}

func TestEditorFieldsSent_ReceivedKeysLicenceVersion(t *testing.T) {
	t.Parallel()

	received := ElementKeySet{licenceVersionKey: true, linkElementKey(songbookPageURL): true}
	assert.Equal(t, received, AllEditorFieldsSent.ReceivedKeys(received))
	assert.Equal(t, ElementKeySet{linkElementKey(songbookPageURL): true},
		EditorFieldsSent{Preferences: true}.ReceivedKeys(received), "only the licence version's key is given up")
}

func TestEditablePiece_ShowsLicenceVersion(t *testing.T) {
	t.Parallel()

	base := licensedPiece(new(LicenceCCBY), new(LicenceVersion40))
	shown, keys := EditableDocument(pieceID, base, editorVisibility(t, RoleContributor))
	assert.Equal(t, new(LicenceVersion40), shown.LicenceVersion, "the licence version is public")
	assert.True(t, keys[licenceVersionKey])
}

func TestRestore_LicenceVersion(t *testing.T) {
	t.Parallel()

	base := licensedPiece(new(LicenceCCBY), new(LicenceVersion40))
	received := ElementKeySet{licenceVersionKey: true}
	tests := []struct {
		name      string
		submitted PieceDocument
		received  ElementKeySet
		want      *LicenceVersion
	}{
		{name: "never received: the base's is kept", submitted: licensedPiece(new(LicenceCCBY), nil),
			received: ElementKeySet{}, want: new(LicenceVersion40)},
		{name: "received and cleared", submitted: licensedPiece(new(LicenceCCBY), nil), received: received},
		{name: "received and changed", submitted: licensedPiece(new(LicenceCCBY), new(LicenceVersion30)),
			received: received, want: new(LicenceVersion30)},
		{name: "never received, and set by the author", submitted: licensedPiece(new(LicenceCCBY), new(LicenceVersion30)),
			received: ElementKeySet{}, want: new(LicenceVersion30)},
		{name: "never received, licence changed to one without versions",
			submitted: licensedPiece(new(LicencePD), nil), received: ElementKeySet{}},
		{name: "never received, licence changed to another creative commons licence",
			submitted: licensedPiece(new(LicenceCCBYSA), nil), received: ElementKeySet{},
			want: new(LicenceVersion40)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, _ := RestoreDocument(pieceID, tt.submitted, &base, tt.received, editorVisibility(t, RoleContributor),
				countingKeys())
			assert.Equal(t, tt.want, got.LicenceVersion)
		})
	}

	t.Run("a new piece has no base to keep", func(t *testing.T) {
		t.Parallel()
		submitted := licensedPiece(new(LicenceCCBY), nil)
		got, _ := RestoreDocument(pieceID, submitted, nil, ElementKeySet{}, editorVisibility(t, RoleContributor),
			countingKeys())
		assert.Nil(t, got.LicenceVersion)
	})
}

func TestSubmit_AbsentLicenceVersionKeepsBase(t *testing.T) {
	t.Parallel()

	base := licensedPiece(new(LicenceCCBY), new(LicenceVersion40))
	vis := editorVisibility(t, RoleContributor)
	sub, keys := EditableDocument(pieceID, base, vis)
	sub.LicenceVersion = nil
	sub.NotesMD = ptr("Edited by an older client.")
	plan, err := PlanSubmit(SubmitState[PieceDocument]{Submitted: sub, Base: &base,
		SubmitInputs: SubmitInputs{ID: pieceID, Keys: countingKeys(),
			Received: EditorFieldsSent{Preferences: true}.ReceivedKeys(keys), Visibility: vis, Refs: testRefs(),
			Summary: testSummary, Today: validToday}})
	require.NoError(t, err, "%v", problemsOrNil(err))
	assert.Equal(t, new(LicenceVersion40), plan.Document.LicenceVersion)
	assert.Equal(t, ptr("Edited by an older client."), plan.Document.NotesMD)
}
