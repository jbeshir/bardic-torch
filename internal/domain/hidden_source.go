package domain

// The hidden-source rule (docs/PLAN.md §4.1, "Hidden links"): a source URL outside a document's links (a
// preference's source, a person's status source, a piece's conservation source and each conservation history
// entry's) is shown only if it normalizes, and to no hidden link's normalized URL. A preference whose source is not
// shown is left out whole; a scalar source that is not shown is served as null. A source that does not normalize
// fails closed, as a link does.

// sourceShown is whether a read shows the source url: it normalizes to a URL that is not among hidden, the
// normalized URLs of hidden links.
func sourceShown(url string, hidden map[string]bool) bool {
	normalized, err := NormalizeURL(url)

	return err == nil && !hidden[normalized]
}

// ShownSource returns source as a read shows it, given hidden, the normalized URLs of the hidden links among the
// sources: source itself, or nil if it is nil or not shown.
func ShownSource(source *string, hidden map[string]bool) *string {
	if source == nil || !sourceShown(*source, hidden) {
		return nil
	}

	return source
}

// ShownPreferences returns the preferences of prefs a read shows, in order, given hidden: those whose source is
// shown. It returns nil if it shows none.
func ShownPreferences(prefs []DocPreference, hidden map[string]bool) []DocPreference {
	var out []DocPreference
	for _, p := range prefs {
		if sourceShown(p.SourceURL, hidden) {
			out = append(out, p)
		}
	}

	return out
}
