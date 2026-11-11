package domain

import (
	"slices"
	"strings"
)

// ValidatePieceSources returns the problems of a revision's sourced piece fields, compared with old, the current
// document (nil for a new piece, compared with no licensing information and an unassessed status): a changed
// licence, licence version or licensing notes, or a licensing status changed to known, needs a link with role
// licensing_source; a conservation status other than unassessed needs a source URL, and a changed one a summary.
// The importer does not run it.
func ValidatePieceSources(old *PieceDocument, doc PieceDocument, summary string) []Problem {
	prev := PieceDocument{LicensingStatus: LicensingStatusUnknown,
		Conservation: DocConservation{Status: ConservationStatusUnassessed}}
	if old != nil {
		prev = *old
	}
	sourced := slices.ContainsFunc(doc.Links, func(l DocPieceLink) bool {
		return slices.Contains(l.Roles, LinkRoleLicensingSource)
	})
	var p problems
	p.check(sourced || doc.Licence == nil || equalPointers(doc.Licence, prev.Licence), "licence",
		ProblemSourceRequired)
	p.check(sourced || doc.LicenceVersion == nil || equalPointers(doc.LicenceVersion, prev.LicenceVersion),
		"licence_version", ProblemSourceRequired)
	p.check(sourced || blank(doc.LicensingNotesMD) || equalPointers(doc.LicensingNotesMD, prev.LicensingNotesMD),
		"licensing_notes_md", ProblemSourceRequired)
	p.check(sourced || doc.LicensingStatus != LicensingStatusKnown || prev.LicensingStatus == LicensingStatusKnown,
		"licensing_status", ProblemSourceRequired)
	p.check(doc.Conservation.Status == ConservationStatusUnassessed || !blank(doc.Conservation.SourceURL),
		"conservation.source_url", ProblemSourceRequired)
	p.check(doc.Conservation.Status == prev.Conservation.Status || strings.TrimSpace(summary) != "", "summary",
		ProblemSummaryRequired)

	return p
}

// ValidatePersonSources returns the problems of a revision's sourced person fields: a status other than active
// needs a source URL. The importer does not run it.
func ValidatePersonSources(doc PersonDocument) []Problem {
	var p problems
	p.check(doc.Status == PersonStatusActive || !blank(doc.StatusSourceURL), "status_source_url",
		ProblemSourceRequired)

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
