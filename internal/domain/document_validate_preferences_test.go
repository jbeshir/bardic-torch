package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The paths and values of the preference validation tests.
const (
	prefSourcePrefix     = "https://example.org/"
	prefSource           = prefSourcePrefix + "prefs"
	pathFirstPreference  = "/preferences/0"
	pathPrefDimension    = pathFirstPreference + "/dimension"
	pathPrefValue        = pathFirstPreference + "/value"
	pathPrefChoices      = pathPrefValue + "/choices"
	pathPrefFirstChoice  = pathPrefChoices + "/0"
	pathPrefNotes        = pathPrefValue + "/notes"
	prefNotes            = "Ask at the event."
	unknownDimension     = PreferenceDimension("singing")
	unknownChoice        = PreferenceChoice("maybe")
	prefTestCaseExpected = "problems"
)

// preferenceRow is a preference profile, of a piece or person document whose comparison document has the
// profile previous, and the problems validating it as author (a contributor if unset) must find: exactly want.
type preferenceRow struct {
	name     string
	entity   EntityType
	author   Role
	prefs    []DocPreference
	previous []DocPreference
	want     []DocumentProblem
}

// perm, adapt, commercial, objection, attribution and recording are preferences of one dimension with a valid
// source.
func perm(choices ...PreferenceChoice) DocPreference {
	return preference(PreferenceDimensionPerformancePermission, prefSource, choices...)
}

func adapt(choices ...PreferenceChoice) DocPreference {
	return preference(PreferenceDimensionAdaptations, prefSource, choices...)
}

func commercial(choices ...PreferenceChoice) DocPreference {
	return preference(PreferenceDimensionCommercialUse, prefSource, choices...)
}

func objection(choices ...PreferenceChoice) DocPreference {
	return preference(PreferenceDimensionObjectionableUse, prefSource, choices...)
}

func attribution(choices ...PreferenceChoice) DocPreference {
	return preference(PreferenceDimensionAttribution, prefSource, choices...)
}

func recording(choices ...PreferenceChoice) DocPreference {
	return preference(PreferenceDimensionRecording, prefSource, choices...)
}

func licensing(choices ...PreferenceChoice) DocPreference {
	return preference(PreferenceDimensionLicensing, prefSource, choices...)
}

func sourced(p DocPreference, source string) DocPreference {
	p.SourceURL = source

	return p
}

func profile(prefs ...DocPreference) []DocPreference { return prefs }

func problem(path string, code ProblemCode) []DocumentProblem {
	return []DocumentProblem{{Path: path, Code: code}}
}

// everyDimension is a valid profile of every dimension, as a person may have.
func everyDimension() []DocPreference {
	return profile(
		licensing(PreferenceChoiceCCBY),
		perm(PreferenceChoiceGiven),
		adapt(PreferenceChoiceEncouraged),
		attribution(PreferenceChoiceSCAName),
		commercial(PreferenceChoiceNonCommercialOnly),
		recording(PreferenceChoiceOralOnly),
		noted(preference(PreferenceDimensionPerformanceEtiquette, prefSource), prefNotes),
		objection(PreferenceChoiceNoObjectionableContent),
	)
}

// The rows of TestValidatePreferences, one group per rule; each group has a valid row.
func preferenceStructureRows() []preferenceRow {
	return []preferenceRow{
		{name: "dimension_known", prefs: profile(recording(PreferenceChoiceOralOnly))},
		{name: "dimension_unknown", prefs: profile(preference(unknownDimension, prefSource)),
			want: problem(pathPrefDimension, ProblemOutOfRange)},
		{name: "dimension_empty", prefs: profile(preference("", prefSource)),
			want: problem(pathPrefDimension, ProblemOutOfRange)},
		{name: "dimensions_distinct", entity: EntityTypePerson,
			prefs: profile(licensing(PreferenceChoiceCCBY), recording(PreferenceChoiceOralOnly))},
		{name: "dimension_twice", prefs: profile(recording(PreferenceChoiceOralOnly), recording(PreferenceChoiceNoWriting)),
			want: problem("/preferences/1", ProblemDuplicate)},
		{name: "licensing_on_person", entity: EntityTypePerson, prefs: profile(licensing(PreferenceChoiceVariesByWork))},
		{name: "licensing_on_piece", prefs: profile(licensing(PreferenceChoiceCCBY)),
			want: problem(pathPrefDimension, ProblemDimensionNotAllowed)},
		{name: "every_dimension_on_person", entity: EntityTypePerson, prefs: everyDimension()},
		// At most 8 preferences follows from one per dimension and 8 dimensions: a ninth repeats one.
		{name: "eight_preferences_at_most", entity: EntityTypePerson,
			prefs: append(everyDimension(), recording(PreferenceChoiceNoWriting)),
			want:  problem("/preferences/8", ProblemDuplicate)},
		{name: "choice_known", prefs: profile(perm(PreferenceChoiceAskFirst))},
		{name: "choice_unknown", prefs: profile(perm(unknownChoice, PreferenceChoiceGiven)),
			want: problem(pathPrefFirstChoice, ProblemOutOfRange)},
		{name: "choice_empty", prefs: profile(perm("", PreferenceChoiceGiven)),
			want: problem(pathPrefFirstChoice, ProblemOutOfRange)},
		{name: "choice_of_the_dimension", prefs: profile(perm(PreferenceChoiceGiven))},
		{name: "choice_of_another_dimension", prefs: profile(perm(PreferenceChoiceCCBY, PreferenceChoiceGiven)),
			want: problem(pathPrefFirstChoice, ProblemOutOfRange)},
		{name: "choices_distinct", prefs: profile(perm(PreferenceChoiceGiven, PreferenceChoiceInformWelcome))},
		{name: "choice_twice", prefs: profile(perm(PreferenceChoiceGiven, PreferenceChoiceGiven)),
			want: problem(pathPrefChoices+"/1", ProblemDuplicate)},
		{name: "value_shape", prefs: profile(noted(recording(), "x"))},
		{name: "value_null", prefs: profile(DocPreference{Dimension: PreferenceDimensionRecording, SourceURL: prefSource}),
			want: problem(pathPrefValue, ProblemRequired)},
		{name: "choices_null", prefs: profile(DocPreference{Dimension: PreferenceDimensionRecording, SourceURL: prefSource,
			Value: &PreferenceValue{Notes: ptr("x")}}),
			want: problem(pathPrefChoices, ProblemRequired)},
	}
}

func preferenceCardinalityRows() []preferenceRow {
	return []preferenceRow{
		{name: "exactly_one_present", entity: EntityTypePerson, prefs: profile(licensing(PreferenceChoiceCCBY))},
		{name: "exactly_one_missing_despite_notes", entity: EntityTypePerson, prefs: profile(noted(licensing(), prefNotes)),
			want: problem(pathPrefChoices, ProblemRequired)},
		{name: "exactly_one_missing_beside_other_groups", prefs: profile(perm(PreferenceChoiceInformWelcome)),
			want: problem(pathPrefChoices, ProblemRequired)},
		{name: "one_of_each_single_group", prefs: profile(perm(PreferenceChoiceGiven, PreferenceChoiceInformWelcome))},
		{name: "exactly_one_twice", prefs: profile(perm(PreferenceChoiceGiven, PreferenceChoiceAskFirst)),
			want: problem(pathPrefChoices, ProblemTooMany)},
		{name: "at_most_one_twice", prefs: profile(perm(PreferenceChoiceGiven, PreferenceChoiceInformRequired,
			PreferenceChoiceInformWelcome)),
			want: problem(pathPrefChoices, ProblemTooMany)},
		{name: "at_least_one_present", prefs: profile(attribution(PreferenceChoiceSCAName))},
		{name: "at_least_one_several", prefs: profile(attribution(PreferenceChoiceSCAName, PreferenceChoiceStageName,
			PreferenceChoiceCreditBefore, PreferenceChoiceMentionWebsite, PreferenceChoiceQRCode))},
		{name: "at_least_one_scope", prefs: profile(objection(PreferenceChoiceNoObjectionableContent,
			PreferenceChoiceProfanity))},
		{name: "at_least_one_missing", prefs: profile(attribution(PreferenceChoiceCreditBefore)),
			want: problem(pathPrefChoices, ProblemRequired)},
		{name: "at_least_one_scope_missing", prefs: profile(objection(PreferenceChoiceProfanity)),
			want: []DocumentProblem{{Path: pathPrefChoices, Code: ProblemRequired},
				{Path: pathPrefChoices, Code: ProblemChoiceConflict}}},
		{name: "optional_groups_with_a_choice", prefs: profile(recording(PreferenceChoiceOralOnly))},
		{name: "optional_groups_with_notes", prefs: profile(noted(
			preference(PreferenceDimensionPerformanceEtiquette, prefSource), prefNotes))},
		{name: "optional_groups_empty", prefs: profile(recording()),
			want: problem(pathPrefChoices, ProblemRequired)},
		{name: "optional_groups_at_most_one", prefs: profile(recording(PreferenceChoiceOralOnly, PreferenceChoiceNoWriting)),
			want: problem(pathPrefChoices, ProblemTooMany)},
	}
}

func preferenceConflictRows() []preferenceRow {
	conflict := problem(pathPrefChoices, ProblemChoiceConflict)

	return []preferenceRow{
		{name: "not_given_alone", prefs: profile(perm(PreferenceChoiceNotGiven))},
		{name: "not_given_informing", prefs: profile(perm(PreferenceChoiceNotGiven, PreferenceChoiceInformWelcome)),
			want: conflict},
		{name: "group_members_named", prefs: profile(noted(perm(PreferenceChoiceGroupMembers), prefNotes))},
		{name: "group_members_unnamed", prefs: profile(perm(PreferenceChoiceGroupMembers)),
			want: problem(pathPrefNotes, ProblemRequired)},
		{name: "commercial_group_members_unnamed", prefs: profile(commercial(PreferenceChoiceAllowedWithConditions,
			PreferenceChoiceGroupMembers)),
			want: problem(pathPrefNotes, ProblemRequired)},
		{name: "kinds_allowed_and_refused_apart", prefs: profile(adapt(PreferenceChoiceAllowed,
			PreferenceChoiceAllowFanVideos, PreferenceChoiceRefuseNewWords))},
		{name: "kind_allowed_and_refused", prefs: profile(adapt(PreferenceChoiceAllowed, PreferenceChoiceAllowNewWords,
			PreferenceChoiceRefuseNewWords)),
			want: conflict},
		{name: "kind_allowed_and_refused_last_pair", prefs: profile(adapt(PreferenceChoiceAllowed,
			PreferenceChoiceAllowStageUse, PreferenceChoiceRefuseStageUse)),
			want: conflict},
		{name: "not_allowed_refusing", prefs: profile(adapt(PreferenceChoiceNotAllowed, PreferenceChoiceRefuseFanVideos))},
		{name: "not_allowed_allowing", prefs: profile(adapt(PreferenceChoiceNotAllowed, PreferenceChoiceAllowShortening)),
			want: conflict},
		{name: "not_allowed_conditions", prefs: profile(adapt(PreferenceChoiceNotAllowed, PreferenceChoiceSeriousOnly)),
			want: conflict},
		{name: "conditions_with_conditions", prefs: profile(adapt(PreferenceChoiceAllowedWithConditions,
			PreferenceChoiceMinorChangesOnly))},
		{name: "conditions_ask_first", prefs: profile(adapt(PreferenceChoiceAskFirst,
			PreferenceChoiceShowBeforePerforming))},
		{name: "conditions_encouraged", prefs: profile(adapt(PreferenceChoiceEncouraged, PreferenceChoiceSeriousOnly)),
			want: conflict},
		{name: "commercial_with_conditions", prefs: profile(commercial(PreferenceChoiceAllowedWithConditions,
			PreferenceChoiceSellPrint, PreferenceChoiceProfitShare))},
		{name: "commercial_conditions_ask_first", prefs: profile(commercial(PreferenceChoiceAskFirst,
			PreferenceChoiceProfitShare))},
		{name: "commercial_conditions_non_commercial_only", prefs: profile(commercial(PreferenceChoiceNonCommercialOnly,
			PreferenceChoiceProfitShare)),
			want: conflict},
		{name: "commercial_non_commercial_uses", prefs: profile(commercial(PreferenceChoiceNonCommercialOnly,
			PreferenceChoiceTipsAndPrizes))},
		{name: "commercial_not_allowed_uses", prefs: profile(commercial(PreferenceChoiceNotAllowed,
			PreferenceChoiceMerchandise)),
			want: conflict},
		{name: "commercial_allowed_conditions", prefs: profile(commercial(PreferenceChoiceAllowed,
			PreferenceChoiceProfitShare)),
			want: conflict},
		{name: "objection_purposes_scoped", prefs: profile(objection(PreferenceChoiceNoObjectionablePurposes,
			PreferenceChoiceViolence))},
		{name: "objection_contexts_scoped", prefs: profile(objection(PreferenceChoiceNoObjectionableContexts,
			PreferenceChoiceAlcoholEvents, PreferenceChoiceAskIfUnsure))},
		{name: "objection_purposes_unscoped", prefs: profile(objection(PreferenceChoiceNoObjectionableContexts,
			PreferenceChoiceViolence)),
			want: conflict},
		{name: "objection_content_unscoped", prefs: profile(objection(PreferenceChoiceNoObjectionablePurposes,
			PreferenceChoiceNSFW)),
			want: conflict},
	}
}

func preferenceTextAndSourceRows() []preferenceRow {
	return []preferenceRow{
		{name: "notes_null", prefs: profile(recording(PreferenceChoiceOralOnly))},
		{name: "notes_text", prefs: profile(noted(recording(PreferenceChoiceOralOnly), prefNotes))},
		{name: "notes_empty", prefs: profile(noted(recording(PreferenceChoiceOralOnly), "")),
			want: problem(pathPrefNotes, ProblemBlank)},
		{name: "notes_spaces", prefs: profile(noted(recording(PreferenceChoiceOralOnly), "  ")),
			want: problem(pathPrefNotes, ProblemBlank)},
		{name: "notes_longest", prefs: profile(noted(recording(), strings.Repeat("\u00e9", maxPreferenceNotesLength)))},
		{name: "notes_too_long", prefs: profile(noted(recording(), strings.Repeat("a", maxPreferenceNotesLength+1))),
			want: problem(pathPrefNotes, ProblemTooLong)},
		{name: "notes_nfc", prefs: profile(noted(recording(), "Caf\u00e9"))},
		{name: "notes_nfd", prefs: profile(noted(recording(), "Cafe\u0301")),
			want: problem(pathPrefNotes, ProblemNotNFC)},
		{name: "notes_crlf", prefs: profile(noted(recording(), "a\r\nb")),
			want: problem(pathPrefNotes, ProblemInvalidCharacters)},
		{name: "notes_line_feed", prefs: profile(noted(recording(), "a\nb"))},
		{name: "notes_bidi_override", prefs: profile(noted(recording(), "a\u202eb")),
			want: problem(pathPrefNotes, ProblemInvalidCharacters)},
		{name: "source_valid", prefs: profile(recording(PreferenceChoiceOralOnly))},
		{name: "source_missing", prefs: profile(sourced(recording(PreferenceChoiceOralOnly), "")),
			want: problem(pathPreferenceSource, ProblemSourceRequired)},
		{name: "source_missing_unchanged", prefs: profile(sourced(recording(PreferenceChoiceOralOnly), "")),
			previous: profile(sourced(recording(PreferenceChoiceOralOnly), "")),
			want:     problem(pathPreferenceSource, ProblemSourceRequired)},
		{name: "source_not_http", prefs: profile(sourced(recording(PreferenceChoiceOralOnly), "ftp://x/y")),
			want: problem(pathPreferenceSource, ProblemInvalidURL)},
		{name: "source_with_space", prefs: profile(sourced(recording(PreferenceChoiceOralOnly), "https://exa mple.org")),
			want: problem(pathPreferenceSource, ProblemInvalidURL)},
		{name: "source_longest", prefs: profile(sourced(recording(PreferenceChoiceOralOnly),
			prefSourcePrefix+strings.Repeat("a", MaxURLLength-len(prefSourcePrefix))))},
		{name: "source_too_long", prefs: profile(sourced(recording(PreferenceChoiceOralOnly),
			prefSourcePrefix+strings.Repeat("a", MaxURLLength+1-len(prefSourcePrefix)))),
			want: problem(pathPreferenceSource, ProblemInvalidURL)},
		{name: "moderator_exactly_one_missing", author: RoleModerator, entity: EntityTypePerson,
			prefs: profile(noted(licensing(), prefNotes)),
			want:  problem(pathPrefChoices, ProblemRequired)},
		{name: "moderator_source_missing", author: RoleModerator, prefs: profile(sourced(recording(), "")),
			want: []DocumentProblem{{Path: pathPreferenceSource, Code: ProblemSourceRequired},
				{Path: pathPrefChoices, Code: ProblemRequired}}},
		{name: "moderator_licensing_on_piece", author: RoleModerator, prefs: profile(licensing(PreferenceChoiceCCBY)),
			want: problem(pathPrefDimension, ProblemDimensionNotAllowed)},
	}
}

// TestValidatePreferences checks every rule of preference profiles (PLAN §4.1 "Preference profiles"), each with a
// valid and an invalid row, on a piece unless the row names a person. Every problem has a path and a code, never
// a value.
func TestValidatePreferences(t *testing.T) {
	t.Parallel()

	var rows []preferenceRow
	for _, group := range [][]preferenceRow{preferenceStructureRows(), preferenceCardinalityRows(),
		preferenceConflictRows(), preferenceTextAndSourceRows()} {
		rows = append(rows, group...)
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			author := row.author
			if author == "" {
				author = RoleContributor
			}
			var got []DocumentProblem
			if row.entity == EntityTypePerson {
				base, d := validPerson(), validPerson()
				base.Preferences, d.Preferences = row.previous, row.prefs
				got = validatePerson(&base, d, author)
			} else {
				base, d := validPiece(), validPiece()
				base.Preferences, d.Preferences = row.previous, row.prefs
				got = validatePiece(&base, d, author, "")
			}
			assert.ElementsMatch(t, row.want, got, prefTestCaseExpected)
		})
	}
}

// TestDraftProblems_Preferences checks the preference rules even a draft must follow: a dimension or choice that
// is given is known, and the notes are NFC without control characters. Everything else waits for submit.
func TestDraftProblems_Preferences(t *testing.T) {
	t.Parallel()

	for _, row := range []preferenceRow{
		{name: "dimension_unknown", prefs: profile(preference(unknownDimension, prefSource)),
			want: problem(pathPrefDimension, ProblemOutOfRange)},
		{name: "choice_unknown", prefs: profile(perm(unknownChoice)),
			want: problem(pathPrefFirstChoice, ProblemOutOfRange)},
		{name: "notes_nfd", prefs: profile(noted(recording(), "Cafe\u0301")),
			want: problem(pathPrefNotes, ProblemNotNFC)},
		{name: "notes_crlf", prefs: profile(noted(recording(), "a\r\nb")),
			want: problem(pathPrefNotes, ProblemInvalidCharacters)},
		{name: "person_choice_unknown", entity: EntityTypePerson, prefs: profile(licensing(unknownChoice)),
			want: problem(pathPrefFirstChoice, ProblemOutOfRange)},
		{name: "dimension_not_chosen", prefs: profile(preference("", ""))},
		{name: "choice_not_chosen", prefs: profile(perm(""))},
		{name: "incomplete", prefs: profile(
			DocPreference{Dimension: PreferenceDimensionRecording},
			sourced(licensing(), ""),
			perm(PreferenceChoiceCCBY, PreferenceChoiceGiven, PreferenceChoiceGiven, PreferenceChoiceAskFirst),
			noted(recording(), ""),
			adapt(PreferenceChoiceNotAllowed, PreferenceChoiceAllowNewWords),
		)},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			var doc AnyDocument = PieceDocument{SchemaVersion: DocumentSchemaVersion, Preferences: row.prefs}
			if row.entity == EntityTypePerson {
				doc = PersonDocument{SchemaVersion: DocumentSchemaVersion, Preferences: row.prefs}
			}
			assert.ElementsMatch(t, row.want, DraftProblems(doc, ""), prefTestCaseExpected)
		})
	}
}

// TestValidateSubmission_NoPagePreferencesArePhaseTwo checks that an invalid preference of a person who prefers no
// page, which the contributor never sees (the profile is withheld whole), is no problem of their edit: it is a
// hidden_data_conflict flag for the moderator, so validation is no oracle for the withheld profile.
func TestValidateSubmission_NoPagePreferencesArePhaseTwo(t *testing.T) {
	t.Parallel()

	base := validPerson()
	base.Status, base.StatusSourceURL = PersonStatusPrefersNoPage, ptr(validSource)
	projection := PersonDocument{SchemaVersion: DocumentSchemaVersion, Name: base.Name, Kind: base.Kind,
		Status: base.Status, StatusSourceURL: base.StatusSourceURL, LicensingStatus: LicensingStatusUnknown}
	editor := projection
	editor.Name = "Morwen Placeholder-Hill"

	for name, tc := range map[string]struct {
		preferences []DocPreference
		flags       []Flag
	}{
		"valid": {preferences: profile(licensing(PreferenceChoiceCCBY))},
		"invalid": {preferences: profile(sourced(noted(licensing(), prefNotes), "")), flags: []Flag{
			{Code: FlagHiddenDataConflict, Path: pathPreferenceSource},
			{Code: FlagHiddenDataConflict, Path: pathPrefChoices},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			withProfile := base
			withProfile.Preferences = tc.preferences
			restored := withProfile
			restored.Name = editor.Name

			problems, flags := ValidateSubmission(editor, restored, &projection,
				validationContext(personID, &withProfile, RoleContributor))
			assert.Empty(t, problems, "no problem from the withheld profile")
			assert.ElementsMatch(t, tc.flags, flags)
		})
	}
}
