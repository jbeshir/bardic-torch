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
//     set it to something else replaced it; so is a scalar source (a person's status source, a piece's
//     conservation source) withheld because it is a hidden link;
//   - a no-page person's profile is copied whole from base, ignoring what the author sent but their identity.
//
// A submitted element whose key equals a withheld one is kept, and the withheld one not put back; for an alias or
// a link this is returned as a hidden_alias_collision or hidden_link_collision flag at the element's path. A
// submitted performance is matched to the base performance it edits by its key, then by its description, year and
// piece (matchPerformances): a matched one keeps the base performance's key, and every other gets a fresh one from
// keys. A performance of base with withheld parts (an event or performers) that no submitted performance matches,
// because the author removed it, or edited it with a client that sends no keys, is returned as a
// hidden_data_conflict flag at its link's performances, and its withheld parts are not put back. Only moderators
// see the flags.
func RestoreDocument[D Document](id int64, submitted D, base *D, received ElementKeySet, vis EditorVisibility,
	keys KeySource,
) (D, []Flag) {
	if base == nil {
		return mapPerformances(submitted, func(_ DocLink, perf DocPerformance, _ *int64) DocPerformance {
			perf.Key = keys()

			return perf
		}), nil
	}
	p := &projector{id: id, vis: vis, kept: ElementKeySet{}, keys: keys,
		withheld: func(key string, hiddenNow bool) bool { return hiddenNow || !received[key] }}
	var out any
	var flags []Flag
	switch d := any(submitted).(type) {
	case PieceDocument:
		piece, pieceFlags := pieceWithheld.restore(p, d, any(*base).(PieceDocument))
		out, flags = p.restorePieceLicenceVersion(piece, any(*base).(PieceDocument)), pieceFlags
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
		return personWithheld.restoreSources(p, withIdentity(base, sub), base), nil
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

// holdsWithheld is whether the restore set holds a part of e, an element of the base: its projection changes it.
func holdsWithheld[E any](p *projector, e E, project func(*projector, E) E) bool {
	probe := *p
	probe.kept = ElementKeySet{}

	return !reflect.DeepEqual(project(&probe, e), e)
}

// restorePieceLink restores the parts of a link of a piece, and reports whether a performance of base with
// withheld parts matches none of sub's.
func (p *projector) restorePieceLink(sub, base DocPieceLink) (DocPieceLink, bool) {
	performances, dropped := restorePerformances(p, base.URL, sub.Performances, base.Performances,
		func(*DocPerformance) bool { return true }, func(perf *DocPerformance) *DocPerformance { return perf },
		func(*DocPerformance) *int64 { return nil },
		func(q *projector, perf DocPerformance) DocPerformance { return q.performance(base.URL, perf, nil) })

	return DocPieceLink{DocLink: p.restoreLinkFields(sub.DocLink, base.DocLink), Performances: performances}, dropped
}

// restorePersonLink restores the parts of a link of a person and its withheld performances, and reports whether a
// performance of base with withheld parts matches none of sub's. A performance of base in the restore set (of a
// piece that is not public) is put back whole, and is not one a submitted performance can match.
func (p *projector) restorePersonLink(sub, base DocPersonLink) (DocPersonLink, bool) {
	keys := personPerformanceKeys(base.URL)
	performances, dropped := restorePerformances(p, base.URL, sub.Performances, base.Performances,
		func(perf *DocPersonPerformance) bool {
			return !p.withheld(performanceKey(base.URL, perf.DocPerformance, perf.PieceID),
				p.hidden(p.performancePieceHidden(*perf)))
		},
		func(perf *DocPersonPerformance) *DocPerformance { return &perf.DocPerformance },
		func(perf *DocPersonPerformance) *int64 { return perf.PieceID },
		func(q *projector, perf DocPersonPerformance) DocPersonPerformance {
			return q.personPerformance(base.URL, perf)
		})
	performances, _ = restoreList(p, performances, base.Performances, keys, p.performancePieceHidden)

	return DocPersonLink{DocLink: p.restoreLinkFields(sub.DocLink, base.DocLink), Performances: performances}, dropped
}

// restorePerformances restores the submitted performances sub of the link with URL url onto base's, of which
// candidate tells those a submitted performance may match (matchPerformances). A submitted performance matching
// one gets back its withheld event and performers and keeps its key; any other is new and gets a fresh key. perf
// is an element's performance and pieceID its piece. It also reports whether a candidate with withheld parts
// (which project changes) matches none of sub, whose withheld parts are then dropped.
func restorePerformances[E any](p *projector, url string, sub, base []E, candidate func(*E) bool,
	perf func(*E) *DocPerformance, pieceID func(*E) *int64, project func(*projector, E) E,
) ([]E, bool) {
	ref := func(e *E, key string) performanceRef {
		return performanceRef{key: key, content: performanceKey(url, *perf(e), pieceID(e))}
	}
	subRefs := make([]performanceRef, 0, len(sub))
	for i := range sub {
		subRefs = append(subRefs, ref(&sub[i], perf(&sub[i]).Key))
	}
	baseRefs := make([]performanceRef, 0, len(base))
	candidates := make([]bool, 0, len(base))
	for i := range base {
		b := &base[i]
		baseRefs = append(baseRefs, ref(b, p.effectiveKey(url, *perf(b), pieceID(b))))
		candidates = append(candidates, candidate(b))
	}
	matches := matchPerformances(subRefs, baseRefs, candidates)
	matched := make([]bool, len(base))
	out := mapIndexed(sub, func(i int, e E) E {
		restored := perf(&e)
		j := matches[i]
		if j < 0 {
			restored.Key = p.keys()

			return e
		}
		matched[j] = true
		b := &base[j]
		*restored = p.restorePerformance(url, *restored, *perf(b), pieceID(b))
		restored.Key = perf(b).Key

		return e
	})
	for j := range base {
		if candidates[j] && !matched[j] && holdsWithheld(p, base[j], project) {
			return out, true
		}
	}

	return out, false
}

// performanceRef is what a performance is matched by: its key, and its content (performanceKey).
type performanceRef struct {
	key, content string
}

// matchPerformances returns, for each submitted performance of sub, the index in base of the performance it edits,
// or -1 for a new one; candidates says which of base may be matched, each at most once. A submitted key that one
// candidate has matches it, and is new if that candidate is already matched (a repeated key); a key no candidate
// has is new. One without a key, or with a key two candidates share, matches the first unmatched candidate with
// its content, as before keys existed: a client that does not round-trip keys edits by content.
func matchPerformances(sub, base []performanceRef, candidates []bool) []int {
	byKey := map[string][]int{}
	for j, b := range base {
		if candidates[j] {
			byKey[b.key] = append(byKey[b.key], j)
		}
	}
	matches := make([]int, len(sub))
	taken := make([]bool, len(base))
	var byContent []int
	for i, s := range sub {
		matches[i] = -1
		js := byKey[s.key]
		switch {
		case s.key == "" || len(js) > 1:
			byContent = append(byContent, i)
		case len(js) == 1 && !taken[js[0]]:
			matches[i], taken[js[0]] = js[0], true
		}
	}
	for _, i := range byContent {
		if j := firstByContent(sub[i], base, candidates, taken); j >= 0 {
			matches[i], taken[j] = j, true
		}
	}

	return matches
}

// firstByContent is the index of the first candidate of base not taken with s's content, or -1 for none.
func firstByContent(s performanceRef, base []performanceRef, candidates, taken []bool) int {
	for j, b := range base {
		if candidates[j] && !taken[j] && b.content == s.content {
			return j
		}
	}

	return -1
}

// mapIndexed returns f of each element of list and its index, nil for nil.
func mapIndexed[E any](list []E, f func(int, E) E) []E {
	if list == nil {
		return nil
	}
	out := make([]E, 0, len(list))
	for i, e := range list {
		out = append(out, f(i, e))
	}

	return out
}

// restoreLinkFields restores the collection of a link.
func (p *projector) restoreLinkFields(sub, base DocLink) DocLink {
	out := sub
	out.CollectionID = p.restoreRef(sub.CollectionID, base.CollectionID,
		func(id int64) string { return collectionKey(base.URL, id) }, p.collectionVisible)

	return out
}

// restorePerformance restores the event and performers of a performance of the link with URL url, onto base, its
// performance in the base, of piece pieceID.
func (p *projector) restorePerformance(url string, sub, base DocPerformance, pieceID *int64) DocPerformance {
	out := sub
	out.EventID = p.restoreRef(sub.EventID, base.EventID,
		func(id int64) string { return eventKey(url, base, pieceID, id) }, p.eventVisible)
	out.PerformerIDs, _ = restoreList(p, sub.PerformerIDs, base.PerformerIDs, performerKeys(url, base, pieceID),
		func(id int64) bool { return !p.personVisible(id) })

	return out
}

// restorePieceLicenceVersion puts back into sub the licence version of base if the author never received it (a
// draft whose client did not send it: EditorFieldsSent) and sub still has a Creative Commons licence, which a
// version may only accompany. It is public, so nothing withholds it otherwise.
func (p *projector) restorePieceLicenceVersion(sub, base PieceDocument) PieceDocument {
	out := sub
	if p.withheld(licenceVersionKey, false) && sub.LicenceVersion == nil && sub.Licence != nil &&
		sub.Licence.IsCreativeCommons() {
		out.LicenceVersion = base.LicenceVersion
	}

	return out
}
