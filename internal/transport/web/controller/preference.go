package controller

import "github.com/jbeshir/bardic-torch/internal/domain"

// PreferenceEntry is the JSON representation of one dimension of a stated preference profile, with its source.
type PreferenceEntry struct {
	Dimension domain.PreferenceDimension `json:"dimension"`
	Value     PreferenceValue            `json:"value"`
	SourceURL string                     `json:"source_url"`
}

// PreferenceValue is the JSON representation of what a preference states: its choices, in canonical order, and
// notes.
type PreferenceValue struct {
	Choices []domain.PreferenceChoice `json:"choices"`
	Notes   *string                   `json:"notes"`
}

func preferenceEntryFromDomain(p domain.DocPreference) PreferenceEntry {
	entry := PreferenceEntry{Dimension: p.Dimension, Value: PreferenceValue{Choices: []domain.PreferenceChoice{}},
		SourceURL: p.SourceURL}
	if p.Value != nil {
		entry.Value = PreferenceValue{Choices: nonNil(p.Value.Choices), Notes: p.Value.Notes}
	}

	return entry
}
