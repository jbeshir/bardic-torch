package domain

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
)

// The fields of each document type a projection may withhold from a caller, declared once: EditableDocument walks
// these tables to withhold, and RestoreDocument walks the same tables to put back what was withheld, so a field is
// always withheld and restored by the same keys and the same rule (docs/PLAN.md §5.1, "Editable projection" and
// "Restore on submit"). One thing is handled outside the tables: a no-page person's profile, which is withheld
// whole (identityOnly); their status source, part of their identity, is still withheld by the table's sources.

// withheldFields are the fields of a document of type D a projection may withhold: lists, whose elements it may
// withhold, references, which it may set to null, and scalar sources, which it may set to null too.
type withheldFields[D any] struct {
	lists   []withheldList[D]
	refs    []refField[D]
	sources []sourceField[D]
}

// withheldList is a list field of a document of type D, whatever its element type.
type withheldList[D any] interface {
	// jsonName is the field's JSON name.
	jsonName() string
	// project withholds the field's elements the projector withholds from out.
	project(p *projector, out *D)
	// restore puts back into out the field's elements of base in the restore set, and returns the flags of what
	// it could not put back.
	restore(p *projector, out *D, base D) []Flag
}

// listField is a list field of a document of type D with elements of type E.
type listField[D, E any] struct {
	name string
	list func(*D) *[]E
	keys func([]E) []string
	// hidden is whether the current state withholds an element from the caller.
	hidden func(*projector, E) bool
	// collision is the flag of a submitted element whose key equals that of a withheld element, which the restore
	// keeps instead of the withheld one; empty for none.
	collision FlagCode
	// parts are the withholdable parts of the elements, nil if they have none.
	parts *elementParts[E]
}

// elementParts are the parts of a list's elements a projection may withhold from an element it shows.
type elementParts[E any] struct {
	// project withholds the parts of an element the projector withholds.
	project func(*projector, E) E
	// restore puts back into an element sub the parts of base, its element in the base (the zero element if the base
	// has none), in the restore set. It reports whether a part it could not put back was dropped.
	restore func(p *projector, sub, base E) (E, bool)
	// droppedPath is the path, under the element, of the hidden_data_conflict flag of a dropped part.
	droppedPath string
}

func (f listField[D, E]) jsonName() string { return f.name }

func (f listField[D, E]) project(p *projector, out *D) {
	list := f.list(out)
	*list = project(p, *list, f.keys, func(e E) bool { return f.hidden(p, e) })
	if f.parts != nil {
		*list = mapList(*list, func(e E) E { return f.parts.project(p, e) })
	}
}

func (f listField[D, E]) restore(p *projector, out *D, base D) []Flag {
	list, baseList := f.list(out), *f.list(&base)
	restored, collided := restoreList(p, *list, baseList, f.keys, func(e E) bool { return f.hidden(p, e) })
	path := "/" + f.name
	var flags []Flag
	if f.collision != "" {
		flags = collisionFlags(f.collision, path, collided)
	}
	if f.parts != nil {
		byKey := keyedBy(baseList, f.keys)
		for i, key := range f.keys(restored) {
			// An element base does not have is restored onto the zero element: nothing is put back.
			b := byKey[key]
			var dropped bool
			restored[i], dropped = f.parts.restore(p, restored[i], b)
			if dropped {
				flags = append(flags, Flag{Code: FlagHiddenDataConflict,
					Path: fmt.Sprintf("%s/%d/%s", path, i, f.parts.droppedPath)})
			}
		}
	}
	*list = restored

	return flags
}

// refField is a reference field of a document of type D, to an entity of ID id, with the element key
// elementKey(key, id).
type refField[D any] struct {
	name string
	key  string
	ref  func(*D) **int64
	// visible is whether the caller may see the referenced entity now.
	visible func(*projector, int64) bool
}

func (f refField[D]) elementKey(id int64) string { return elementKey(f.key, id) }

func (f refField[D]) project(p *projector, out *D) {
	ref := f.ref(out)
	if *ref != nil {
		*ref = p.ref(*ref, f.elementKey(**ref), f.visible(p, **ref))
	}
}

func (f refField[D]) restore(p *projector, out *D, base D) {
	ref := f.ref(out)
	*ref = p.restoreRef(*ref, *f.ref(&base), f.elementKey, func(id int64) bool { return f.visible(p, id) })
}

// sourceField is a scalar source URL field of a document of type D, at path, with the element key
// elementKey("source", key): it is withheld (set to null) when the source is a hidden link now, or does not
// normalize (fail closed), the rule links and preferences have, so that a hidden link never reaches a contributor
// as a source. A submitted null is restored from the base when it was withheld; a submitted URL replaces it.
type sourceField[D any] struct {
	path   string
	key    string
	source func(*D) **string
}

func (f sourceField[D]) elementKey() string { return elementKey("source", f.key) }

func (f sourceField[D]) project(p *projector, out *D) {
	source := f.source(out)
	if *source == nil {
		return
	}
	if p.withheld(f.elementKey(), p.hidden(p.linkHidden(**source))) {
		*source = nil

		return
	}
	p.kept[f.elementKey()] = true
}

func (f sourceField[D]) restore(p *projector, out *D, base D) {
	source, baseSource := f.source(out), *f.source(&base)
	if *source == nil && baseSource != nil && p.withheld(f.elementKey(), p.hidden(p.linkHidden(*baseSource))) {
		restored := *baseSource
		*source = &restored
	}
}

// project withholds from d every field the projector withholds.
func (fs withheldFields[D]) project(p *projector, d D) D {
	out := d
	for _, f := range fs.lists {
		f.project(p, &out)
	}
	for _, f := range fs.refs {
		f.project(p, &out)
	}

	return fs.projectSources(p, out)
}

// projectSources withholds from d the scalar sources the projector withholds.
func (fs withheldFields[D]) projectSources(p *projector, d D) D {
	out := d
	for _, f := range fs.sources {
		f.project(p, &out)
	}

	return out
}

// restore puts back into sub every field of base in the restore set, and returns the restore's flags.
func (fs withheldFields[D]) restore(p *projector, sub, base D) (D, []Flag) {
	out := sub
	var flags []Flag
	for _, f := range fs.lists {
		flags = append(flags, f.restore(p, &out, base)...)
	}
	for _, f := range fs.refs {
		f.restore(p, &out, base)
	}

	return fs.restoreSources(p, out, base), flags
}

// restoreSources puts back into sub the scalar sources of base in the restore set that sub leaves null.
func (fs withheldFields[D]) restoreSources(p *projector, sub, base D) D {
	out := sub
	for _, f := range fs.sources {
		f.restore(p, &out, base)
	}

	return out
}

// withheldSourcePaths are the paths of the scalar sources of D that base holds and its projection, the editable
// document a contributor was given, does not: those the projection withheld.
func (fs withheldFields[D]) withheldSourcePaths(base, projection D) map[string]bool {
	paths := map[string]bool{}
	for _, f := range fs.sources {
		if *f.source(&base) != nil && *f.source(&projection) == nil {
			paths[f.path] = true
		}
	}

	return paths
}

// scalarSource is the value of a scalar source field at its path, with its element key, nil for null.
type scalarSource struct {
	path string
	key  string
	url  *string
}

// sourcesOf are the scalar sources of d, in the table's order.
func (fs withheldFields[D]) sourcesOf(d D) []scalarSource {
	sources := make([]scalarSource, 0, len(fs.sources))
	for _, f := range fs.sources {
		sources = append(sources, scalarSource{path: f.path, key: f.elementKey(), url: *f.source(&d)})
	}

	return sources
}

// names are the JSON names of the fields.
func (fs withheldFields[D]) names() []string {
	var names []string
	for _, f := range fs.lists {
		names = append(names, f.jsonName())
	}
	for _, f := range fs.refs {
		names = append(names, f.name)
	}

	return names
}

// linkField is the links field of a document whose links of type L have the URL url: a link is withheld if it is
// hidden, a submitted link with a hidden link's URL is a hidden_link_collision, and the parts of a link it shows
// (its collection and its performances' events and performers) are withheld by project and restored by restore.
func linkField[D, L any](list func(*D) *[]L, url func(L) string, project func(*projector, L) L,
	restore func(p *projector, sub, base L) (L, bool),
) listField[D, L] {
	return listField[D, L]{name: "links", list: list,
		keys:      elementKeys(func(l L) string { return linkElementKey(url(l)) }),
		hidden:    func(p *projector, l L) bool { return p.linkHidden(url(l)) },
		collision: FlagHiddenLinkCollision,
		parts:     &elementParts[L]{project: project, restore: restore, droppedPath: "performances"}}
}

// preferenceField is the preferences field of a document: a preference is withheld if its source is hidden now,
// or does not normalize (fail closed), the rule links have, so that a hidden link never reaches a contributor as a
// preference's source. A submitted preference of a withheld one's dimension replaces it.
func preferenceField[D any](list func(*D) *[]DocPreference) listField[D, DocPreference] {
	return listField[D, DocPreference]{name: "preferences", list: list, keys: elementKeys(preferenceElementKey),
		hidden: func(p *projector, pref DocPreference) bool { return p.linkHidden(pref.SourceURL) }}
}

var pieceWithheld = withheldFields[PieceDocument]{lists: []withheldList[PieceDocument]{
	listField[PieceDocument, DocPieceTerm]{name: "terms",
		list: func(d *PieceDocument) *[]DocPieceTerm { return &d.Terms }, keys: elementKeys(termElementKey),
		hidden: func(p *projector, t DocPieceTerm) bool { return !p.termVisible(t.TermID) }},
	listField[PieceDocument, DocPieceRelation]{name: "relations",
		list: func(d *PieceDocument) *[]DocPieceRelation { return &d.Relations }, keys: elementKeys(pieceRelationKey),
		hidden: func(p *projector, r DocPieceRelation) bool { return !p.pieceVisible(r.ToPieceID) }},
	listField[PieceDocument, DocCredit]{name: "credits",
		list: func(d *PieceDocument) *[]DocCredit { return &d.Credits }, keys: creditKeys,
		hidden: (*projector).creditHidden},
	linkField(func(d *PieceDocument) *[]DocPieceLink { return &d.Links }, func(l DocPieceLink) string { return l.URL },
		(*projector).pieceLink, (*projector).restorePieceLink),
	preferenceField(func(d *PieceDocument) *[]DocPreference { return &d.Preferences }),
}, sources: []sourceField[PieceDocument]{
	{path: "/conservation/source_url", key: "conservation",
		source: func(d *PieceDocument) **string { return &d.Conservation.SourceURL }},
}}

var personWithheld = withheldFields[PersonDocument]{lists: []withheldList[PersonDocument]{
	listField[PersonDocument, DocPersonAlias]{name: "aliases",
		list:   func(d *PersonDocument) *[]DocPersonAlias { return &d.Aliases },
		keys:   elementKeys(func(a DocPersonAlias) string { return aliasElementKey(a.Name) }),
		hidden: func(_ *projector, a DocPersonAlias) bool { return !a.IsPublic }, collision: FlagHiddenAliasCollision},
	listField[PersonDocument, int64]{name: "affiliation_ids",
		list: func(d *PersonDocument) *[]int64 { return &d.AffiliationIDs }, keys: elementKeys(affiliationElementKey),
		hidden: func(p *projector, id int64) bool { return !p.termVisible(id) }},
	listField[PersonDocument, DocPersonRelation]{name: "relations",
		list: func(d *PersonDocument) *[]DocPersonRelation { return &d.Relations }, keys: elementKeys(personRelationKey),
		hidden: func(p *projector, r DocPersonRelation) bool { return !p.personVisible(r.ToPersonID) }},
	linkField(func(d *PersonDocument) *[]DocPersonLink { return &d.Links }, func(l DocPersonLink) string { return l.URL },
		(*projector).personLink, (*projector).restorePersonLink),
	preferenceField(func(d *PersonDocument) *[]DocPreference { return &d.Preferences }),
}, sources: []sourceField[PersonDocument]{
	{path: "/status_source_url", key: "status",
		source: func(d *PersonDocument) **string { return &d.StatusSourceURL }},
}}

var termWithheld = withheldFields[TermDocument]{refs: []refField[TermDocument]{
	{name: "parent_id", key: "parent", ref: func(d *TermDocument) **int64 { return &d.ParentID },
		visible: (*projector).termVisible},
	{name: "related_affiliation_id", key: "related_affiliation",
		ref: func(d *TermDocument) **int64 { return &d.RelatedAffiliationID }, visible: (*projector).termVisible},
}}

var collectionWithheld = withheldFields[CollectionDocument]{refs: []refField[CollectionDocument]{
	{name: "region_term_id", key: "region", ref: func(d *CollectionDocument) **int64 { return &d.RegionTermID },
		visible: (*projector).termVisible},
	{name: "parent_id", key: "collection_parent", ref: func(d *CollectionDocument) **int64 { return &d.ParentID },
		visible: (*projector).collectionVisible},
}}

// eventWithheld is empty: an event document references nothing a projection withholds.
var eventWithheld = withheldFields[EventDocument]{}

// identityFields are the JSON names of the fields of a person document that a person who prefers no page has:
// everything else of their profile is withheld from contributors, and restored from the base.
var identityFields = []string{"schema_version", "name", "kind", "status", "status_date", "status_source_url", "is_stub"}

// withIdentity is profile with the identity fields of d.
func withIdentity(profile, d PersonDocument) PersonDocument {
	out := profile
	ov, dv := reflect.ValueOf(&out).Elem(), reflect.ValueOf(d)
	for i := range dv.NumField() {
		if slices.Contains(identityFields, jsonFieldName(dv.Type().Field(i))) {
			ov.Field(i).Set(dv.Field(i))
		}
	}

	return out
}

// jsonFieldName is the JSON name of a struct field.
func jsonFieldName(f reflect.StructField) string {
	name, _, _ := strings.Cut(f.Tag.Get("json"), ",")

	return name
}

// withheldSourcePaths are the paths of the scalar sources base holds that projection, the editable document of
// base a contributor was given, withheld; none when either is nil (a new entity).
func withheldSourcePaths[D Document](base, projection *D) map[string]bool {
	if base == nil || projection == nil {
		return nil
	}
	switch b := any(*base).(type) {
	case PieceDocument:
		return pieceWithheld.withheldSourcePaths(b, any(*projection).(PieceDocument))
	case PersonDocument:
		return personWithheld.withheldSourcePaths(b, any(*projection).(PersonDocument))
	}

	return nil
}

// documentSources are the scalar sources of doc: none for a document type without them.
func documentSources[D Document](doc D) []scalarSource {
	switch d := any(doc).(type) {
	case PieceDocument:
		return pieceWithheld.sourcesOf(d)
	case PersonDocument:
		return personWithheld.sourcesOf(d)
	}

	return nil
}

// sourceElement is an element of a document with a source URL outside its links, a preference or a scalar source,
// by its element key.
type sourceElement struct {
	key string
	url string
}

// sourceElements are the elements of doc with a source URL: its preferences and its scalar sources that are set.
func sourceElements[D Document](doc D) []sourceElement {
	var prefs []DocPreference
	switch d := any(doc).(type) {
	case PieceDocument:
		prefs = d.Preferences
	case PersonDocument:
		prefs = d.Preferences
	}
	elements := make([]sourceElement, 0, len(prefs))
	for _, p := range prefs {
		elements = append(elements, sourceElement{key: preferenceElementKey(p), url: p.SourceURL})
	}
	for _, s := range documentSources(doc) {
		if s.url != nil {
			elements = append(elements, sourceElement{key: s.key, url: *s.url})
		}
	}

	return elements
}

// receivedSourceKey is the key a projection records for the source of the element of key it shows, which is
// normalized to normalized: with it the author's views tell a source left as received from one the author edited
// (receivedAsSubmitted).
func receivedSourceKey(key, normalized string) string {
	return elementKey("received_source", key, normalized)
}

// withReceivedSources is kept with the receivedSourceKey of every source of doc, a projection, that normalizes.
func withReceivedSources[D Document](kept ElementKeySet, doc D) ElementKeySet {
	out := maps.Clone(kept)
	for _, e := range sourceElements(doc) {
		if normalized, err := NormalizeURL(e.url); err == nil {
			out[receivedSourceKey(e.key, normalized)] = true
		}
	}

	return out
}

// receivedAsSubmitted is received without the keys of the sources of submitted the author edited: a preference or
// scalar source whose key is received, but whose URL no longer normalizes to the one the projection showed, is the
// author's own, as one they typed (docs/PLAN.md §5.1, "Author's view"). A received key without a recorded URL (a
// projection made before they were recorded) stays received: it fails closed.
func receivedAsSubmitted[D Document](submitted D, received ElementKeySet) ElementKeySet {
	out := maps.Clone(received)
	for _, e := range sourceElements(submitted) {
		if received[e.key] && sourceEdited(e, received) {
			delete(out, e.key)
		}
	}

	return out
}

// sourceEdited is whether received records a URL for the source of e, and not the URL e now has.
func sourceEdited(e sourceElement, received ElementKeySet) bool {
	if normalized, err := NormalizeURL(e.url); err == nil && received[receivedSourceKey(e.key, normalized)] {
		return false
	}
	prefix := elementKey("received_source", e.key)
	for key := range received {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}

	return false
}
