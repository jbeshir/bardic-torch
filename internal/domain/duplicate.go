package domain

// The duplicate check on create (D13): a new entity's names are compared, by DuplicateKey, with the names of the
// existing entities of its type. Contributor-facing checks never compare with hidden aliases, which would make
// them an oracle for hidden data; moderator views do.

// DuplicateField is what an existing entity matched a new one by.
type DuplicateField string

const (
	// DuplicateFieldTitle is a piece's title.
	DuplicateFieldTitle DuplicateField = "title"
	// DuplicateFieldAlias is an alias of a piece or person. It never says which alias, nor whether it is hidden.
	DuplicateFieldAlias DuplicateField = "alias"
	// DuplicateFieldName is the name of a person, term, collection or event, or a term's display name.
	DuplicateFieldName DuplicateField = "name"
	// DuplicateFieldURL is a collection's URL, compared normalized.
	DuplicateFieldURL DuplicateField = "url"
)

// DuplicateCandidate is a new entity's names, as compared with existing entities': a piece's title and aliases; a
// person's name and public aliases; a term's name and display name, within its facet; a collection's name and URL;
// an event's name.
type DuplicateCandidate struct {
	EntityType EntityType
	Names      []string
	// Facet is a term's facet.
	Facet Facet
	// URL is a collection's URL.
	URL *string
}

// NewDuplicateCandidate returns the names of the new entity doc describes.
func NewDuplicateCandidate(doc AnyDocument) DuplicateCandidate {
	c := DuplicateCandidate{EntityType: doc.EntityType()}
	switch d := doc.(type) {
	case PieceDocument:
		c.Names = append(c.Names, d.Title)
		for _, a := range d.Aliases {
			c.Names = append(c.Names, a.Text)
		}
	case PersonDocument:
		c.Names = append(c.Names, d.Name)
		for _, a := range d.Aliases {
			if a.IsPublic {
				c.Names = append(c.Names, a.Name)
			}
		}
	case TermDocument:
		c.Names, c.Facet = []string{d.Name, d.DisplayName}, d.Facet
	case CollectionDocument:
		c.Names, c.URL = []string{d.Name}, d.URL
	case EventDocument:
		c.Names = []string{d.Name}
	}

	return c
}

// DuplicateMatch is an existing entity a new one may duplicate: by slug, or a collection by ID, with its title or
// name. Blocked is set for a person who prefers no page, which a new person may not duplicate (D13).
type DuplicateMatch struct {
	EntityType   EntityType
	Slug         string
	CollectionID int64
	Name         string
	Matched      DuplicateField
	Blocked      bool
}

// DuplicateName is one name an existing entity is known by, Text, and the match it gives.
type DuplicateName struct {
	Match DuplicateMatch
	Text  string
}

// FindDuplicates returns the existing entities of names that candidate may duplicate, each once, in the order of
// their first matching name: a name whose DuplicateKey is a candidate name's, or a collection URL that normalizes
// to the candidate's. An entity matched by several names is blocked if any match is.
func FindDuplicates(candidate DuplicateCandidate, names []DuplicateName) []DuplicateMatch {
	keys := map[string]bool{}
	for _, name := range candidate.Names {
		if key := DuplicateKey(name); key != "" {
			keys[key] = true
		}
	}
	var url string
	if candidate.URL != nil {
		url, _ = NormalizeURL(*candidate.URL)
	}

	matches := []DuplicateMatch{}
	index := map[DuplicateMatch]int{}
	for _, name := range names {
		if !duplicateNameMatches(name, keys, url) {
			continue
		}
		entity := DuplicateMatch{EntityType: name.Match.EntityType, Slug: name.Match.Slug,
			CollectionID: name.Match.CollectionID}
		if i, ok := index[entity]; ok {
			matches[i].Blocked = matches[i].Blocked || name.Match.Blocked
			continue
		}
		index[entity] = len(matches)
		matches = append(matches, name.Match)
	}

	return matches
}

// duplicateNameMatches is whether name matches the candidate names with keys, or its URL url.
func duplicateNameMatches(name DuplicateName, keys map[string]bool, url string) bool {
	if name.Match.Matched == DuplicateFieldURL {
		normalized, err := NormalizeURL(name.Text)
		return err == nil && url != "" && normalized == url
	}

	return keys[DuplicateKey(name.Text)]
}
