package domain

import "fmt"

// The editable document (docs/PLAN.md §5.1, "Editable projection"): a contributor never receives hidden data, and
// the server puts it back when their document returns. EditableDocument withholds from a document every element
// the caller may not see; the keys of what it shows are the received keys of a draft made from it. RestoreDocument
// puts back into a submitted document what was withheld from its base. AuthorDocument (author.go) shows the author
// what they submitted: what they typed, and what they received as far as the current state still shows it.

// EditorVisibility is what decides what a caller sees of a document: the current visibility of the entities and
// links it references, the caller's own pending new terms and persons, the hidden links among its links and
// sources, and the caller's effective role. An interactive moderator sees everything.
type EditorVisibility struct {
	SnapshotVisibility
	OwnPending OwnPendingRefs
	// HiddenLinkURLs are the normalized URLs of the hidden links among the document's links and sources.
	HiddenLinkURLs map[string]bool
	Role           Role
}

// OwnPendingRefs are the caller's pending new terms and persons, which they may see and reference, as their
// views show them: under their placeholder slugs, with the names of their pending documents.
type OwnPendingRefs struct {
	Terms   map[int64]TermSummary
	Persons map[int64]PersonSummary
}

// ElementKeySet is a set of element keys: the elements of a document a caller was shown (a draft's received keys).
type ElementKeySet map[string]bool

// profileKey is the element key of a person's profile, everything but their identity, which is withheld whole
// from a contributor when the person prefers no page.
const profileKey = "profile"

// projector walks a document and decides, element by element, whether the element is withheld: withheld is given
// the element's key and whether the current state withholds it from the caller. Each element it does not withhold
// is recorded in kept.
type projector struct {
	id       int64
	vis      EditorVisibility
	withheld func(key string, hiddenNow bool) bool
	kept     ElementKeySet
	// keys issues the keys of new performances when restoring; nil when projecting.
	keys KeySource
}

// hidden is whether the current state withholds from the caller what cond describes: never from a moderator.
func (p *projector) hidden(cond bool) bool {
	return p.vis.Role != RoleModerator && cond
}

func (p *projector) personVisible(id int64) bool {
	_, public := p.vis.Persons[id]
	_, own := p.vis.OwnPending.Persons[id]

	return public || own
}

func (p *projector) termVisible(id int64) bool {
	_, public := p.vis.Terms[id]
	_, own := p.vis.OwnPending.Terms[id]

	return public || own
}

func (p *projector) pieceVisible(id int64) bool {
	_, ok := p.vis.Pieces[id]

	return ok
}

func (p *projector) collectionVisible(id int64) bool {
	_, ok := p.vis.Collections[id]

	return ok
}

func (p *projector) eventVisible(id int64) bool {
	_, ok := p.vis.Events[id]

	return ok
}

// creditHidden is whether a credit is withheld: it names a person the caller may not see.
func (p *projector) creditHidden(c DocCredit) bool {
	return c.PersonID != nil && !p.personVisible(*c.PersonID)
}

// linkHidden is whether a link is withheld: it is hidden now, or its URL does not normalize (fail closed).
func (p *projector) linkHidden(url string) bool {
	normalized, err := NormalizeURL(url)

	return err != nil || p.vis.HiddenLinkURLs[normalized]
}

// noPage is whether the person of document d prefers no page, now or in d.
func (p *projector) noPage(d PersonDocument) bool {
	_, now := p.vis.NoPagePersons[p.id]

	return now || d.Status == PersonStatusPrefersNoPage
}

// The element keys of the elements a projection may withhold.
func aliasElementKey(name string) string         { return elementKey("alias", CollationKey(name)) }
func linkElementKey(url string) string           { return elementKey("link", linkKey(url)) }
func termElementKey(t DocPieceTerm) string       { return elementKey("term", t.TermID) }
func affiliationElementKey(id int64) string      { return elementKey("affiliation", id) }
func pieceRelationKey(r DocPieceRelation) string { return elementKey("relation", r.ToPieceID, r.Type) }

func personRelationKey(r DocPersonRelation) string {
	return elementKey("relation", r.ToPersonID, r.Type)
}

// performanceKey identifies a performance of the link with URL url: its description, year and piece. Its event is
// not part of it, since the event is a reference a projection may set to null.
func performanceKey(url string, p DocPerformance, pieceID *int64) string {
	return elementKey("performance", linkKey(url), p.DescriptionVerbatim, p.Year, pieceID)
}

// mapList returns f of each element of list, nil for nil.
func mapList[E any](list []E, f func(E) E) []E {
	if list == nil {
		return nil
	}
	out := make([]E, 0, len(list))
	for _, e := range list {
		out = append(out, f(e))
	}

	return out
}

// elementKeys returns a function giving the keys of a list's elements by key.
func elementKeys[E any](key func(E) string) func([]E) []string {
	return func(list []E) []string {
		keys := make([]string, 0, len(list))
		for _, e := range list {
			keys = append(keys, key(e))
		}

		return keys
	}
}

// creditKeys are the keys of credits: by person (none for a credit naming no one), role and part, and occurrence
// among the credits with those. A credit naming no one is never withheld, but anchors the credits restored after it.
func creditKeys(credits []DocCredit) []string {
	keys := make([]string, 0, len(credits))
	occurrences := map[string]int{}
	for _, c := range credits {
		key := elementKey("credit", c.PersonID, c.Role, c.Part)
		keys = append(keys, fmt.Sprintf("%s#%d", key, occurrences[key]))
		occurrences[key]++
	}

	return keys
}

// project returns the elements of list the projector does not withhold, of which hidden tells whether the current
// state withholds one.
func project[E any](p *projector, list []E, keys func([]E) []string, hidden func(E) bool) []E {
	if list == nil {
		return nil
	}
	out := make([]E, 0, len(list))
	for i, key := range keys(list) {
		if p.withheld(key, p.hidden(hidden(list[i]))) {
			continue
		}
		p.kept[key] = true
		out = append(out, list[i])
	}

	return out
}

// ref returns a reference with key key, or nil if the projector withholds it; visible is whether the caller may see
// the referenced entity now.
func (p *projector) ref(id *int64, key string, visible bool) *int64 {
	if id == nil || p.withheld(key, p.hidden(!visible)) {
		return nil
	}
	p.kept[key] = true

	return id
}

// pieceLink is the projection of a link of a piece the projector shows.
func (p *projector) pieceLink(l DocPieceLink) DocPieceLink {
	return DocPieceLink{DocLink: p.link(l.DocLink), Performances: mapList(l.Performances,
		func(perf DocPerformance) DocPerformance { return p.performance(l.URL, perf, nil) })}
}

// link is the projection of a link the projector shows: its collection withheld if the caller may not see it.
func (p *projector) link(l DocLink) DocLink {
	out := l
	if l.CollectionID != nil {
		out.CollectionID = p.ref(l.CollectionID, collectionKey(l.URL, *l.CollectionID),
			p.collectionVisible(*l.CollectionID))
	}

	return out
}

func collectionKey(url string, id int64) string { return elementKey("collection", linkKey(url), id) }

func eventKey(url string, perf DocPerformance, pieceID *int64, id int64) string {
	return elementKey("event", performanceKey(url, perf, pieceID), id)
}

func performerKeys(url string, perf DocPerformance, pieceID *int64) func([]int64) []string {
	return elementKeys(func(id int64) string { return elementKey("performer", performanceKey(url, perf, pieceID), id) })
}

// performance is the projection of a performance of the link with URL url: its event and performers withheld if
// the caller may not see them.
func (p *projector) performance(url string, perf DocPerformance, pieceID *int64) DocPerformance {
	out := perf
	if perf.EventID != nil {
		out.EventID = p.ref(perf.EventID, eventKey(url, perf, pieceID, *perf.EventID), p.eventVisible(*perf.EventID))
	}
	out.PerformerIDs = project(p, perf.PerformerIDs, performerKeys(url, perf, pieceID),
		func(id int64) bool { return !p.personVisible(id) })

	return out
}

// personLink is the projection of a link of a person the projector shows: its performances of pieces that are not
// public withheld, and the parts of the others the caller may not see.
func (p *projector) personLink(l DocPersonLink) DocPersonLink {
	performances := project(p, l.Performances, personPerformanceKeys(l.URL), p.performancePieceHidden)

	return DocPersonLink{DocLink: p.link(l.DocLink), Performances: mapList(performances,
		func(perf DocPersonPerformance) DocPersonPerformance { return p.personPerformance(l.URL, perf) })}
}

// personPerformance is the projection of a performance of a person's link with URL url the projector shows.
func (p *projector) personPerformance(url string, perf DocPersonPerformance) DocPersonPerformance {
	return DocPersonPerformance{PieceID: perf.PieceID,
		DocPerformance: p.performance(url, perf.DocPerformance, perf.PieceID)}
}

func personPerformanceKeys(url string) func([]DocPersonPerformance) []string {
	return elementKeys(func(perf DocPersonPerformance) string {
		return performanceKey(url, perf.DocPerformance, perf.PieceID)
	})
}

// performancePieceHidden is whether a performance of a person's link is withheld: it is of a piece that is not
// public, which its description may name.
func (p *projector) performancePieceHidden(perf DocPersonPerformance) bool {
	return perf.PieceID != nil && !p.pieceVisible(*perf.PieceID)
}

// identityOnly is the projection of a person who prefers no page: their identity, with the rest of the profile
// as a new person's.
func identityOnly(d PersonDocument) PersonDocument {
	return withIdentity(DefaultPersonDocument(), d)
}

// projectDocument is the projector's projection of doc: the fields of its type's table withheld
// (withheld_fields.go); a no-page person's profile is withheld whole.
func projectDocument[D Document](p *projector, doc D) D {
	var out any
	switch d := any(doc).(type) {
	case PieceDocument:
		out = pieceWithheld.project(p, d)
	case PersonDocument:
		out = p.person(d)
	case TermDocument:
		out = termWithheld.project(p, d)
	case CollectionDocument:
		out = collectionWithheld.project(p, d)
	case EventDocument:
		out = eventWithheld.project(p, d)
	}

	return out.(D)
}

// person is the projection of a person: their identity only if the caller may not see their profile, with a
// withheld status source.
func (p *projector) person(d PersonDocument) PersonDocument {
	if p.withheld(profileKey, p.hidden(p.noPage(d))) {
		return personWithheld.projectSources(p, identityOnly(d))
	}
	p.kept[profileKey] = true

	return personWithheld.project(p, d)
}

// EditableDocument returns the document of entity id (0 for a new one) as the caller may edit it: without every
// element the current state withholds from them (hidden aliases and links, preferences, status and conservation
// sources that are a hidden link, references to entities they may not see, a no-page person's profile), each
// performance with its key, derived
// for one stored without (key_source.go). It also returns the keys of the elements it shows, the received keys of a
// draft made from it.
func EditableDocument[D Document](id int64, doc D, vis EditorVisibility) (D, ElementKeySet) {
	p := &projector{id: id, vis: vis, kept: ElementKeySet{},
		withheld: func(_ string, hiddenNow bool) bool { return hiddenNow }}
	out := withPerformanceKeys(p, projectDocument(p, doc))

	return out, p.kept
}

// effectiveKey is the key of a performance of the link with URL url, of piece pieceID if not nil, as the projector's
// caller is given it: its own, or for a performance stored without one, its derived key over what they are shown.
func (p *projector) effectiveKey(url string, perf DocPerformance, pieceID *int64) string {
	if perf.Key != "" {
		return perf.Key
	}

	return derivedPerformanceKey(url, perf, p.shownPiece(pieceID))
}

// shownPiece is pieceID if the caller is shown that piece, else 0.
func (p *projector) shownPiece(pieceID *int64) *int64 {
	if pieceID == nil || !p.hidden(!p.pieceVisible(*pieceID)) {
		return pieceID
	}

	return new(int64)
}

// withPerformanceKeys is doc with each performance's effective key (effectiveKey).
func withPerformanceKeys[D Document](p *projector, doc D) D {
	return mapPerformances(doc, func(l DocLink, perf DocPerformance, pieceID *int64) DocPerformance {
		perf.Key = p.effectiveKey(l.URL, perf, pieceID)

		return perf
	})
}

// mapPerformances is doc with f of each performance of its links, given its link and its piece (nil for a link of a
// piece); a document without links is doc.
func mapPerformances[D Document](doc D, f func(l DocLink, perf DocPerformance, pieceID *int64) DocPerformance) D {
	var out any = doc
	switch d := any(doc).(type) {
	case PieceDocument:
		d.Links = mapList(d.Links, func(l DocPieceLink) DocPieceLink {
			l.Performances = mapList(l.Performances, func(perf DocPerformance) DocPerformance {
				return f(l.DocLink, perf, nil)
			})

			return l
		})
		out = d
	case PersonDocument:
		d.Links = mapList(d.Links, func(l DocPersonLink) DocPersonLink {
			l.Performances = mapList(l.Performances, func(perf DocPersonPerformance) DocPersonPerformance {
				perf.DocPerformance = f(l.DocLink, perf.DocPerformance, perf.PieceID)

				return perf
			})

			return l
		})
		out = d
	}

	return out.(D)
}
