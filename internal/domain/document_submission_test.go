package domain

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// hiddenLegalName is a person's hidden legal name, which a contributor never sees.
var hiddenLegalName = DocPersonAlias{Name: legalName, Kind: PersonAliasKindLegal}

// personWithHiddenAlias is a person whose aliases are a public one, the hidden one and another public one, and
// its projection for a contributor, without the hidden alias.
func personWithHiddenAlias() (full, editor PersonDocument) {
	full = validPerson()
	full.Aliases = []DocPersonAlias{
		{Name: secondAliasName, Kind: PersonAliasKindSCA, IsPublic: true}, hiddenLegalName,
		{Name: morwenTheBard, Kind: PersonAliasKindNickname, IsPublic: true},
	}
	editor = full
	editor.Aliases = []DocPersonAlias{full.Aliases[0], full.Aliases[2]}

	return full, editor
}

// restoreAlias is editor with the hidden alias put back after the first alias, as restore does.
func restoreAlias(editor PersonDocument) PersonDocument {
	restored := editor
	restored.Aliases = append([]DocPersonAlias{editor.Aliases[0], hiddenLegalName}, editor.Aliases[1:]...)

	return restored
}

func TestValidateSubmission_RenameToHiddenAlias(t *testing.T) {
	t.Parallel()

	base, projection := personWithHiddenAlias()
	submit := func(name string) ([]DocumentProblem, []Flag) {
		editor := projection
		editor.Name = name

		return ValidateSubmission(editor, restoreAlias(editor), &projection,
			validationContext(personID, &base, RoleContributor))
	}

	hiddenProblems, hiddenFlags := submit(hiddenLegalName.Name)
	freshProblems, freshFlags := submit("Ash Fresh-Name")
	assert.Empty(t, hiddenProblems, "phase 1 passes")
	assert.Equal(t, freshProblems, hiddenProblems, "the contributor's answer is the same as for a fresh name")
	assert.Empty(t, freshFlags)
	assert.Equal(t, []Flag{{Code: FlagHiddenDataConflict, Path: "/aliases/1/name"}}, hiddenFlags)
}

func TestValidateSubmission_CapsCountVisibleElements(t *testing.T) {
	t.Parallel()

	base := validPerson()
	base.Aliases = nil
	for k := range 99 {
		base.Aliases = append(base.Aliases, DocPersonAlias{Name: fmt.Sprintf("Morwen %03d", k),
			Kind: PersonAliasKindNickname, IsPublic: true})
	}
	projection := base
	base.Aliases = append(base.Aliases, hiddenLegalName,
		DocPersonAlias{Name: otherLegalName, Kind: PersonAliasKindLegal})
	editor := projection
	editor.Aliases = append(append([]DocPersonAlias{}, projection.Aliases...),
		DocPersonAlias{Name: "Morwen 100", Kind: PersonAliasKindNickname, IsPublic: true})
	restored := editor
	restored.Aliases = append(append([]DocPersonAlias{}, editor.Aliases...), base.Aliases[99:]...)

	problems, flags := ValidateSubmission(editor, restored, &projection,
		validationContext(personID, &base, RoleContributor))
	assert.Empty(t, problems, "100 visible aliases")
	assert.Equal(t, []Flag{{Code: FlagHiddenDataConflict, Path: pathAliases}}, flags, "102 in the full document")
}

func TestValidateSubmission_HiddenLicensingSource(t *testing.T) {
	t.Parallel()

	// SF03b and W29: the piece's only licensing source is a hidden link, withheld from the contributor.
	base := withLicensingLink(validPiece(), ptr("https://example.org/hidden-licence"))
	projection := validPiece()
	editor := pieceWith(func(d *PieceDocument) { d.LicensingStatus = LicensingStatusKnown })
	restored := withLicensingLink(editor, ptr("https://example.org/hidden-licence"))

	problems, flags := ValidateSubmission(editor, restored, &projection,
		validationContext(pieceID, &base, RoleContributor))
	assert.Equal(t, []DocumentProblem{{Path: pathLicensingStatus, Code: ProblemSourceRequired}}, problems)
	assert.Empty(t, flags)

	noLink := validPiece()
	withoutLink, _ := ValidateSubmission(editor, editor, &noLink, validationContext(pieceID, &noLink, RoleContributor))
	assert.Equal(t, withoutLink, problems, "the same answer as with no link at all")
}

func TestValidateSubmission_EditorPaths(t *testing.T) {
	t.Parallel()

	// W30: a problem after a withheld element is at its index in the document the contributor sees.
	base, projection := personWithHiddenAlias()
	editor := projection
	editor.Aliases = append(append([]DocPersonAlias{}, projection.Aliases...),
		DocPersonAlias{Name: " ", Kind: PersonAliasKindNickname, IsPublic: true})

	problems, flags := ValidateSubmission(editor, restoreAlias(editor), &projection,
		validationContext(personID, &base, RoleContributor))
	assert.Equal(t, []DocumentProblem{{Path: "/aliases/2/name", Code: ProblemBlank}}, problems)
	assert.Empty(t, flags, "a submission phase 1 refuses gets no flags")
}

func TestValidateSubmission_RestoredNoPageCredit(t *testing.T) {
	t.Parallel()

	// W31: a piece crediting a person who has since chosen to have no page; the credit is withheld from the
	// contributor and restored unchanged, so it needs only to exist.
	base := validPiece()
	base.Credits = append(base.Credits, DocCredit{PersonID: ptr(refNoPagePerson), Role: CreditRoleTune,
		Attribution: AttributionNamed, Certainty: CertaintyCertain})
	projection := validPiece()
	editor := pieceWith(func(d *PieceDocument) { d.NotesMD = ptr("Edited notes.") })
	restored := editor
	restored.Credits = base.Credits

	for _, author := range []Role{RoleContributor, RoleModerator} {
		problems, flags := ValidateSubmission(editor, restored, &projection,
			validationContext(pieceID, &base, author))
		assert.Empty(t, problems, author)
		assert.Empty(t, flags, author)
		assert.Empty(t, restored.Validate(validationContext(pieceID, &base, author)), "approval by %s", author)
	}
}

func TestValidateSubmission_NoPageProfileNotesIgnored(t *testing.T) {
	t.Parallel()

	// W14: notes a contributor sends for a no-page person are not a problem in phase 1, as restore discards them.
	base := validPerson()
	base.Status, base.StatusSourceURL = PersonStatusPrefersNoPage, ptr(validSource)
	projection := PersonDocument{SchemaVersion: DocumentSchemaVersion, Name: base.Name, Kind: base.Kind,
		Status: base.Status, StatusSourceURL: base.StatusSourceURL, LicensingStatus: LicensingStatusUnknown}
	editor := projection
	editor.NotesMD = ptr("Typed notes.")

	problems, flags := ValidateSubmission(editor, base, &projection, validationContext(personID, &base, RoleContributor))
	assert.Empty(t, problems)
	assert.Empty(t, flags)

	unrestored := base
	unrestored.NotesMD = editor.NotesMD
	_, flags = ValidateSubmission(editor, unrestored, &projection, validationContext(personID, &base, RoleContributor))
	assert.Equal(t, []Flag{{Code: FlagHiddenDataConflict, Path: pathNotes}}, flags, "defence in depth")
}
