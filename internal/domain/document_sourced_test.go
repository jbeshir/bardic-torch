package domain

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// validSource is a valid source URL; invalidSources are the invalid ones every sourced rule is tested with.
const validSource = "https://example.org/source"

var invalidSources = []string{
	"ftp://x", "javascript:alert(1)", "not a url", "https://example.org/" + strings.Repeat("x", 2049-20),
	"https://example.org/\u0000",
}

// sourcedRow is a §6.3 row: validate validates the row's change by author, with the source the rule checks set
// to source, or absent if nil.
type sourcedRow struct {
	id       string
	validate func(author Role, source *string) []DocumentProblem
	// path and code are the problem when the source is absent; with no code, an absent source is accepted.
	path string
	code ProblemCode
	// invalidPath is where an invalid source is reported.
	invalidPath string
	// always is a problem at path whatever the source, for the rows whose rule is not about the source.
	always ProblemCode
}

func validatePiece(prev *PieceDocument, d PieceDocument, author Role, summary string) []DocumentProblem {
	v := validationContext(pieceID, prev, author)
	v.Summary = summary

	return d.Validate(v)
}

func validatePerson(prev *PersonDocument, d PersonDocument, author Role) []DocumentProblem {
	return d.Validate(validationContext(personID, prev, author))
}

// withLicensingLink is d with a second link, a licensing source at source, if source is set.
func withLicensingLink(d PieceDocument, source *string) PieceDocument {
	if source != nil {
		d.Links = append(append([]DocPieceLink{}, d.Links...),
			DocPieceLink{DocLink: DocLink{URL: *source, Roles: []LinkRole{LinkRoleLicensingSource}}})
	}

	return d
}

func withPersonLicensingLink(d PersonDocument, source *string) PersonDocument {
	if source != nil {
		d.Links = append(append([]DocPersonLink{}, d.Links...),
			DocPersonLink{DocLink: DocLink{URL: *source, Roles: []LinkRole{LinkRoleLicensingSource}}})
	}

	return d
}

func pieceWith(mutate func(*PieceDocument)) PieceDocument {
	d := validPiece()
	mutate(&d)

	return d
}

func personWith(mutate func(*PersonDocument)) PersonDocument {
	d := validPerson()
	mutate(&d)

	return d
}

func TestSourcedFields(t *testing.T) {
	t.Parallel()

	known := func(d *PieceDocument) { d.LicensingStatus = LicensingStatusKnown }
	deceased := func(source *string) func(*PersonDocument) {
		return func(d *PersonDocument) { d.Status, d.StatusSourceURL = PersonStatusDeceased, source }
	}
	conservation := func(status ConservationStatus, source *string) func(*PieceDocument) {
		return func(d *PieceDocument) { d.Conservation = DocConservation{Status: status, SourceURL: source} }
	}
	rows := []sourcedRow{
		{id: "licensing_notes_changed", validate: func(a Role, s *string) []DocumentProblem {
			base := validPiece()
			return validatePiece(&base, withLicensingLink(pieceWith(func(d *PieceDocument) {
				d.LicensingNotesMD = ptr("Ask before recording.")
			}), s), a, "")
		}, path: "/licensing_notes_md", code: ProblemSourceRequired, invalidPath: pathLicensingLinkURL},
		{id: "licensing_notes_cleared", validate: func(a Role, s *string) []DocumentProblem {
			base := pieceWith(func(d *PieceDocument) { d.LicensingNotesMD = ptr("Ask first.") })
			return validatePiece(&base, withLicensingLink(pieceWith(func(d *PieceDocument) {
				d.LicensingNotesMD = ptr("")
			}), s), a, "")
		}, invalidPath: pathLicensingLinkURL},
		{id: "licensing_notes_removed", validate: func(a Role, s *string) []DocumentProblem {
			base := pieceWith(func(d *PieceDocument) { d.LicensingNotesMD = ptr("Ask first.") })
			return validatePiece(&base, withLicensingLink(validPiece(), s), a, "")
		}, invalidPath: pathLicensingLinkURL},
		{id: "licence_changed_with_known_status", validate: func(a Role, s *string) []DocumentProblem {
			base := pieceWith(known)
			return validatePiece(&base, withLicensingLink(pieceWith(func(d *PieceDocument) {
				known(d)
				d.Licence = ptr(LicenceCCBYSA)
			}), s), a, "")
		}, path: pathLicence, code: ProblemSourceRequired, invalidPath: pathLicensingLinkURL},
		{id: "licence_needs_known_status", validate: func(a Role, s *string) []DocumentProblem {
			base := validPiece()
			return validatePiece(&base, withLicensingLink(pieceWith(func(d *PieceDocument) {
				d.Licence = ptr(LicenceCCBY)
			}), s), a, "")
		}, path: pathLicence, always: ProblemLicenceNeedsKnown, invalidPath: pathLicensingLinkURL},
		{id: "licensing_status_known", validate: func(a Role, s *string) []DocumentProblem {
			base := validPiece()
			return validatePiece(&base, withLicensingLink(pieceWith(known), s), a, "")
		}, path: pathLicensingStatus, code: ProblemSourceRequired, invalidPath: pathLicensingLinkURL},
		{id: "person_licensing_notes_changed", validate: func(a Role, s *string) []DocumentProblem {
			base := validPerson()
			return validatePerson(&base, withPersonLicensingLink(personWith(func(d *PersonDocument) {
				d.LicensingNotesMD = ptr("Ask before recording.")
			}), s), a)
		}, path: "/licensing_notes_md", code: ProblemSourceRequired, invalidPath: pathLicensingLinkURL},
		{id: "person_licensing_status_known", validate: func(a Role, s *string) []DocumentProblem {
			base := validPerson()
			return validatePerson(&base, withPersonLicensingLink(personWith(func(d *PersonDocument) {
				d.LicensingStatus = LicensingStatusKnown
			}), s), a)
		}, path: pathLicensingStatus, code: ProblemSourceRequired, invalidPath: pathLicensingLinkURL},
		{id: "deceased_status_needs_source", validate: func(a Role, s *string) []DocumentProblem {
			base := personWith(deceased(s)) // an existing document that violates the source rule when s is absent
			return validatePerson(&base, personWith(deceased(s)), a)
		}, path: pathStatusSource, code: ProblemSourceRequired, invalidPath: pathStatusSource},
		{id: "prefers_no_page_needs_source", validate: func(a Role, s *string) []DocumentProblem {
			base := validPerson()
			return validatePerson(&base, personWith(func(d *PersonDocument) {
				d.Status, d.StatusSourceURL = PersonStatusPrefersNoPage, s
			}), a)
		}, path: pathStatusSource, code: ProblemSourceRequired, invalidPath: pathStatusSource},
		{id: "status_source_removed", validate: func(a Role, s *string) []DocumentProblem {
			base := personWith(deceased(ptr(validSource + "/obituary")))
			return validatePerson(&base, personWith(func(d *PersonDocument) { d.StatusSourceURL = s }), a)
		}, path: pathStatusSource, code: ProblemSourceRequired, invalidPath: pathStatusSource},
		{id: "status_date_without_status_change", validate: func(a Role, s *string) []DocumentProblem {
			base := validPerson()
			return validatePerson(&base, personWith(func(d *PersonDocument) {
				d.StatusDate, d.StatusSourceURL = &validToday, s
			}), a)
		}, path: "/status_date", always: ProblemDateNeedsStatus, invalidPath: pathStatusSource},
		{id: "conservation_status_needs_source", validate: func(a Role, s *string) []DocumentProblem {
			base := validPiece()
			return validatePiece(&base, pieceWith(conservation(ConservationStatusEndangered, s)), a, "Assessed.")
		}, path: pathConservationSource, code: ProblemSourceRequired, invalidPath: pathConservationSource},
		{id: "conservation_blank_reason", validate: func(a Role, s *string) []DocumentProblem {
			base := validPiece()
			return validatePiece(&base, pieceWith(conservation(ConservationStatusEndangered, s)), a, " ")
		}, path: pathSummary, always: ProblemReasonRequired, invalidPath: pathConservationSource},
		{id: "conservation_status_changed_without_reason", validate: func(a Role, s *string) []DocumentProblem {
			base := pieceWith(conservation(ConservationStatusEndangered, ptr(validSource)))
			return validatePiece(&base, pieceWith(conservation(ConservationStatusRestricted, s)), a, "")
		}, path: pathSummary, always: ProblemReasonRequired, invalidPath: pathConservationSource},
		{id: "conservation_unchanged_status_needs_source", validate: func(a Role, s *string) []DocumentProblem {
			base := pieceWith(conservation(ConservationStatusLowRisk, s))
			return validatePiece(&base, pieceWith(conservation(ConservationStatusLowRisk, s)), a, "")
		}, path: pathConservationSource, code: ProblemSourceRequired, invalidPath: pathConservationSource},
		{id: "new_piece_conservation_needs_source", validate: func(a Role, s *string) []DocumentProblem {
			return validatePiece(nil, pieceWith(conservation(ConservationStatusLowRisk, s)), a, "New piece.")
		}, path: pathConservationSource, code: ProblemSourceRequired, invalidPath: pathConservationSource},
		{id: "new_piece_conservation_without_reason", validate: func(a Role, s *string) []DocumentProblem {
			return validatePiece(nil, pieceWith(conservation(ConservationStatusLowRisk, s)), a, "")
		}, path: pathSummary, always: ProblemReasonRequired, invalidPath: pathConservationSource},
		{id: "piece_preference_needs_source", validate: func(a Role, s *string) []DocumentProblem {
			base := validPiece()
			return validatePiece(&base, pieceWith(func(d *PieceDocument) {
				d.Preferences = []DocPreference{{Dimension: PreferenceDimensionRecording, Value: []byte(`{}`),
					SourceURL: deref(s)}}
			}), a, "")
		}, path: pathPreferenceSource, code: ProblemSourceRequired, invalidPath: pathPreferenceSource},
		{id: "person_preference_needs_source", validate: func(a Role, s *string) []DocumentProblem {
			base := validPerson()
			return validatePerson(&base, personWith(func(d *PersonDocument) {
				d.Preferences = []DocPreference{{Dimension: PreferenceDimensionLicensing, Value: []byte(`{}`),
					SourceURL: deref(s)}}
			}), a)
		}, path: pathPreferenceSource, code: ProblemSourceRequired, invalidPath: pathPreferenceSource},
		{id: "unchanged_status_source_not_required", validate: func(a Role, s *string) []DocumentProblem {
			base := personWith(func(d *PersonDocument) { d.StatusSourceURL = s })
			return validatePerson(&base, personWith(func(d *PersonDocument) { d.StatusSourceURL = s }), a)
		}, invalidPath: pathStatusSource},
		{id: "reverted_conservation_without_reason", validate: func(a Role, s *string) []DocumentProblem {
			current := pieceWith(conservation(ConservationStatusLowRisk, ptr(validSource+"/current")))
			return validatePiece(&current, pieceWith(conservation(ConservationStatusEndangered, s)), a, "")
		}, path: pathSummary, always: ProblemReasonRequired, invalidPath: pathConservationSource},
		{id: "reverted_conservation_with_summary", validate: func(a Role, s *string) []DocumentProblem {
			current := pieceWith(conservation(ConservationStatusLowRisk, ptr(validSource+"/current")))
			return validatePiece(&current, pieceWith(conservation(ConservationStatusEndangered, s)), a, "Reverted.")
		}, path: pathConservationSource, code: ProblemSourceRequired, invalidPath: pathConservationSource},
		{id: "licence_changed_after_known_status", validate: func(a Role, s *string) []DocumentProblem {
			current := pieceWith(known)
			resolved := withLicensingLink(pieceWith(func(d *PieceDocument) {
				known(d)
				d.Licence = ptr(LicencePD)
			}), s)
			return validatePiece(&current, resolved, a, "")
		}, path: pathLicence, code: ProblemSourceRequired, invalidPath: pathLicensingLinkURL},
		{id: "person_deceased_needs_source", validate: func(a Role, s *string) []DocumentProblem {
			base := validPerson()
			return validatePerson(&base, personWith(deceased(s)), a)
		}, path: pathStatusSource, code: ProblemSourceRequired, invalidPath: pathStatusSource},
		{id: "merged_licensing_link", validate: func(a Role, s *string) []DocumentProblem {
			// The revision set the status to known with a licensing link; the clean merge with current, which
			// deleted that link meanwhile, keeps the link only when s is set.
			current := validPiece()
			merged := withLicensingLink(pieceWith(known), s)
			return validatePiece(&current, merged, a, "")
		}, path: pathLicensingStatus, code: ProblemSourceRequired, invalidPath: pathLicensingLinkURL},
	}

	for _, row := range rows {
		for _, author := range []Role{RoleContributor, RoleModerator} {
			t.Run(fmt.Sprintf("%s %s", row.id, author), func(t *testing.T) {
				t.Parallel()

				present := row.validate(author, ptr(validSource))
				absent := row.validate(author, nil)
				if row.always != "" {
					assert.Contains(t, present, DocumentProblem{Path: row.path, Code: row.always}, "present")
					assert.Contains(t, absent, DocumentProblem{Path: row.path, Code: row.always}, "absent")
				} else {
					assert.Empty(t, present, "present")
					if row.code == "" {
						assert.Empty(t, absent, "absent")
					} else {
						assert.Equal(t, []DocumentProblem{{Path: row.path, Code: row.code}}, absent, "absent")
					}
				}
				for _, invalid := range invalidSources {
					assert.Contains(t, row.validate(author, &invalid),
						DocumentProblem{Path: row.invalidPath, Code: ProblemInvalidURL}, "invalid %.30q", invalid)
				}
			})
		}
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}
