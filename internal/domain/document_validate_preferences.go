package domain

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// maxPreferenceNotesLength is the most code points a preference's notes may have.
const maxPreferenceNotesLength = 2000

// preferences checks a document's preference profile: each dimension at most once (so at most one preference per
// dimension), no licensing dimension on a piece (whose licence has its own fields), and each preference with a
// valid source and a value (preferenceValue). Unknown dimensions and choices, and the characters of the notes,
// are checked with the rest of the document (contents). Every preference needs its source, changed or not.
func (c *docCheck) preferences(prefs []DocPreference, entity EntityType) {
	dims := make([]string, 0, len(prefs))
	for k, p := range prefs {
		path := fmt.Sprintf("/preferences/%d", k)
		dims = append(dims, string(p.Dimension))
		allowed := entity != EntityTypePiece || p.Dimension != PreferenceDimensionLicensing
		c.check(allowed, path+"/dimension", ProblemDimensionNotAllowed)
		c.check(p.SourceURL != "", path+"/source_url", ProblemSourceRequired)
		c.check(p.SourceURL == "" || IsValidURL(p.SourceURL), path+"/source_url", ProblemInvalidURL)
		if p.Value == nil {
			c.report(path+"/value", ProblemRequired)

			continue
		}
		if p.Dimension.Valid() && allowed {
			c.preferenceValue(p.Dimension, *p.Value, path+"/value")
		}
	}
	c.unique(dims, "/preferences")
}

// preferenceValue checks the value of a preference of a known dimension, at path: its choices are given, each a
// choice of the dimension, at most once, as many of each group as the group allows (preferenceGroups) and without
// contradicting each other (preferenceChoicesConflict); and its notes, if any, are not blank and fit, and are
// given where a choice needs them.
func (c *docCheck) preferenceValue(dimension PreferenceDimension, v PreferenceValue, path string) {
	choicesPath, notesPath := path+"/choices", path+"/notes"
	c.check(v.Choices != nil, choicesPath, ProblemRequired)
	if v.Notes != nil {
		c.check(strings.TrimSpace(*v.Notes) != "", notesPath, ProblemBlank)
		c.check(utf8.RuneCountInString(*v.Notes) <= maxPreferenceNotesLength, notesPath, ProblemTooLong)
	}
	held := map[PreferenceChoice]bool{}
	for j, choice := range v.Choices {
		_, ok := preferenceChoiceOrder(dimension, choice)
		c.check(ok, c.paths.index(choicesPath, j), ProblemOutOfRange)
		held[choice] = ok
	}
	c.unique(stringsOf(v.Choices), choicesPath)
	c.preferenceGroups(dimension, held, v, choicesPath)
	c.check(!preferenceChoicesConflict(dimension, held), choicesPath, ProblemChoiceConflict)
	for _, choice := range preferenceChoicesNeedingNotes {
		c.check(!held[choice] || v.Notes != nil, notesPath, ProblemRequired)
	}
}

// preferenceGroups checks that a value of dimension, holding the choices held, has as many choices of each group
// as the group's cardinality allows, and, if no group needs a choice, has a choice or notes.
func (c *docCheck) preferenceGroups(dimension PreferenceDimension, held map[PreferenceChoice]bool,
	v PreferenceValue, path string,
) {
	needsChoice := false
	for _, g := range preferenceVocabularies[dimension] {
		n := len(slices.DeleteFunc(slices.Clone(g.choices), func(choice PreferenceChoice) bool { return !held[choice] }))
		needsChoice = needsChoice || g.cardinality.needsChoice()
		c.check(n > 0 || !g.cardinality.needsChoice(), path, ProblemRequired)
		c.check(n <= 1 || g.cardinality.allowsSeveral(), path, ProblemTooMany)
	}
	c.check(needsChoice || len(v.Choices) > 0 || v.Notes != nil, path, ProblemRequired)
}

// needsChoice reports whether a group of cardinality k needs a choice.
func (k preferenceCardinality) needsChoice() bool {
	return k == cardinalityExactlyOne || k == cardinalityAtLeastOne
}

// allowsSeveral reports whether a group of cardinality k allows more than one choice.
func (k preferenceCardinality) allowsSeveral() bool {
	return k == cardinalityAtLeastOne || k == cardinalityAny
}

// preferenceChoicesConflict reports whether the choices held of dimension break a rule between its groups
// (preferenceRules) or hold both choices of a pair of twins (preferenceTwins).
func preferenceChoicesConflict(dimension PreferenceDimension, held map[PreferenceChoice]bool) bool {
	holds := func(choices []PreferenceChoice) bool {
		return slices.ContainsFunc(choices, func(choice PreferenceChoice) bool { return held[choice] })
	}
	broken := slices.ContainsFunc(preferenceRules, func(r preferenceRule) bool {
		return r.dimension == dimension && holds(preferenceGroupOf(dimension, r.group).choices) &&
			(r.requires != nil && !holds(r.requires) || holds(r.excludedBy))
	})

	return broken || slices.ContainsFunc(preferenceTwins, func(t preferenceTwin) bool {
		return t.dimension == dimension && t.bothHeld(held)
	})
}

// bothHeld reports whether held holds both choices of one of t's pairs.
func (t preferenceTwin) bothHeld(held map[PreferenceChoice]bool) bool {
	first, second := preferenceGroupOf(t.dimension, t.groups[0]), preferenceGroupOf(t.dimension, t.groups[1])
	for i, choice := range first.choices {
		if held[choice] && i < len(second.choices) && held[second.choices[i]] {
			return true
		}
	}

	return false
}
