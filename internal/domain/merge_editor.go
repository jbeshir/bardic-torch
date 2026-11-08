package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strconv"
	"strings"
)

// Diff and merge name fields, elements and values in the stored format, which references entities by internal ID.
// Moderation views show them in the editor format of their documents instead (docs/PLAN.md §5.1 "Moderation
// views"): each field by its editor name ("term", "parent", "affiliations"), each element of a list keyed by a
// reference with the reference's slug ("terms[drinking-songs]", "relations[the-copper-kettle:sibling]") and each
// value as the editor format has it. A reference the slugs cannot name is shown as pending-<type>-<id>.

// ErrUnknownFieldPath is a field path that names no field of its document type.
var ErrUnknownFieldPath = errors.New("unknown field path")

// EditorConflicts returns conflicts, between documents of entity type t, in the editor format with the slugs s: the
// path in editor vocabulary, and each value converted as it is in an editor document of that type.
func EditorConflicts(t EntityType, conflicts []RebaseConflict, s SlugMap) ([]RebaseConflict, error) {
	v, err := vocabularyFor(t)
	if err != nil {
		return nil, err
	}
	out := make([]RebaseConflict, 0, len(conflicts))
	for _, c := range conflicts {
		p, err := v.parse(c.Path)
		if err != nil {
			return nil, err
		}
		conflict := RebaseConflict{Path: v.editorPath(p, s)}
		for _, value := range []struct {
			raw json.RawMessage
			out *json.RawMessage
		}{{c.Base, &conflict.Base}, {c.Revision, &conflict.Revision}, {c.Current, &conflict.Current}} {
			if *value.out, err = v.editorValue(p, value.raw, s); err != nil {
				return nil, fmt.Errorf("conflict at %s: %w", c.Path, err)
			}
		}
		out = append(out, conflict)
	}

	return out, nil
}

// EditorFieldPaths returns paths, of documents of entity type t, in the editor format with the slugs s, each once.
func EditorFieldPaths(t EntityType, paths []FieldPath, s SlugMap) ([]FieldPath, error) {
	v, err := vocabularyFor(t)
	if err != nil {
		return nil, err
	}
	out := make([]FieldPath, 0, len(paths))
	seen := map[FieldPath]bool{}
	for _, path := range paths {
		p, err := v.parse(path)
		if err != nil {
			return nil, err
		}
		editor := v.editorPath(p, s)
		if !seen[editor] {
			seen[editor] = true
			out = append(out, editor)
		}
	}

	return out, nil
}

// editorVocabulary converts the field paths and values of one document type to the editor format.
type editorVocabulary struct {
	typ    reflect.Type
	fields []docField
	// document returns the default document of the type, settable.
	document func() reflect.Value
	// toEditor converts doc, a document of the type, with the slugs s and a slug for each reference s lacks.
	toEditor func(doc reflect.Value, s SlugMap) (AnyEditorDocument, error)
}

func vocabularyOf[D Document]() editorVocabulary {
	return editorVocabulary{
		typ:    reflect.TypeFor[D](),
		fields: documentFields[D](),
		document: func() reflect.Value {
			doc := DefaultDocument[D]()

			return reflect.ValueOf(&doc).Elem()
		},
		toEditor: func(doc reflect.Value, s SlugMap) (AnyEditorDocument, error) {
			d, ok := doc.Interface().(D)
			if !ok {
				return nil, fmt.Errorf("%w [%s]", ErrUnknownDocumentType, doc.Type())
			}
			stored, ok := any(d).(AnyDocument)
			if !ok {
				return nil, fmt.Errorf("%w [%s]", ErrUnknownDocumentType, doc.Type())
			}

			return ToEditor(stored, s.withFallbacks(documentReferences(d)))
		},
	}
}

// vocabularyFor returns the editor vocabulary of documents of entity type t.
func vocabularyFor(t EntityType) (editorVocabulary, error) {
	switch t {
	case EntityTypePiece:
		return vocabularyOf[PieceDocument](), nil
	case EntityTypePerson:
		return vocabularyOf[PersonDocument](), nil
	case EntityTypeTerm:
		return vocabularyOf[TermDocument](), nil
	case EntityTypeCollection:
		return vocabularyOf[CollectionDocument](), nil
	case EntityTypeEvent:
		return vocabularyOf[EventDocument](), nil
	}

	return editorVocabulary{}, fmt.Errorf("%w [%s]", ErrUnknownDocumentType, t)
}

// fieldPath is a FieldPath taken apart: the field, and for an element its key, or for an order, order set.
type fieldPath struct {
	field   docField
	key     string
	element bool
	order   bool
}

func (v editorVocabulary) parse(path FieldPath) (fieldPath, error) {
	name, key, element := strings.Cut(string(path), "[")
	name, order := strings.CutSuffix(name, "#order")
	for _, f := range v.fields {
		if f.path == name {
			return fieldPath{field: f, key: strings.TrimSuffix(key, "]"), element: element, order: order}, nil
		}
	}

	return fieldPath{}, fmt.Errorf("%w [%s]", ErrUnknownFieldPath, path)
}

// editorPath is p in the editor format.
func (v editorVocabulary) editorPath(p fieldPath, s SlugMap) FieldPath {
	name := p.field.path
	if len(p.field.names) == 1 {
		name = v.editorName(p.field.names[0])
	}
	switch {
	case p.order:
		return FieldPath(name + "#order")
	case p.element:
		return FieldPath(name + "[" + p.field.editorKey(p.key, s) + "]")
	}

	return FieldPath(name)
}

// editorName is the editor name of the document field whose JSON name is name, as document problems name it.
func (v editorVocabulary) editorName(name string) string {
	return strings.TrimPrefix(documentPaths.field("", v.typ.FieldByIndex(jsonFieldIndex(v.typ, name))), "/")
}

// editorKey is the element key k of f with the referenced entity it starts with named by slug.
func (f docField) editorKey(k string, s SlugMap) string {
	if f.ref == "" {
		return k
	}
	end := strings.IndexFunc(k, func(r rune) bool { return r < '0' || r > '9' })
	if end < 0 {
		end = len(k)
	}
	id, err := strconv.ParseInt(k[:end], 10, 64)
	if err != nil {
		return k
	}

	return s.slug(f.ref, id) + k[end:]
}

// editorValue is raw, a value of a conflict at p, in the editor format: the element keys of an order, an element as
// the editor list holds it, a field's value or a group's object of its fields' values. Null stays null.
func (v editorVocabulary) editorValue(p fieldPath, raw json.RawMessage, s SlugMap) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage("null"), nil
	}
	if p.order {
		return editorOrder(p.field, raw, s)
	}
	ev, err := v.editorDocument(p, raw, s)
	if err != nil {
		return nil, err
	}
	if len(p.field.names) > 1 {
		group := map[string]any{}
		for _, name := range p.field.names {
			group[v.editorName(name)] = fieldByJSON(ev, v.editorName(name)).Interface()
		}

		return marshalValue(group)
	}
	field := fieldByJSON(ev, v.editorName(p.field.names[0]))
	if !p.element {
		return marshalValue(field.Interface())
	}
	if field.Len() == 0 {
		return json.RawMessage("null"), nil
	}

	return marshalValue(field.Index(0).Interface())
}

// editorOrder is raw, the element keys of the list f in an order conflict, in the editor format.
func editorOrder(f docField, raw json.RawMessage, s SlugMap) (json.RawMessage, error) {
	var keys []string
	if err := json.Unmarshal(raw, &keys); err != nil {
		return nil, fmt.Errorf("decoding order: %w", err)
	}
	editor := make([]string, len(keys))
	for i, k := range keys {
		editor[i] = f.editorKey(k, s)
	}

	return marshalValue(editor)
}

// editorDocument is the editor document of the default document with the field of p set to raw: the field's
// value, a group's object of its fields' values, or an element of the list.
func (v editorVocabulary) editorDocument(p fieldPath, raw json.RawMessage, s SlugMap) (reflect.Value, error) {
	doc := v.document()
	values := map[string]json.RawMessage{p.field.names[0]: raw}
	if len(p.field.names) > 1 {
		if err := json.Unmarshal(raw, &values); err != nil {
			return reflect.Value{}, fmt.Errorf("decoding group: %w", err)
		}
	}
	for _, name := range p.field.names {
		if err := setFieldJSON(fieldByJSON(doc, name), values[name], p.element); err != nil {
			return reflect.Value{}, fmt.Errorf("decoding %s: %w", name, err)
		}
	}
	editor, err := v.toEditor(doc, s)
	if err != nil {
		return reflect.Value{}, err
	}

	return reflect.ValueOf(editor), nil
}

// marshalValue is the JSON of v.
func marshalValue(v any) (json.RawMessage, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encoding value: %w", err)
	}

	return data, nil
}

// setFieldJSON sets field to the value raw holds, or with element set to a list of the one element raw holds.
func setFieldJSON(field reflect.Value, raw json.RawMessage, element bool) error {
	if len(raw) == 0 {
		return nil
	}
	typ := field.Type()
	if element {
		typ = typ.Elem()
	}
	value := reflect.New(typ)
	if err := json.Unmarshal(raw, value.Interface()); err != nil {
		return err
	}
	if !element {
		field.Set(value.Elem())

		return nil
	}
	list := reflect.MakeSlice(field.Type(), 1, 1)
	list.Index(0).Set(value.Elem())
	field.Set(list)

	return nil
}

// slug is the slug of the entity of type t with ID id, or pending-<type>-<id> if s has none.
func (s SlugMap) slug(t EntityType, id int64) string {
	if slug, ok := s.slugs(t)[id]; ok {
		return slug
	}

	return fmt.Sprintf("pending-%s-%d", t, id)
}

// slugs is the slugs of entities of type t; none for a collection, which is referenced by ID.
func (s SlugMap) slugs(t EntityType) map[int64]string {
	switch t {
	case EntityTypePiece:
		return s.Pieces
	case EntityTypePerson:
		return s.Persons
	case EntityTypeTerm:
		return s.Terms
	case EntityTypeEvent:
		return s.Events
	case EntityTypeCollection:
	}

	return nil
}

// withFallbacks is a copy of s that names every reference of refs: a slug (slug) for each entity s has none for,
// and every collection.
func (s SlugMap) withFallbacks(refs []DocReference) SlugMap {
	out := SlugMap{Pieces: maps.Clone(s.Pieces), Persons: maps.Clone(s.Persons), Terms: maps.Clone(s.Terms),
		Events: maps.Clone(s.Events), Collections: maps.Clone(s.Collections)}
	for _, m := range []*map[int64]string{&out.Pieces, &out.Persons, &out.Terms, &out.Events} {
		if *m == nil {
			*m = map[int64]string{}
		}
	}
	if out.Collections == nil {
		out.Collections = map[int64]bool{}
	}
	for _, r := range refs {
		if r.Type == EntityTypeCollection {
			out.Collections[r.ID] = true

			continue
		}
		if _, ok := out.slugs(r.Type)[r.ID]; !ok {
			out.slugs(r.Type)[r.ID] = s.slug(r.Type, r.ID)
		}
	}

	return out
}
