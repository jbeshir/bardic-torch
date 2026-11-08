package domain

import "strings"

// EditorFieldsSent says which of the editor fields added after a client may have been built a document it sent
// carried (docs/PLAN.md §5.1, "Editor format"). A client built before a field existed omits it, and an omitted
// field keeps the base's value rather than clearing it: the author is treated as never having received it.
type EditorFieldsSent struct {
	// Preferences is whether a piece or person document carried its preferences (a list, not null).
	Preferences bool
}

// AllEditorFieldsSent is a document that carried every editor field.
var AllEditorFieldsSent = EditorFieldsSent{Preferences: true}

// preferenceKeyPrefix starts the element key of every preference (preferenceElementKey).
var preferenceKeyPrefix = elementKey("preference")

// preferenceElementKey is the element key of a preference: its dimension, of which a document holds one at most.
func preferenceElementKey(p DocPreference) string { return elementKey("preference", p.Dimension) }

// ReceivedKeys returns received without the keys of the elements of the fields the document did not carry, so
// that restoring a submission puts the base's elements of those fields back. It only ever removes keys.
func (s EditorFieldsSent) ReceivedKeys(received ElementKeySet) ElementKeySet {
	out := make(ElementKeySet, len(received))
	for key, ok := range received {
		if !s.Preferences && strings.HasPrefix(key, preferenceKeyPrefix) {
			continue
		}
		out[key] = ok
	}

	return out
}
