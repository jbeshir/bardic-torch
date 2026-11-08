// Package domaintest builds domain values for tests.
package domaintest

import "github.com/jbeshir/bardic-torch/internal/domain"

// Preference is a preference of dimension, stated at sourceURL, holding choices and no notes. Its choices are
// never nil, so a preference without choices is the empty list, as a client sends it.
func Preference(dimension domain.PreferenceDimension, sourceURL string,
	choices ...domain.PreferenceChoice,
) domain.DocPreference {
	return domain.DocPreference{
		Dimension: dimension,
		Value:     &domain.PreferenceValue{Choices: append([]domain.PreferenceChoice{}, choices...)},
		SourceURL: sourceURL,
	}
}
