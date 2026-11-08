package domain

import (
	"fmt"
	"reflect"
	"slices"
)

// RestoreDocument puts back into a submitted document of entity id what was withheld from its author
// (docs/PLAN.md §5.1, "Restore on submit"). base is the full document of the draft's own base revision, nil for a
// new entity, and received the keys of the projection the author was given. The restore set is every element of
// base the current state withholds from the author, and every one whose key the author did not receive (it was
// withheld when the draft was made):
//   - an element of a list is put back after its nearest preceding base element the result holds, or first; an
//     element the author removed after receiving it stays removed, unless it is withheld now (a preference is an
//     element, keyed by dimension, so one whose source is a hidden link is put back, and so is every preference
//     of a draft whose client never sent the preferences: EditorFieldsSent);
//   - a reference set to null in the projection is restored if the submitted value is still null: an author who
//     set it to something else replaced it;
//   - a no-page person's profile is copied whole from base, ignoring what the author sent but their identity.
//
// A submitted element whose key equals a withheld one is kept, and the withheld one not put back; for an alias or
// a link this is returned as a hidden_alias_collision or hidden_link_collision flag at the element's path. A
// performance is matched by its description, year and piece: a performance of base with withheld parts (an event
// or performers) that no submitted performance matches, because the author edited or removed it, is returned as a
// hidden_data_conflict flag at its link's performances, and its withheld parts are not put back. Only moderators
// see the flags.
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
		out, flags = pieceWithheld.restore(p, d, any(*base).(PieceDocument))
	case PersonDocument:
		out, flags = p.restorePerson(d, any(*base).(PersonDocument))
	case TermDocument:
		out, flags = termWithheld.restore(p, d, any(*base).(TermDocument))
	case CollectionDocument:
		out, flags = collectionWithheld.restore(p, d, any(*base).(CollectionDocument))
	case EventDocument:
		out, flags = eventWithheld.restore(p, d, any(*base).(EventDocument))
	}

	return out.(D), flags
}

func (p *projector) restorePerson(sub, base PersonDocument) (PersonDocument, []Flag) {
	if p.withheld(profileKey, p.hidden(p.noPage(base))) {
		return withIdentity(base, sub), nil
	}

	return personWithheld.restore(p, sub, base)
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

// dropsWithheld is whether an element of base with withheld parts has a key none of list has: the author removed it
// or changed what its key is made of, and the restore cannot tell which, so cannot put its withheld parts back.
// project is the projection of such an element.
func dropsWithheld[E any](p *projector, base, list []E, keys func([]E) []string, project func(*projector, E) E) bool {
	kept := keys(list)
	for i, key := range keys(base) {
		if !slices.Contains(kept, key) && holdsWithheld(p, base[i], project) {
			return true
		}
	}

	return false
}

// holdsWithheld is whether the restore set holds a part of e, an element of the base: its projection changes it.
func holdsWithheld[E any](p *projector, e E, project func(*projector, E) E) bool {
	probe := *p
	probe.kept = ElementKeySet{}

	return !reflect.DeepEqual(project(&probe, e), e)
}

// restorePieceLink restores the parts of a link of a piece, and reports whether a performance of base with
// withheld parts matches none of sub's.
func (p *projector) restorePieceLink(sub, base DocPieceLink) (DocPieceLink, bool) {
	keys := elementKeys(func(perf DocPerformance) string { return performanceKey(base.URL, perf, nil) })
	basePerformances := keyedBy(base.Performances, keys)
	dropped := dropsWithheld(p, base.Performances, sub.Performances, keys,
		func(q *projector, perf DocPerformance) DocPerformance { return q.performance(base.URL, perf, nil) })

	return DocPieceLink{DocLink: p.restoreLinkFields(sub.DocLink, base.DocLink), Performances: mapList(sub.Performances,
		func(perf DocPerformance) DocPerformance {
			if b, ok := basePerformances[performanceKey(base.URL, perf, nil)]; ok {
				return p.restorePerformance(base.URL, perf, b, nil)
			}

			return perf
		})}, dropped
}

// restorePersonLink restores the parts of a link of a person and its withheld performances, and reports whether a
// performance of base with withheld parts matches none of sub's.
func (p *projector) restorePersonLink(sub, base DocPersonLink) (DocPersonLink, bool) {
	keys := personPerformanceKeys(base.URL)
	performances, _ := restoreList(p, sub.Performances, base.Performances, keys, p.performancePieceHidden)
	basePerformances := keyedBy(base.Performances, keys)
	dropped := dropsWithheld(p, base.Performances, performances, keys,
		func(q *projector, perf DocPersonPerformance) DocPersonPerformance {
			return q.personPerformance(base.URL, perf)
		})

	return DocPersonLink{DocLink: p.restoreLinkFields(sub.DocLink, base.DocLink), Performances: mapList(performances,
		func(perf DocPersonPerformance) DocPersonPerformance {
			if b, ok := basePerformances[performanceKey(base.URL, perf.DocPerformance, perf.PieceID)]; ok {
				perf.DocPerformance = p.restorePerformance(base.URL, perf.DocPerformance, b.DocPerformance, perf.PieceID)
			}

			return perf
		})}, dropped
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
