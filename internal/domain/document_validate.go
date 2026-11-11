package domain

import (
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// ProblemCode is what is wrong with a field of a document.
type ProblemCode string

// The problem codes.
const (
	ProblemRequired         ProblemCode = "required"
	ProblemTooLong          ProblemCode = "too_long"
	ProblemNotNFC           ProblemCode = "not_nfc"
	ProblemControlCharacter ProblemCode = "control_character"
	ProblemDuplicate        ProblemCode = "duplicate"
	ProblemInvalid          ProblemCode = "invalid"
	ProblemOutOfRange       ProblemCode = "out_of_range"
	ProblemCycle            ProblemCode = "cycle"
	ProblemTooDeep          ProblemCode = "too_deep"
	ProblemWrongFacet       ProblemCode = "wrong_facet"
	ProblemUnknownReference ProblemCode = "unknown_reference"
	ProblemSourceRequired   ProblemCode = "source_required"
	ProblemSummaryRequired  ProblemCode = "summary_required"
)

// Problem is a problem with the field at Path of a document, in the editor format's JSON names
// ("credits[2].person_slug"); "summary" is the revision's summary.
type Problem struct {
	Path string      `json:"path"`
	Code ProblemCode `json:"code"`
}

// ValidationContext is what validating a document needs besides the document.
type ValidationContext struct {
	// ID is the entity's own ID, or 0 for a new entity.
	ID int64
	// Terms are the terms the document references and, for a term document, every term of its facet.
	Terms map[int64]TermInfo
	// CollectionParents are the parents of the collections the document references and of their ancestors.
	CollectionParents map[int64]*int64
}

// TermInfo is what validation knows of a term.
type TermInfo struct {
	Facet    Facet
	ParentID *int64
	Name     string
}

// The column widths Validate checks: VARCHAR widths in characters, TEXT in bytes.
const (
	maxNameLength        = 255
	maxLinkTitleLength   = 500
	maxCreditNoteLength  = 500
	maxSummaryLength     = 500
	maxPlatformLength    = 64
	maxPronounsLength    = 64
	maxTextBytes         = 65535
	maxContributorsCount = 65535
)

// problems collects the problems of a document.
type problems []Problem

func (p *problems) check(ok bool, path string, code ProblemCode) {
	if !ok {
		*p = append(*p, Problem{Path: path, Code: code})
	}
}

// document checks the schema version and every string and vocabulary value of doc (contents).
func (p *problems) document(doc any, version int) {
	p.check(version == DocumentSchemaVersion, "schema_version", ProblemInvalid)
	p.contents(reflect.ValueOf(doc), "")
}

// name checks a name or title: not blank, and it fits a VARCHAR(255).
func (p *problems) name(s, path string) {
	p.check(strings.TrimSpace(s) != "", path, ProblemRequired)
	p.check(utf8.RuneCountInString(s) <= maxNameLength, path, ProblemTooLong)
}

// varchar checks a nullable VARCHAR(n) value.
func (p *problems) varchar(s *string, n int, path string) {
	p.check(s == nil || utf8.RuneCountInString(*s) <= n, path, ProblemTooLong)
}

// text checks a nullable TEXT value.
func (p *problems) text(s *string, path string) {
	p.check(s == nil || len(*s) <= maxTextBytes, path, ProblemTooLong)
}

// url checks a nullable VARCHAR(2048) URL.
func (p *problems) url(s *string, path string) {
	p.check(s == nil || len(*s) <= MaxURLLength, path, ProblemTooLong)
}

// unique checks that no two of keys are equal, reporting each repeat at path[k].
func (p *problems) unique(keys []string, path string) {
	seen := map[string]bool{}
	for k, key := range keys {
		p.check(!seen[key], fmt.Sprintf("%s[%d]", path, k), ProblemDuplicate)
		seen[key] = true
	}
}

// term checks a reference to a term: the term is known and, if facet is set, of that facet.
func (p *problems) term(terms map[int64]TermInfo, id int64, facet Facet, path string) (TermInfo, bool) {
	t, ok := terms[id]
	p.check(ok, path, ProblemUnknownReference)
	p.check(!ok || facet == "" || t.Facet == facet, path, ProblemWrongFacet)

	return t, ok
}

// links checks a document's links: each a link (link), at most once by normalised URL, with roles, each once.
func (p *problems) links(links []DocLink) {
	urls := make([]string, 0, len(links))
	for k, l := range links {
		path := fmt.Sprintf("links[%d]", k)
		p.link(l, path+".")
		u, _ := NormalizeURL(l.URL)
		urls = append(urls, u)
		p.check(len(l.Roles) > 0, path+".roles", ProblemRequired)
		p.unique(stringsOf(l.Roles), path+".roles")
	}
	p.unique(urls, "links")
}

// link checks the fields of a link, at prefix: its URL normalises and the others fit.
func (p *problems) link(l DocLink, prefix string) {
	_, err := NormalizeURL(l.URL)
	p.check(err == nil, prefix+"url", ProblemInvalid)
	p.varchar(l.Title, maxLinkTitleLength, prefix+"title")
	p.text(l.Description, prefix+"description")
	p.varchar(l.Locator, maxNameLength, prefix+"locator")
	if l.Platform != nil {
		p.check(*l.Platform != "", prefix+"platform", ProblemRequired)
		p.check(len(*l.Platform) <= maxPlatformLength, prefix+"platform", ProblemTooLong)
		p.check(isASCII(*l.Platform), prefix+"platform", ProblemInvalid)
	}
}

// performance checks a performance at path.
func (p *problems) performance(perf DocPerformance, path string) {
	if perf.Uploader != nil {
		p.name(*perf.Uploader, path+".uploader")
	}
	p.check(len(perf.DescriptionVerbatim) <= maxTextBytes, path+".description_verbatim", ProblemTooLong)
	performers := make([]string, 0, len(perf.PerformerIDs))
	for _, id := range perf.PerformerIDs {
		performers = append(performers, fmt.Sprint(id))
	}
	p.unique(performers, path+".performer_slugs")
}

// aliases checks a list of aliases at path: each a name, and unique as the collation compares them.
func (p *problems) aliases(names []string, path, field string) {
	keys := make([]string, 0, len(names))
	for k, a := range names {
		p.name(a, fmt.Sprintf("%s[%d]%s", path, k, field))
		keys = append(keys, CollationKey(a))
	}
	p.unique(keys, path)
}

// contents checks every string in v, descending into pointers, slices and exported struct fields with their JSON
// names: a vocabulary value must be known, any other string valid UTF-8 in NFC without control characters.
func (p *problems) contents(v reflect.Value, path string) {
	kind := v.Kind()
	if kind == reflect.Pointer && !v.IsNil() {
		p.contents(v.Elem(), path)
	}
	if kind == reflect.Slice || kind == reflect.Array {
		for i := range v.Len() {
			p.contents(v.Index(i), fmt.Sprintf("%s[%d]", path, i))
		}
	}
	if kind == reflect.Struct {
		for i := range v.NumField() {
			if f := v.Type().Field(i); f.IsExported() {
				p.contents(v.Field(i), fieldPath(path, f))
			}
		}
	}
	if kind != reflect.String {
		return
	}
	if e, ok := v.Interface().(validator); ok {
		p.check(e.Valid(), path, ProblemInvalid)

		return
	}
	s := v.String()
	p.check(utf8.ValidString(s) && norm.NFC.IsNormalString(s), path, ProblemNotNFC)
	p.check(!strings.ContainsFunc(s, IsControlCharacter), path, ProblemControlCharacter)
}

// fieldPath is the path of the struct field f below path: by its JSON name, or path itself for an embedded field.
func fieldPath(path string, f reflect.StructField) string {
	if f.Anonymous {
		return path
	}
	name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	if name == "" {
		name = f.Name
	}
	if path == "" {
		return name
	}

	return path + "." + name
}

func stringsOf[S ~string](values []S) []string {
	out := make([]string, 0, len(values))
	for _, s := range values {
		out = append(out, string(s))
	}

	return out
}

func isASCII(s string) bool {
	for k := range len(s) {
		if s[k] >= utf8.RuneSelf {
			return false
		}
	}

	return true
}
