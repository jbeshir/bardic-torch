package domain

import (
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// The column widths of the catalogue fields: VARCHAR widths in characters, TEXT in bytes.
const (
	maxNameLength       = 255
	maxLinkTitleLength  = 500
	maxCreditNoteLength = 500
	// MaxSummaryLength is the width of a revision's summary, which a moderator's replacement summary is checked
	// against as a request field.
	MaxSummaryLength     = 500
	maxPlatformLength    = 64
	maxPronounsLength    = 64
	maxTextBytes         = 65535
	maxContributorsCount = 65535
)

// ProblemCode is what is wrong with a field of a document (DocumentProblem). The codes are the problem vocabulary
// of the API's DocumentError.
type ProblemCode string

// The problem codes.
const (
	ProblemRequired               ProblemCode = "required"
	ProblemTooLong                ProblemCode = "too_long"
	ProblemInvalidCharacters      ProblemCode = "invalid_characters"
	ProblemNotNFC                 ProblemCode = "not_nfc"
	ProblemBlank                  ProblemCode = "blank"
	ProblemDuplicate              ProblemCode = "duplicate"
	ProblemUnknownReference       ProblemCode = "unknown_reference"
	ProblemSelfReference          ProblemCode = "self_reference"
	ProblemCycle                  ProblemCode = "cycle"
	ProblemTooDeep                ProblemCode = "too_deep"
	ProblemWrongFacet             ProblemCode = "wrong_facet"
	ProblemFacetImmutable         ProblemCode = "facet_immutable"
	ProblemSourceRequired         ProblemCode = "source_required"
	ProblemReasonRequired         ProblemCode = "reason_required"
	ProblemInvalidURL             ProblemCode = "invalid_url"
	ProblemLicenceNeedsKnown      ProblemCode = "licence_needs_known_status"
	ProblemDateNeedsStatus        ProblemCode = "date_needs_status"
	ProblemNamedNeedsPerson       ProblemCode = "named_needs_person"
	ProblemPersonNeedsNamed       ProblemCode = "person_needs_named"
	ProblemCreditToNoPagePerson   ProblemCode = "credit_to_no_page_person"
	ProblemRelationToNoPagePerson ProblemCode = "relation_to_no_page_person"
	ProblemSecondPrimaryTerm      ProblemCode = "second_primary_term"
	ProblemPartNotAllowed         ProblemCode = "part_not_allowed"
	ProblemOutOfRange             ProblemCode = "out_of_range"
	ProblemTooMany                ProblemCode = "too_many"
	ProblemAliasVisibility        ProblemCode = "alias_visibility"
	ProblemWithheldPlaceholder    ProblemCode = "withheld_placeholder"
	ProblemWithheldFieldChanged   ProblemCode = "withheld_field_changed"
	ProblemChoiceConflict         ProblemCode = "choice_conflict"
	ProblemDimensionNotAllowed    ProblemCode = "dimension_not_allowed"
)

// DocumentProblem is one problem with a document: the code, at the JSON-pointer-like path of the field ("/title",
// "/links/2/roles"). It never holds the field's value, which may be private.
type DocumentProblem struct {
	Path string
	Code ProblemCode
}

// pathStyle names the fields and elements of a path: the import's error messages name Go fields
// (".Doc.Links[0].DocLink.URL"), document problems the API's fields ("/links/0/url").
type pathStyle struct {
	field func(parent string, f reflect.StructField) string
	index func(parent string, i int) string
}

// importPaths is the path style of the import's error messages.
var importPaths = pathStyle{
	field: func(parent string, f reflect.StructField) string { return parent + "." + f.Name },
	index: func(parent string, i int) string { return fmt.Sprintf("%s[%d]", parent, i) },
}

// documentPaths is the path style of document problems: each field by its name in the editor format, the editor
// tag where the editor format renames a reference ("person" for person_id) and the JSON name otherwise, and an
// embedded struct's fields as the struct's own, as JSON encodes them.
var documentPaths = pathStyle{
	field: func(parent string, f reflect.StructField) string {
		if name, ok := f.Tag.Lookup("editor"); ok {
			return parent + "/" + name
		}
		name := jsonFieldName(f)
		if f.Anonymous && name == "" {
			return parent
		}

		return parent + "/" + name
	},
	index: func(parent string, i int) string { return fmt.Sprintf("%s/%d", parent, i) },
}

// fieldCheck checks fields of the catalogue against the columns that hold them, reporting each problem: the checks
// the import and document validation share.
type fieldCheck struct {
	report func(path string, code ProblemCode)
	paths  pathStyle
}

func (c fieldCheck) check(ok bool, path string, code ProblemCode) {
	if !ok {
		c.report(path, code)
	}
}

// name checks a name or title of the catalogue (a required VARCHAR(255)): it fits, and holds no withheld-text
// placeholder (WithheldName, WithheldEmail), which only free text may hold.
func (c fieldCheck) name(s, path string) {
	c.varchar(s, maxNameLength, path, true)
	c.check(!HoldsWithheldText(s), path, ProblemWithheldPlaceholder)
}

// varchar checks a VARCHAR(n) value: at most n characters, and not empty if required.
func (c fieldCheck) varchar(s string, n int, path string, required bool) {
	c.check(!required || s != "", path, ProblemBlank)
	c.check(utf8.RuneCountInString(s) <= n, path, ProblemTooLong)
}

// optionalVarchar checks a nullable VARCHAR(n) value.
func (c fieldCheck) optionalVarchar(s *string, n int, path string) {
	if s != nil {
		c.varchar(*s, n, path, false)
	}
}

// text checks a nullable TEXT value.
func (c fieldCheck) text(s *string, path string) {
	if s != nil {
		c.check(len(*s) <= maxTextBytes, path, ProblemTooLong)
	}
}

// url checks the width of a nullable VARCHAR(2048) URL.
func (c fieldCheck) url(s *string, path string) {
	if s != nil {
		c.check(len(*s) <= MaxURLLength, path, ProblemTooLong)
	}
}

// unique checks that no two of keys, the keys of the elements of the list at path, are equal, reporting each
// later element of a pair.
func (c fieldCheck) unique(keys []string, path string) {
	seen := map[string]bool{}
	for k, key := range keys {
		c.check(!seen[key], c.paths.index(path, k), ProblemDuplicate)
		seen[key] = true
	}
}

// strings checks that every string in v is valid UTF-8 in NFC and holds no control character
// (IsControlCharacter).
func (c fieldCheck) strings(v reflect.Value, path string) {
	walkStrings(v, path, c.paths, func(path string, v reflect.Value) {
		s := v.String()
		if !utf8.ValidString(s) || !norm.NFC.IsNormalString(s) {
			c.report(path, ProblemNotNFC)

			return
		}
		c.check(!strings.ContainsFunc(s, IsControlCharacter), path, ProblemInvalidCharacters)
	})
}

// walkStrings calls visit with every string-kinded value in v and its path, descending into pointers, slices,
// arrays and the exported fields of structs, in order.
func walkStrings(v reflect.Value, path string, paths pathStyle, visit func(path string, v reflect.Value)) {
	switch kind := v.Kind(); {
	case kind == reflect.Pointer:
		if !v.IsNil() {
			walkStrings(v.Elem(), path, paths, visit)
		}
	case isList(kind):
		for i := range v.Len() {
			walkStrings(v.Index(i), paths.index(path, i), paths, visit)
		}
	case kind == reflect.Struct:
		for i := range v.NumField() {
			if f := v.Type().Field(i); f.IsExported() {
				walkStrings(v.Field(i), paths.field(path, f), paths, visit)
			}
		}
	case kind == reflect.String:
		visit(path, v)
	}
}

// isList reports whether values of kind are lists, slices or arrays, whose elements the walks visit.
func isList(kind reflect.Kind) bool {
	return kind == reflect.Slice || kind == reflect.Array
}
