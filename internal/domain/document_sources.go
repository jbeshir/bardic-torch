package domain

import (
	"slices"
	"strings"
)

// ValidatePieceSources returns the problems of a revision's licensing fields (R-12), compared with old, the
// current document (nil for a new piece, compared with no licensing information): changed licensing notes, or a
// licensing status changed to known, needs a link with role licensing_source.
// The importer does not run it.
func ValidatePieceSources(old *PieceDocument, doc PieceDocument) []Problem {
	prev := PieceDocument{LicensingStatus: LicensingStatusUnknown}
	if old != nil {
		prev = *old
	}
	sourced := slices.ContainsFunc(doc.Links, func(l DocPieceLink) bool {
		return slices.Contains(l.Roles, LinkRoleLicensingSource)
	})
	var p problems
	p.check(sourced || blank(doc.LicensingNotesMD) || equalPointers(doc.LicensingNotesMD, prev.LicensingNotesMD),
		"licensing_notes_md", ProblemSourceRequired)
	p.check(sourced || doc.LicensingStatus != LicensingStatusKnown || prev.LicensingStatus == LicensingStatusKnown,
		"licensing_status", ProblemSourceRequired)

	return p
}

// blank reports whether s is nil or only white space.
func blank(s *string) bool {
	return s == nil || strings.TrimSpace(*s) == ""
}

// equalPointers reports whether a and b are both nil or point to equal values.
func equalPointers[T comparable](a, b *T) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
