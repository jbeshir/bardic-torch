package domain

import (
	"fmt"
	"reflect"
	"strings"
)

// referenceUse is the field a document references another entity by, which decides the rules for the reference.
type referenceUse int

const (
	usePieceTerm referenceUse = iota
	usePieceRelation
	useCredit
	useAffiliation
	usePersonRelation
	useLinkCollection
	usePerformanceEvent
	usePerformancePiece
	usePerformer
	useTermParent
	useRelatedAffiliation
	useCollectionRegion
	useCollectionParent
)

// referenceTypes is the type of entity each use references.
var referenceTypes = map[referenceUse]EntityType{
	usePieceTerm: EntityTypeTerm, usePieceRelation: EntityTypePiece, useCredit: EntityTypePerson,
	useAffiliation: EntityTypeTerm, usePersonRelation: EntityTypePerson, useLinkCollection: EntityTypeCollection,
	usePerformanceEvent: EntityTypeEvent, usePerformancePiece: EntityTypePiece, usePerformer: EntityTypePerson,
	useTermParent: EntityTypeTerm, useRelatedAffiliation: EntityTypeTerm, useCollectionRegion: EntityTypeTerm,
	useCollectionParent: EntityTypeCollection,
}

// DocReference is one reference of a document to another entity: the entity's type and ID, the path of the
// reference (DocumentProblem paths) and its element key, which identifies the reference across two versions of the
// document, as restore and merge identify elements: a credit by its person, role and part (and occurrence), a
// performer within its performance, a link's collection within its link.
type DocReference struct {
	Path string
	Key  string
	Type EntityType
	ID   int64
	use  referenceUse
}

// ChangedReferences returns the references of next that the edit from prev adds or changes: those whose element
// key is not a key of a reference of prev. With no prev (a new entity) every reference is changed. The rules on
// what a document may reference apply to these only; a reference that is unchanged needs only to exist (D12).
func ChangedReferences[D Document](prev *D, next D) []DocReference {
	var before map[string]bool
	if prev != nil {
		before = map[string]bool{}
		for _, r := range documentReferences(*prev) {
			before[r.Key] = true
		}
	}
	var out []DocReference
	for _, r := range documentReferences(next) {
		if !before[r.Key] {
			out = append(out, r)
		}
	}

	return out
}

// documentReferences returns every reference of doc, in document order.
func documentReferences[D Document](doc D) []DocReference {
	var refs referenceList
	switch d := any(doc).(type) {
	case PieceDocument:
		refs.piece(d)
	case PersonDocument:
		refs.person(d)
	case TermDocument:
		refs.optional(d.ParentID, "/parent", "parent", useTermParent)
		refs.optional(d.RelatedAffiliationID, "/related_affiliation", "related_affiliation", useRelatedAffiliation)
	case CollectionDocument:
		refs.optional(d.RegionTermID, "/region", "region", useCollectionRegion)
		refs.optional(d.ParentID, "/parent_id", "parent", useCollectionParent)
	case EventDocument:
	}

	return refs
}

type referenceList []DocReference

func (l *referenceList) add(id int64, path, key string, use referenceUse) {
	*l = append(*l, DocReference{Path: path, Key: key, Type: referenceTypes[use], ID: id, use: use})
}

func (l *referenceList) optional(id *int64, path, key string, use referenceUse) {
	if id != nil {
		l.add(*id, path, fmt.Sprintf("%s:%d", key, *id), use)
	}
}

func (l *referenceList) piece(d PieceDocument) {
	for k, t := range d.Terms {
		l.add(t.TermID, fmt.Sprintf("/terms/%d/term", k), fmt.Sprintf("term:%d", t.TermID), usePieceTerm)
	}
	for k, r := range d.Relations {
		l.add(r.ToPieceID, fmt.Sprintf("/relations/%d/piece", k), fmt.Sprintf("relation:%d:%s", r.ToPieceID, r.Type),
			usePieceRelation)
	}
	occurrences := map[string]int{}
	for k, c := range d.Credits {
		if c.PersonID == nil {
			continue
		}
		key := elementKey("credit", *c.PersonID, c.Role, c.Part)
		l.add(*c.PersonID, fmt.Sprintf("/credits/%d/person", k), fmt.Sprintf("%s#%d", key, occurrences[key]),
			useCredit)
		occurrences[key]++
	}
	for k, link := range d.Links {
		l.link(link.DocLink, k)
		for j, p := range link.Performances {
			l.performance(p, nil, link.URL, fmt.Sprintf("/links/%d/performances/%d", k, j))
		}
	}
}

func (l *referenceList) person(d PersonDocument) {
	for k, id := range d.AffiliationIDs {
		l.add(id, fmt.Sprintf("/affiliations/%d", k), fmt.Sprintf("affiliation:%d", id), useAffiliation)
	}
	for k, r := range d.Relations {
		l.add(r.ToPersonID, fmt.Sprintf("/relations/%d/person", k),
			fmt.Sprintf("relation:%d:%s", r.ToPersonID, r.Type), usePersonRelation)
	}
	for k, link := range d.Links {
		l.link(link.DocLink, k)
		for j, p := range link.Performances {
			l.performance(p.DocPerformance, p.PieceID, link.URL, fmt.Sprintf("/links/%d/performances/%d", k, j))
		}
	}
}

func (l *referenceList) link(link DocLink, k int) {
	if link.CollectionID != nil {
		l.add(*link.CollectionID, fmt.Sprintf("/links/%d/collection_id", k),
			elementKey("collection", linkKey(link.URL), *link.CollectionID), useLinkCollection)
	}
}

// performance adds the references of a performance at path of the link with URL url, of the piece pieceID if it
// is a performance of a person's link: its event, its piece and its performers, each keyed within the performance
// key (description, year, event and piece).
func (l *referenceList) performance(p DocPerformance, pieceID *int64, url, path string) {
	key := elementKey("performance", linkKey(url), p.DescriptionVerbatim, p.Year, p.EventID, pieceID)
	if p.EventID != nil {
		l.add(*p.EventID, path+"/event", key+":event", usePerformanceEvent)
	}
	if pieceID != nil {
		l.add(*pieceID, path+"/piece", key+":piece", usePerformancePiece)
	}
	for k, id := range p.PerformerIDs {
		l.add(id, fmt.Sprintf("%s/performers/%d", path, k), fmt.Sprintf("%s:performer:%d", key, id), usePerformer)
	}
}

// linkKey is the element key of a link: its normalized URL, or the URL as written if it does not normalize.
func linkKey(url string) string {
	if u, err := NormalizeURL(url); err == nil {
		return u
	}

	return "unnormalized:" + url
}

// elementKey joins the parts of an element key unambiguously, each quoted, a nil pointer as null and any other
// pointer as its value.
func elementKey(parts ...any) string {
	var b strings.Builder
	for _, part := range parts {
		v := reflect.ValueOf(part)
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				b.WriteString("null|")

				continue
			}
			v = v.Elem()
		}
		fmt.Fprintf(&b, "%q|", fmt.Sprint(v.Interface()))
	}

	return b.String()
}
