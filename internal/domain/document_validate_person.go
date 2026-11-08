package domain

import (
	"fmt"
	"reflect"
	"slices"
)

// personPerformances checks the performances of a person's links, and returns the links.
func (c *docCheck) personPerformances(links []DocPersonLink) []DocLink {
	out := make([]DocLink, 0, len(links))
	for k, l := range links {
		out = append(out, l.DocLink)
		performances := make([]DocPerformance, 0, len(l.Performances))
		for _, p := range l.Performances {
			performances = append(performances, p.DocPerformance)
		}
		c.performances(k, performances)
	}

	return out
}

// Validate returns the problems of a person document.
func (d PersonDocument) Validate(v ValidationContext[PersonDocument]) []DocumentProblem {
	prev := DefaultPersonDocument()
	if v.Previous != nil {
		prev = *v.Previous
	}
	c := newDocCheck(d, v.Previous, v.Refs, v.ID)
	c.document(d, d.SchemaVersion, v.Summary)
	c.title(d.Name, "/name")
	c.optionalVarchar(d.Pronouns, maxPronounsLength, "/pronouns")
	c.text(d.NotesMD, "/notes_md")
	c.text(d.LicensingNotesMD, "/licensing_notes_md")
	c.personAliases(d, prev, v.Author)
	c.unique(stringsOf(d.Roles), "/roles")
	c.cap(len(d.AffiliationIDs), maxAffiliations, "/affiliations")
	affiliations := make([]string, 0, len(d.AffiliationIDs))
	for _, id := range d.AffiliationIDs {
		affiliations = append(affiliations, fmt.Sprint(id))
	}
	c.unique(affiliations, "/affiliations")
	relations := make([]string, 0, len(d.Relations))
	for _, r := range d.Relations {
		relations = append(relations, fmt.Sprint(r.ToPersonID, r.Type))
	}
	c.relations(relations)
	links := c.personPerformances(d.Links)
	c.links(links)
	c.licensingSources(d.LicensingNotesMD, prev.LicensingNotesMD, d.LicensingStatus, prev.LicensingStatus,
		hasLicensingSource(links))
	c.personStatus(d, prev, v.Today)
	c.preferences(d.Preferences, prev.Preferences)
	c.references(documentReferences(d))
	if v.Author == RoleContributor && !v.editor && v.Previous != nil &&
		(prev.Status == PersonStatusPrefersNoPage || v.Refs.Persons[v.ID].PrefersNoPage) {
		c.identityOnly(d, prev)
	}

	return c.problems
}

// personStatus checks a person's status: any status but active, and any change of status, needs a
// valid source; only a status other than active has a date, which is not after today.
func (c *docCheck) personStatus(d, prev PersonDocument, today Date) {
	c.sourceURL(d.StatusSourceURL, "/status_source_url")
	if d.Status != PersonStatusActive || d.Status != prev.Status {
		c.requireSource(d.StatusSourceURL, "/status_source_url")
	}
	c.check(d.Status != PersonStatusActive || d.StatusDate == nil, "/status_date", ProblemDateNeedsStatus)
	c.check(d.StatusDate == nil || !d.StatusDate.After(today.Time), "/status_date", ProblemOutOfRange)
}

// personAliases checks a person's aliases: each a name, unique and not the person's name (as the collation
// compares them), and, of a contributor's document, each alias the edit adds or changes is hidden exactly when it
// is a legal name: a contributor cannot publish a legal name or hide another.
func (c *docCheck) personAliases(d, prev PersonDocument, author Role) {
	c.cap(len(d.Aliases), maxAliases, "/aliases")
	before := map[string]DocPersonAlias{}
	for _, a := range prev.Aliases {
		before[CollationKey(a.Name)] = a
	}
	keys := make([]string, 0, len(d.Aliases))
	for k, a := range d.Aliases {
		path := fmt.Sprintf("/aliases/%d", k)
		c.title(a.Name, path+"/name")
		key := CollationKey(a.Name)
		c.check(key != CollationKey(d.Name), path+"/name", ProblemDuplicate)
		keys = append(keys, key)
		if old, ok := before[key]; author == RoleContributor && (!ok || old != a) {
			c.check(a.IsPublic == (a.Kind != PersonAliasKindLegal), path+"/is_public", ProblemAliasVisibility)
		}
	}
	c.unique(keys, "/aliases")
}

// identityOnly checks that a contributor's document of a person who prefers no page differs from the comparison
// document in the identity fields only. Restore makes any other difference impossible; this is defence in depth.
func (c *docCheck) identityOnly(d, prev PersonDocument) {
	dv, pv := reflect.ValueOf(d), reflect.ValueOf(prev)
	for i := range dv.NumField() {
		f := dv.Type().Field(i)
		if !slices.Contains(identityFields, jsonFieldName(f)) && !equalValues(dv.Field(i), pv.Field(i)) {
			c.report(documentPaths.field("", f), ProblemWithheldFieldChanged)
		}
	}
}

// preferences checks preference profiles: each dimension once, and each added or changed one sourced.
func (c *docCheck) preferences(prefs, prev []DocPreference) {
	dims := make([]string, 0, len(prefs))
	for k, p := range prefs {
		path := fmt.Sprintf("/preferences/%d/source_url", k)
		dims = append(dims, string(p.Dimension))
		if p.SourceURL != "" {
			c.sourceURL(&p.SourceURL, path)
		}
		unchanged := slices.ContainsFunc(prev, func(q DocPreference) bool { return reflect.DeepEqual(p, q) })
		c.check(unchanged || p.SourceURL != "", path, ProblemSourceRequired)
	}
	c.unique(dims, "/preferences")
}
