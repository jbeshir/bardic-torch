package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
)

// FieldPath names what a diff found changed, or where a merge found a conflict (D9): a field by its JSON name
// ("title"), a group of fields by the group's name ("licensing", "conservation"), an element of a keyed list by its
// element key ("links[https://example.org/a]") and the order of a keyed list's elements ("links#order").
type FieldPath string

// RebaseConflict is a field, element or order that a revision and the current document both changed, differently,
// since their common base. Each value is the JSON of what base, revision and current hold there, null where the
// element is missing. A conflict may hold hidden aliases and links, so it is shown to moderators only.
type RebaseConflict struct {
	Path     FieldPath
	Base     json.RawMessage
	Revision json.RawMessage
	Current  json.RawMessage
}

// fieldKind is how a field is compared and merged.
type fieldKind int

const (
	// scalarField is one value: a scalar, a group of fields that is never split, or a list whose elements have no
	// identity (credits), merged as one value.
	scalarField fieldKind = iota
	// keyedField is a list whose elements are identified by a key, merged element by element and with its order.
	keyedField
	// setField is a list whose order does not matter, merged as a set of keys.
	setField
)

// docField is a field of a document type as diff and merge see it: the JSON names of the document fields it is
// made of (several for a group) and, for a list, the key of its elements. For a list whose element keys start with
// the internal ID of a referenced entity, ref is that entity's type (EditorConflicts names it by slug instead).
type docField struct {
	path  string
	names []string
	kind  fieldKind
	key   func(e reflect.Value) string
	ref   EntityType
}

// referencing is f with element keys that start with the ID of an entity of type t.
func (f docField) referencing(t EntityType) docField {
	out := f
	out.ref = t

	return out
}

func scalar(name string) docField { return docField{path: name, names: []string{name}} }

func group(path string, names ...string) docField { return docField{path: path, names: names} }

func keyed(name string, key func(reflect.Value) string) docField {
	return docField{path: name, names: []string{name}, kind: keyedField, key: key}
}

func set(name string, key func(reflect.Value) string) docField {
	return docField{path: name, names: []string{name}, kind: setField, key: key}
}

// The element keys of the lists (D9).
var (
	// keyOfValue keys a list of values (kinds, roles, IDs) by the value.
	keyOfValue = func(e reflect.Value) string { return fmt.Sprint(e.Interface()) }
	// keyOfName keys a list of names by how the collation compares them, as the UNIQUE indexes do.
	keyOfName       = func(e reflect.Value) string { return CollationKey(e.String()) }
	keyOfAliasText  = func(e reflect.Value) string { return CollationKey(e.FieldByName("Text").String()) }
	keyOfAliasName  = func(e reflect.Value) string { return CollationKey(e.FieldByName("Name").String()) }
	keyOfLink       = func(e reflect.Value) string { return linkKey(e.FieldByName("URL").String()) }
	keyOfTerm       = func(e reflect.Value) string { return fmt.Sprint(e.FieldByName("TermID").Int()) }
	keyOfPreference = func(e reflect.Value) string {
		return e.FieldByName("Dimension").String()
	}
	keyOfPieceRelation = func(e reflect.Value) string {
		return fmt.Sprintf("%d:%s", e.FieldByName("ToPieceID").Int(), e.FieldByName("Type").String())
	}
	keyOfPersonRelation = func(e reflect.Value) string {
		return fmt.Sprintf("%d:%s", e.FieldByName("ToPersonID").Int(), e.FieldByName("Type").String())
	}
)

// The fields of each document type. A group keeps a sourced value and its source together (D11), so that a merge
// never takes them from different sides. Every field but schema_version is in exactly one docField
// (TestMergeFields_CoverEveryDocumentField).
var (
	pieceFields = []docField{
		scalar("title"), scalar("kind"), set("secondary_kinds", keyOfValue), scalar("conservation"),
		scalar("is_stub"), scalar("notes_md"), scalar("version_notes_md"),
		group("licensing", "licensing_status", "licence", "licensing_notes_md"), scalar("historical_source_md"),
		group("period", "period_start", "period_end"), keyed("aliases", keyOfAliasText),
		keyed("terms", keyOfTerm).referencing(EntityTypeTerm),
		keyed("relations", keyOfPieceRelation).referencing(EntityTypePiece), scalar("credits"), keyed("links", keyOfLink),
		keyed("preferences", keyOfPreference),
	}
	personFields = []docField{
		scalar("name"), scalar("kind"), group("status", "status", "status_date", "status_source_url"),
		scalar("pronouns"), scalar("notes_md"), group("licensing", "licensing_status", "licensing_notes_md"),
		scalar("is_stub"), keyed("aliases", keyOfAliasName), set("roles", keyOfValue),
		set("affiliation_ids", keyOfValue).referencing(EntityTypeTerm),
		keyed("relations", keyOfPersonRelation).referencing(EntityTypePerson), keyed("links", keyOfLink),
		keyed("preferences", keyOfPreference),
	}
	termFields = []docField{
		scalar("facet"), scalar("name"), scalar("display_name"), scalar("parent_id"),
		scalar("related_affiliation_id"), scalar("description_md"), scalar("position"), set("aliases", keyOfName),
	}
	collectionFields = []docField{
		scalar("name"), scalar("url"), scalar("kind"), scalar("description_md"), scalar("region_term_id"),
		scalar("parent_id"), scalar("is_bardic_resource"), scalar("enumerable"),
	}
	eventFields = []docField{scalar("name"), scalar("kind"), scalar("description_md")}
)

// documentFields returns the fields of the document type D.
func documentFields[D Document]() []docField {
	var doc D
	switch any(doc).(type) {
	case PieceDocument:
		return pieceFields
	case PersonDocument:
		return personFields
	case TermDocument:
		return termFields
	case CollectionDocument:
		return collectionFields
	}

	return eventFields
}

// DiffDocuments returns the fields, elements and orders that differ between a and b, in field order. An element
// differs when it is in one only, or its JSON differs (an empty list being equal to none); a keyed list's order
// differs when the elements both have are in a different order.
func DiffDocuments[D Document](a, b D) []FieldPath {
	av, bv := reflect.ValueOf(a), reflect.ValueOf(b)
	var paths []FieldPath
	for _, f := range documentFields[D]() {
		paths = append(paths, f.diff(av, bv)...)
	}

	return paths
}

// MergeDocuments merges ours (a revision) and theirs (the current document), both edits of base, field by field
// (D9). A field one side changed takes that side's value; a field both changed to equal values takes it; a field
// both changed differently is a conflict. Keyed lists merge element by element, with the order of the side that
// reordered them and each element the other side added after its nearest preceding element; sets merge as sets
// and never conflict. With conflicts the merged document is incomplete, and only the conflicts are meaningful.
func MergeDocuments[D Document](base, ours, theirs D) (D, []RebaseConflict) {
	merged := ours
	out := reflect.ValueOf(&merged).Elem()
	bv, ov, tv := reflect.ValueOf(base), reflect.ValueOf(ours), reflect.ValueOf(theirs)
	var conflicts []RebaseConflict
	for _, f := range documentFields[D]() {
		conflicts = append(conflicts, f.merge(bv, ov, tv, out)...)
	}

	return merged, conflicts
}

func (f docField) diff(a, b reflect.Value) []FieldPath {
	if f.kind == scalarField {
		if f.equal(a, b) {
			return nil
		}

		return []FieldPath{FieldPath(f.path)}
	}
	al, bl := f.list(a), f.list(b)
	var paths []FieldPath
	for _, k := range unionKeys(al.keys, bl.keys) {
		ae, be := al.element(k), bl.element(k)
		if !equalValues(reflect.ValueOf(ae), reflect.ValueOf(be)) {
			paths = append(paths, f.elementPath(k))
		}
	}
	if f.kind == keyedField && !slices.Equal(intersect(al.keys, bl.keys), intersect(bl.keys, al.keys)) {
		paths = append(paths, FieldPath(f.path+"#order"))
	}

	return paths
}

func (f docField) merge(base, ours, theirs, out reflect.Value) []RebaseConflict {
	if f.kind == scalarField {
		side, ok := mergeChoice(f.equal(ours, base), f.equal(theirs, base), f.equal(ours, theirs))
		if !ok {
			return []RebaseConflict{newConflict(FieldPath(f.path), f.value(base), f.value(ours), f.value(theirs))}
		}
		for _, name := range f.names {
			fieldByJSON(out, name).Set(fieldByJSON([]reflect.Value{ours, theirs}[side], name))
		}

		return nil
	}
	b, o, t := f.list(base), f.list(ours), f.list(theirs)
	if f.kind == setField {
		fieldByJSON(out, f.names[0]).Set(mergeSet(b, o, t))

		return nil
	}
	merged, conflicts := f.mergeKeyed(b, o, t)
	fieldByJSON(out, f.names[0]).Set(merged)

	return conflicts
}

// mergeChoice is the three-way choice for one value: 0 to take ours, 1 to take theirs, and false for a conflict.
func mergeChoice(oursUnchanged, theirsUnchanged, sidesEqual bool) (int, bool) {
	switch {
	case oursUnchanged:
		return 1, true
	case theirsUnchanged, sidesEqual:
		return 0, true
	}

	return 1, false
}

// mergeKeyed merges a keyed list element by element and then its order, returning the merged list and the
// conflicts.
func (f docField) mergeKeyed(b, o, t keyedList) (reflect.Value, []RebaseConflict) {
	var conflicts []RebaseConflict
	result := map[string]reflect.Value{}
	for _, k := range unionKeys(o.keys, t.keys, b.keys) {
		be, oe, te := b.element(k), o.element(k), t.element(k)
		bv, ov, tv := reflect.ValueOf(be), reflect.ValueOf(oe), reflect.ValueOf(te)
		side, ok := mergeChoice(equalValues(ov, bv), equalValues(tv, bv), equalValues(ov, tv))
		if !ok {
			conflicts = append(conflicts, newConflict(f.elementPath(k), be, oe, te))

			continue
		}
		if e := []keyedList{o, t}[side].byKey; e[k].IsValid() {
			result[k] = e[k]
		}
	}
	order, ok := mergeOrder(b.keys, o.keys, t.keys, result)
	if !ok {
		conflicts = append(conflicts, newConflict(FieldPath(f.path+"#order"), b.keys, o.keys, t.keys))
	}
	merged := reflect.MakeSlice(o.typ, 0, len(order))
	for _, k := range order {
		merged = reflect.Append(merged, result[k])
	}

	return merged, conflicts
}

// mergeOrder returns the order of the merged elements: the order of the side that reordered the elements base,
// ours and theirs all have (theirs if neither did), with each element only the other side has inserted after its
// nearest preceding element there, or first. Both sides reordering them differently is a conflict.
func mergeOrder(b, o, t []string, result map[string]reflect.Value) ([]string, bool) {
	bo := intersect(b, intersect(o, t))
	oo, to := intersect(o, bo), intersect(t, bo)
	oursReordered, theirsReordered := !slices.Equal(oo, bo), !slices.Equal(to, bo)
	primary, other := t, o
	if oursReordered {
		primary, other = o, t
	}
	var order []string
	for _, k := range primary {
		if _, ok := result[k]; ok {
			order = append(order, k)
		}
	}
	for i, k := range other {
		if _, ok := result[k]; ok && !slices.Contains(order, k) {
			order = slices.Insert(order, insertionIndex(order, other[:i]), k)
		}
	}

	return order, !oursReordered || !theirsReordered || slices.Equal(oo, to)
}

// insertionIndex is where an element goes in order: after the last of preceding (the elements before it in its own
// list) that order holds, or first if order holds none of them.
func insertionIndex(order, preceding []string) int {
	for i := len(preceding) - 1; i >= 0; i-- {
		if at := slices.Index(order, preceding[i]); at >= 0 {
			return at + 1
		}
	}

	return 0
}

// mergeSet merges a set: an element is in the merge if both sides kept it or one side added it, with ours if ours
// changed it and theirs otherwise; theirs' order, then what only ours added.
func mergeSet(b, o, t keyedList) reflect.Value {
	merged := reflect.MakeSlice(o.typ, 0, len(o.keys)+len(t.keys))
	for _, k := range unionKeys(t.keys, o.keys) {
		be, oe, te := b.byKey[k], o.byKey[k], t.byKey[k]
		inBase, inOurs, inTheirs := be.IsValid(), oe.IsValid(), te.IsValid()
		switch {
		case inBase && (!inOurs || !inTheirs):
		case !inTheirs, inBase && !equalValues(oe, be):
			merged = reflect.Append(merged, oe)
		default:
			merged = reflect.Append(merged, te)
		}
	}

	return merged
}

// keyedList is a list's elements by key. A key that repeats in the list is qualified by its occurrence ("k#1"), so
// that every element has its own key and none is lost.
type keyedList struct {
	typ   reflect.Type
	keys  []string
	byKey map[string]reflect.Value
}

func (f docField) list(doc reflect.Value) keyedList {
	v := fieldByJSON(doc, f.names[0])
	l := keyedList{typ: v.Type(), byKey: map[string]reflect.Value{}}
	occurrences := map[string]int{}
	for i := range v.Len() {
		k := f.key(v.Index(i))
		if n := occurrences[k]; n > 0 {
			occurrences[k]++
			k = fmt.Sprintf("%s#%d", k, n)
		} else {
			occurrences[k] = 1
		}
		l.keys = append(l.keys, k)
		l.byKey[k] = v.Index(i)
	}

	return l
}

// element returns the element with key k as a pointer, nil if the list has none, which compares and encodes as a
// missing element.
func (l keyedList) element(k string) any {
	e, ok := l.byKey[k]
	if !ok {
		return reflect.Zero(reflect.PointerTo(l.typ.Elem())).Interface()
	}
	p := reflect.New(e.Type())
	p.Elem().Set(e)

	return p.Interface()
}

func (f docField) elementPath(k string) FieldPath {
	return FieldPath(fmt.Sprintf("%s[%s]", f.path, k))
}

// equal reports whether the field has equal values in a and b.
func (f docField) equal(a, b reflect.Value) bool {
	for _, name := range f.names {
		if !equalValues(fieldByJSON(a, name), fieldByJSON(b, name)) {
			return false
		}
	}

	return true
}

// value is the field's value in doc, as a conflict shows it: a group as an object of its fields.
func (f docField) value(doc reflect.Value) any {
	if len(f.names) == 1 {
		return fieldByJSON(doc, f.names[0]).Interface()
	}
	values := map[string]any{}
	for _, name := range f.names {
		values[name] = fieldByJSON(doc, name).Interface()
	}

	return values
}

// fieldByJSON is the field of the struct doc whose JSON name is name.
func fieldByJSON(doc reflect.Value, name string) reflect.Value {
	return doc.FieldByIndex(jsonFieldIndex(doc.Type(), name))
}

// jsonFieldIndex is the index of the field of the struct type t whose JSON name is name, or nil if it has none.
func jsonFieldIndex(t reflect.Type, name string) []int {
	for i := range t.NumField() {
		if jsonFieldName(t.Field(i)) == name {
			return t.Field(i).Index
		}
	}

	return nil
}

// newConflict returns the conflict at path between the values of base, revision and current.
func newConflict(path FieldPath, base, revision, current any) RebaseConflict {
	return RebaseConflict{Path: path, Base: conflictJSON(base), Revision: conflictJSON(revision),
		Current: conflictJSON(current)}
}

// conflictJSON is the JSON of v, or null if it has none (a document whose raw preference value is not JSON).
func conflictJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}

	return data
}

// unionKeys returns the keys of lists, each once, in the order they first appear.
func unionKeys(lists ...[]string) []string {
	var keys []string
	seen := map[string]bool{}
	for _, l := range lists {
		for _, k := range l {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}

	return keys
}

// intersect returns the keys of a that b has too, in a's order.
func intersect(a, b []string) []string {
	var keys []string
	for _, k := range a {
		if slices.Contains(b, k) {
			keys = append(keys, k)
		}
	}

	return keys
}

// rawMessageType is the type of raw JSON, which equalValues compares as bytes.
var rawMessageType = reflect.TypeFor[json.RawMessage]()

// equalValues reports whether a and b are deeply equal, an empty list being equal to none: the values encode to the
// same JSON but for an empty list encoding as [] or null.
func equalValues(a, b reflect.Value) bool {
	if a.Type() == rawMessageType {
		return bytes.Equal(a.Bytes(), b.Bytes())
	}
	if a.Kind() == reflect.Slice {
		return equalLists(a, b)
	}
	if a.Kind() == reflect.Pointer {
		return a.IsNil() == b.IsNil() && (a.IsNil() || equalValues(a.Elem(), b.Elem()))
	}
	if a.Kind() == reflect.Struct {
		return equalFields(a, b)
	}

	return a.Interface() == b.Interface()
}

// equalLists is equalValues for lists: element by element.
func equalLists(a, b reflect.Value) bool {
	if a.Len() != b.Len() {
		return false
	}
	for i := range a.Len() {
		if !equalValues(a.Index(i), b.Index(i)) {
			return false
		}
	}

	return true
}

// equalFields is equalValues for structs: field by field, and deeply for a struct with unexported fields (a time).
func equalFields(a, b reflect.Value) bool {
	for i := range a.NumField() {
		if !a.Type().Field(i).IsExported() {
			return reflect.DeepEqual(a.Interface(), b.Interface())
		}
		if !equalValues(a.Field(i), b.Field(i)) {
			return false
		}
	}

	return true
}
