package domain

import (
	"fmt"
	"slices"
)

// RestoreDocument puts back into a submitted document of entity id what was withheld from its author (D5). base is
// the full document of the draft's own base revision, nil for a new entity, and received the keys of the
// projection the author was given. The restore set is every element of base the current state withholds from the
// author, and every one whose key the author did not receive (it was withheld when the draft was made):
//   - an element of a list is put back after its nearest preceding base element the result holds, or first; an
//     element the author removed after receiving it stays removed, unless it is withheld now;
//   - a reference set to null in the projection is restored if the submitted value is still null: an author who
//     set it to something else replaced it;
//   - a no-page person's profile is copied whole from base, ignoring what the author sent but their identity;
//   - preferences are copied whole from base, for every actor, since no editor document holds them.
//
// A submitted element whose key equals a withheld one is kept, and the withheld one not put back; for an alias or
// a link this is returned as a hidden_alias_collision or hidden_link_collision flag at the element's path, which
// only moderators see.
func RestoreDocument[D Document](id int64, submitted D, base *D, received ElementKeySet, vis EditorVisibility) (
	D, []Flag,
) {
	if base == nil {
		return submitted, nil
	}
	p := &projector{id: id, vis: vis, kept: ElementKeySet{},
		withheld: func(key string, hiddenNow bool) bool { return hiddenNow || !received[key] }}
	var out any
	var flags []Flag
	switch d := any(submitted).(type) {
	case PieceDocument:
		out, flags = p.restorePiece(d, any(*base).(PieceDocument))
	case PersonDocument:
		out, flags = p.restorePerson(d, any(*base).(PersonDocument))
	case TermDocument:
		out = p.restoreTerm(d, any(*base).(TermDocument))
	case CollectionDocument:
		out = p.restoreCollection(d, any(*base).(CollectionDocument))
	case EventDocument:
		out = d
	}

	return out.(D), flags
}

// restoreList puts back into sub each element of base in the restore set, of which hidden tells whether the
// current state withholds one. It returns the result and the indexes in it of the submitted elements that
// collided with one.
func restoreList[E any](p *projector, sub, base []E, keys func([]E) []string, hidden func(E) bool) ([]E, []int) {
	out, outKeys := slices.Clone(sub), keys(sub)
	baseKeys := keys(base)
	var collided []string
	for i, e := range base {
		key := baseKeys[i]
		if !p.withheld(key, p.hidden(hidden(e))) {
			continue
		}
		if slices.Contains(outKeys, key) {
			collided = append(collided, key)

			continue
		}
		at := insertionIndex(outKeys, baseKeys[:i])
		out, outKeys = slices.Insert(out, at, e), slices.Insert(outKeys, at, key)
	}
	indexes := make([]int, 0, len(collided))
	for _, key := range collided {
		indexes = append(indexes, slices.Index(outKeys, key))
	}

	return out, indexes
}

// collisionFlags are the flags with code for the elements of the list at path at indexes.
func collisionFlags(code FlagCode, path string, indexes []int) []Flag {
	flags := make([]Flag, 0, len(indexes))
	for _, i := range indexes {
		flags = append(flags, Flag{Code: code, Path: fmt.Sprintf("%s/%d", path, i)})
	}

	return flags
}

// restoreRef restores a reference set to null in the projection, with key key, if sub is still null.
func (p *projector) restoreRef(sub, base *int64, key func(int64) string, visible func(int64) bool) *int64 {
	if sub == nil && base != nil && p.withheld(key(*base), p.hidden(!visible(*base))) {
		return base
	}

	return sub
}

func (p *projector) restorePiece(sub, base PieceDocument) (PieceDocument, []Flag) {
	out := sub
	out.Preferences = base.Preferences
	out.Terms, _ = restoreList(p, sub.Terms, base.Terms, elementKeys(termElementKey),
		func(t DocPieceTerm) bool { return !p.termVisible(t.TermID) })
	out.Relations, _ = restoreList(p, sub.Relations, base.Relations, elementKeys(pieceRelationKey),
		func(r DocPieceRelation) bool { return !p.pieceVisible(r.ToPieceID) })
	out.Credits, _ = restoreList(p, sub.Credits, base.Credits, creditKeys,
		p.creditHidden)
	linkKeys := elementKeys(func(l DocPieceLink) string { return linkElementKey(l.URL) })
	links, collided := restoreList(p, sub.Links, base.Links, linkKeys,
		func(l DocPieceLink) bool { return p.linkHidden(l.URL) })
	baseLinks := keyedBy(base.Links, linkKeys)
	for i, l := range links {
		if b, ok := baseLinks[linkElementKey(l.URL)]; ok {
			links[i] = p.restorePieceLink(l, b)
		}
	}
	out.Links = links

	return out, collisionFlags(FlagHiddenLinkCollision, "/links", collided)
}

// keyedBy is the first element of list with each key.
func keyedBy[E any](list []E, keys func([]E) []string) map[string]E {
	byKey := map[string]E{}
	for i, key := range keys(list) {
		if _, ok := byKey[key]; !ok {
			byKey[key] = list[i]
		}
	}

	return byKey
}

func (p *projector) restorePieceLink(sub, base DocPieceLink) DocPieceLink {
	performanceKeys := elementKeys(func(perf DocPerformance) string { return performanceKey(base.URL, perf, nil) })
	basePerformances := keyedBy(base.Performances, performanceKeys)

	return DocPieceLink{DocLink: p.restoreLinkFields(sub.DocLink, base.DocLink), Performances: mapList(sub.Performances,
		func(perf DocPerformance) DocPerformance {
			if b, ok := basePerformances[performanceKey(base.URL, perf, nil)]; ok {
				return p.restorePerformance(base.URL, perf, b, nil)
			}

			return perf
		})}
}

// restoreLinkFields restores the collection of a link.
func (p *projector) restoreLinkFields(sub, base DocLink) DocLink {
	out := sub
	out.CollectionID = p.restoreRef(sub.CollectionID, base.CollectionID,
		func(id int64) string { return collectionKey(base.URL, id) }, p.collectionVisible)

	return out
}

// restorePerformance restores the event and performers of a performance of the link with URL url.
func (p *projector) restorePerformance(url string, sub, base DocPerformance, pieceID *int64) DocPerformance {
	out := sub
	out.EventID = p.restoreRef(sub.EventID, base.EventID,
		func(id int64) string { return eventKey(url, base, pieceID, id) }, p.eventVisible)
	out.PerformerIDs, _ = restoreList(p, sub.PerformerIDs, base.PerformerIDs, performerKeys(url, base, pieceID),
		func(id int64) bool { return !p.personVisible(id) })

	return out
}

func (p *projector) restorePerson(sub, base PersonDocument) (PersonDocument, []Flag) {
	if p.withheld(profileKey, p.hidden(p.noPage(base))) {
		return withIdentity(base, sub), nil
	}
	out := sub
	out.Preferences = base.Preferences
	aliases, aliasCollisions := restoreList(p, sub.Aliases, base.Aliases,
		elementKeys(func(a DocPersonAlias) string { return aliasElementKey(a.Name) }),
		func(a DocPersonAlias) bool { return !a.IsPublic })
	out.Aliases = aliases
	out.AffiliationIDs, _ = restoreList(p, sub.AffiliationIDs, base.AffiliationIDs, elementKeys(affiliationElementKey),
		func(id int64) bool { return !p.termVisible(id) })
	out.Relations, _ = restoreList(p, sub.Relations, base.Relations, elementKeys(personRelationKey),
		func(r DocPersonRelation) bool { return !p.personVisible(r.ToPersonID) })
	linkKeys := elementKeys(func(l DocPersonLink) string { return linkElementKey(l.URL) })
	links, linkCollisions := restoreList(p, sub.Links, base.Links, linkKeys,
		func(l DocPersonLink) bool { return p.linkHidden(l.URL) })
	baseLinks := keyedBy(base.Links, linkKeys)
	for i, l := range links {
		if b, ok := baseLinks[linkElementKey(l.URL)]; ok {
			links[i] = p.restorePersonLink(l, b)
		}
	}
	out.Links = links

	return out, append(collisionFlags(FlagHiddenAliasCollision, "/aliases", aliasCollisions),
		collisionFlags(FlagHiddenLinkCollision, "/links", linkCollisions)...)
}

func (p *projector) restorePersonLink(sub, base DocPersonLink) DocPersonLink {
	keys := personPerformanceKeys(base.URL)
	performances, _ := restoreList(p, sub.Performances, base.Performances, keys, p.performancePieceHidden)
	basePerformances := keyedBy(base.Performances, keys)

	return DocPersonLink{DocLink: p.restoreLinkFields(sub.DocLink, base.DocLink), Performances: mapList(performances,
		func(perf DocPersonPerformance) DocPersonPerformance {
			if b, ok := basePerformances[performanceKey(base.URL, perf.DocPerformance, perf.PieceID)]; ok {
				perf.DocPerformance = p.restorePerformance(base.URL, perf.DocPerformance, b.DocPerformance, perf.PieceID)
			}

			return perf
		})}
}

func (p *projector) restoreTerm(sub, base TermDocument) TermDocument {
	out := sub
	out.ParentID = p.restoreRef(sub.ParentID, base.ParentID,
		func(id int64) string { return elementKey("parent", id) }, p.termVisible)
	out.RelatedAffiliationID = p.restoreRef(sub.RelatedAffiliationID, base.RelatedAffiliationID,
		func(id int64) string { return elementKey("related_affiliation", id) }, p.termVisible)

	return out
}

func (p *projector) restoreCollection(sub, base CollectionDocument) CollectionDocument {
	out := sub
	out.RegionTermID = p.restoreRef(sub.RegionTermID, base.RegionTermID,
		func(id int64) string { return elementKey("region", id) }, p.termVisible)
	out.ParentID = p.restoreRef(sub.ParentID, base.ParentID,
		func(id int64) string { return elementKey("collection_parent", id) }, p.collectionVisible)

	return out
}
