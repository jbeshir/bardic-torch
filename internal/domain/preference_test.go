package domain

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// preference is a preference of dimension, stated at source, holding choices (never nil) and no notes.
func preference(dimension PreferenceDimension, source string, choices ...PreferenceChoice) DocPreference {
	return DocPreference{Dimension: dimension, Value: &PreferenceValue{Choices: append([]PreferenceChoice{}, choices...)},
		SourceURL: source}
}

// noted is p with notes.
func noted(p DocPreference, notes string) DocPreference {
	p.Value = &PreferenceValue{Choices: p.Value.Choices, Notes: &notes}

	return p
}

// TestPreferenceVocabularies pins the shape of the vocabularies: every dimension has one, its codes are unique,
// the union of the codes in order is preferenceChoiceValues (what PreferenceChoice.Valid accepts), the licensing
// codes are the Licence values and varies_by_work, and the rules between groups name groups and codes of their
// dimension.
func TestPreferenceVocabularies(t *testing.T) {
	t.Parallel()

	var union []PreferenceChoice
	for _, d := range preferenceDimensionValues {
		groups, ok := preferenceVocabularies[d]
		require.True(t, ok, "%s has no vocabulary", d)
		var codes []PreferenceChoice
		for _, g := range groups {
			assert.NotEmpty(t, g.choices, "%s.%s", d, g.name)
			codes = append(codes, g.choices...)
		}
		assert.Len(t, codes, len(uniqueChoices(codes)), "%s has a code twice", d)
		for _, c := range codes {
			if !slices.Contains(union, c) {
				union = append(union, c)
			}
		}
	}
	assert.Len(t, preferenceVocabularies, len(preferenceDimensionValues))
	assert.Equal(t, preferenceChoiceValues, union)

	licensing := preferenceVocabularies[PreferenceDimensionLicensing]
	require.Len(t, licensing, 1)
	assert.Equal(t, append(strs(licenceValues), string(PreferenceChoiceVariesByWork)), strs(licensing[0].choices))

	for _, r := range preferenceRules {
		assert.NotEmpty(t, preferenceGroupOf(r.dimension, r.group).choices, "%s has no group %s", r.dimension, r.group)
		assert.NotEqual(t, r.requires == nil, r.excludedBy == nil, "a rule requires or excludes")
		for _, c := range slices.Concat(r.requires, r.excludedBy) {
			_, ok := preferenceChoiceOrder(r.dimension, c)
			assert.True(t, ok, "%s has no choice %s", r.dimension, c)
		}
	}
	for _, tw := range preferenceTwins {
		first, second := preferenceGroupOf(tw.dimension, tw.groups[0]), preferenceGroupOf(tw.dimension, tw.groups[1])
		require.Len(t, second.choices, len(first.choices), "%s twins", tw.dimension)
		for i, c := range first.choices {
			assert.Equal(t, strings.TrimPrefix(string(c), "allow_"),
				strings.TrimPrefix(string(second.choices[i]), "refuse_"), "twins pair by position")
		}
	}
	for _, c := range preferenceChoicesNeedingNotes {
		assert.True(t, c.Valid(), c)
	}
}

func uniqueChoices(codes []PreferenceChoice) []PreferenceChoice {
	out := slices.Clone(codes)
	slices.Sort(out)

	return slices.Compact(out)
}

// TestCanonicalisePreferences checks the canonical order profiles are stored and compared in: dimensions in
// their order and each value's choices in vocabulary order, anything unknown after the known in the order given,
// without changing the input.
func TestCanonicalisePreferences(t *testing.T) {
	t.Parallel()

	const source = "https://example.org/canonical"
	notes := "Ask at the event."
	for _, tc := range []struct {
		name string
		in   []DocPreference
		want []DocPreference
	}{
		{name: "nil", in: nil, want: nil},
		{name: "no_preferences", in: []DocPreference{}, want: []DocPreference{}},
		{
			name: "choices_in_vocabulary_order",
			in: []DocPreference{preference(PreferenceDimensionPerformancePermission, source,
				PreferenceChoiceInformWelcome, PreferenceChoiceGiven)},
			want: []DocPreference{preference(PreferenceDimensionPerformancePermission, source,
				PreferenceChoiceGiven, PreferenceChoiceInformWelcome)},
		},
		{
			name: "order_within_a_group",
			in: []DocPreference{preference(PreferenceDimensionObjectionableUse, source,
				PreferenceChoiceViolence, PreferenceChoiceProselytising, PreferenceChoiceNoObjectionablePurposes)},
			want: []DocPreference{preference(PreferenceDimensionObjectionableUse, source,
				PreferenceChoiceNoObjectionablePurposes, PreferenceChoiceProselytising, PreferenceChoiceViolence)},
		},
		{
			name: "dimensions_in_order",
			in: []DocPreference{
				preference(PreferenceDimensionRecording, source, PreferenceChoiceOralOnly),
				preference(PreferenceDimensionLicensing, source, PreferenceChoiceCCBY),
			},
			want: []DocPreference{
				preference(PreferenceDimensionLicensing, source, PreferenceChoiceCCBY),
				preference(PreferenceDimensionRecording, source, PreferenceChoiceOralOnly),
			},
		},
		{
			name: "unknown_and_other_dimensions_last_in_order",
			in: []DocPreference{preference(PreferenceDimensionRecording, source,
				"maybe", PreferenceChoiceCCBY, PreferenceChoiceNoRecording, PreferenceChoiceOralOnly)},
			want: []DocPreference{preference(PreferenceDimensionRecording, source,
				PreferenceChoiceOralOnly, PreferenceChoiceNoRecording, "maybe", PreferenceChoiceCCBY)},
		},
		{
			name: "unknown_dimension_last",
			in: []DocPreference{
				preference("singing", source),
				preference(PreferenceDimensionAttribution, source, PreferenceChoiceSCAName),
			},
			want: []DocPreference{
				preference(PreferenceDimensionAttribution, source, PreferenceChoiceSCAName),
				preference("singing", source),
			},
		},
		{
			name: "notes_kept_and_null_value_kept",
			in: []DocPreference{
				{Dimension: PreferenceDimensionRecording, SourceURL: source},
				noted(preference(PreferenceDimensionPerformanceEtiquette, source), notes),
			},
			want: []DocPreference{
				{Dimension: PreferenceDimensionRecording, SourceURL: source},
				noted(preference(PreferenceDimensionPerformanceEtiquette, source), notes),
			},
		},
		{
			name: "null_choices_stay_null",
			in: []DocPreference{{Dimension: PreferenceDimensionRecording, SourceURL: source,
				Value: &PreferenceValue{Notes: &notes}}},
			want: []DocPreference{{Dimension: PreferenceDimensionRecording, SourceURL: source,
				Value: &PreferenceValue{Notes: &notes}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			before := cloneProfile(tc.in)
			got := CanonicalisePreferences(tc.in)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, before, tc.in, "the input is not modified")
			assert.Equal(t, got, CanonicalisePreferences(got), "canonical order is stable")
		})
	}
}

// cloneProfile is a deep copy of prefs.
func cloneProfile(prefs []DocPreference) []DocPreference {
	if prefs == nil {
		return nil
	}
	out := make([]DocPreference, 0, len(prefs))
	for _, p := range prefs {
		if p.Value != nil {
			p.Value = &PreferenceValue{Choices: slices.Clone(p.Value.Choices), Notes: p.Value.Notes}
		}
		out = append(out, p)
	}

	return out
}
