package domain

import "maps"

// AuthorDocument returns what the author of a submitted document of entity id is shown of it, by provenance:
// every element whose key is not in received, which the author typed, is kept whatever its hidden state; every
// element whose key is in received came from the projection, and is withheld as the current state withholds it
// from the author. So nothing the author typed vanishes because it equals hidden data, and nothing they received
// survives a later takedown. The same document is the starting point of an edit of a pending revision, and the
// preview of a draft.
func AuthorDocument[D Document](id int64, submitted D, received ElementKeySet, vis EditorVisibility) D {
	p := &projector{id: id, vis: vis, kept: ElementKeySet{},
		withheld: func(key string, hiddenNow bool) bool { return hiddenNow && received[key] }}

	return projectDocument(p, submitted)
}

// AuthorVisibility returns the visibility the views of an author document of entity entityID are built with:
// public, plus the author's own pending new terms and persons, the links the author typed (shown as typed), the
// public aliases the author typed and, for a person, the name the document gives them. A typed alias that is not
// public is never shown, not even to its author.
func AuthorVisibility[D Document](authorDoc D, received ElementKeySet, public SnapshotVisibility, own OwnPendingRefs,
	entityID int64,
) SnapshotVisibility {
	vis := public
	vis.Persons = withEntries(public.Persons, own.Persons)
	vis.Terms = withEntries(public.Terms, own.Terms)
	vis.VisibleLinks = maps.Clone(public.VisibleLinks)
	if vis.VisibleLinks == nil {
		vis.VisibleLinks = map[string]string{}
	}
	for _, url := range linkURLs(authorDoc) {
		normalized, err := NormalizeURL(url)
		if err == nil && !received[linkElementKey(url)] {
			vis.VisibleLinks[normalized] = url
		}
	}
	if d, ok := any(authorDoc).(PersonDocument); ok {
		aliases := withEntries(public.PublicAliases[entityID], map[string]bool{d.Name: true})
		for _, a := range d.Aliases {
			if a.IsPublic && !received[aliasElementKey(a.Name)] {
				aliases[a.Name] = true
			}
		}
		vis.PublicAliases = withEntries(public.PublicAliases, map[int64]map[string]bool{entityID: aliases})
	}

	return vis
}

// withEntries returns a new map with the entries of m and then of add.
func withEntries[K comparable, V any](m, add map[K]V) map[K]V {
	out := make(map[K]V, len(m)+len(add))
	maps.Copy(out, m)
	maps.Copy(out, add)

	return out
}

// linkURLs are the URLs of a document's links.
func linkURLs[D Document](doc D) []string {
	var urls []string
	switch d := any(doc).(type) {
	case PieceDocument:
		for _, l := range d.Links {
			urls = append(urls, l.URL)
		}
	case PersonDocument:
		for _, l := range d.Links {
			urls = append(urls, l.URL)
		}
	}

	return urls
}
