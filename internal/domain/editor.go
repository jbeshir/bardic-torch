package domain

import (
	"errors"
	"fmt"
	"slices"
)

// The editor format is how documents are edited through the API: each stored document's fields, except
// schema_version, with references to pieces, persons, terms and events by slug instead of internal ID. Collections
// are referenced by ID, as they are addressed. A pending new term or person of the caller's is referenced by its
// placeholder slug.
//
// ToEditor* converts a document to the editor format with the slugs the caller may see (SlugMap); FromEditor*
// converts back with the references the caller may make (RefMap), giving an unknown_reference problem at the
// editor path of each one it cannot resolve, whether it does not exist or the caller may not reference it.

// EditorPieceDocument is a PieceDocument in the editor format.
type EditorPieceDocument struct {
	Title              string                `json:"title"`
	Kind               PieceKind             `json:"kind"`
	SecondaryKinds     []PieceKind           `json:"secondary_kinds"`
	Conservation       DocConservation       `json:"conservation"`
	IsStub             bool                  `json:"is_stub"`
	NotesMD            *string               `json:"notes_md"`
	VersionNotesMD     *string               `json:"version_notes_md"`
	LicensingStatus    LicensingStatus       `json:"licensing_status"`
	LicensingNotesMD   *string               `json:"licensing_notes_md"`
	Licence            *Licence              `json:"licence"`
	LicenceVersion     *LicenceVersion       `json:"licence_version"`
	HistoricalSourceMD *string               `json:"historical_source_md"`
	PeriodStart        *int16                `json:"period_start"`
	PeriodEnd          *int16                `json:"period_end"`
	Aliases            []DocPieceAlias       `json:"aliases"`
	Terms              []EditorPieceTerm     `json:"terms"`
	Relations          []EditorPieceRelation `json:"relations"`
	Credits            []EditorCredit        `json:"credits"`
	Links              []EditorPieceLink     `json:"links"`
	Preferences        []DocPreference       `json:"preferences"`
}

// EditorPieceTerm is a DocPieceTerm with the term by slug.
type EditorPieceTerm struct {
	Term      string     `json:"term"`
	IsPrimary bool       `json:"is_primary"`
	Part      *PiecePart `json:"part"`
}

// EditorPieceRelation is a DocPieceRelation with the other piece by slug.
type EditorPieceRelation struct {
	Piece string            `json:"piece"`
	Type  PieceRelationType `json:"type"`
}

// EditorCredit is a DocCredit with the person, if any, by slug.
type EditorCredit struct {
	Person      *string     `json:"person"`
	Role        CreditRole  `json:"role"`
	Attribution Attribution `json:"attribution"`
	Certainty   Certainty   `json:"certainty"`
	Part        *PiecePart  `json:"part"`
	Note        *string     `json:"note"`
}

// EditorPieceLink is a DocPieceLink with its performances in the editor format. A link's collection is by ID, as in
// the document.
type EditorPieceLink struct {
	DocLink
	Performances []EditorPerformance `json:"performances"`
}

// EditorPerformance is a DocPerformance with the event and performers by slug. Key is the performance's key, null
// for a new performance or one sent by a client that does not round-trip it.
type EditorPerformance struct {
	Key                 *string  `json:"key"`
	Event               *string  `json:"event"`
	Year                *int16   `json:"year"`
	Uploader            *string  `json:"uploader"`
	DescriptionVerbatim string   `json:"description_verbatim"`
	Performers          []string `json:"performers"`
}

// EditorPersonDocument is a PersonDocument in the editor format.
type EditorPersonDocument struct {
	Name             string                 `json:"name"`
	Kind             PersonKind             `json:"kind"`
	Status           PersonStatus           `json:"status"`
	StatusDate       *Date                  `json:"status_date"`
	StatusSourceURL  *string                `json:"status_source_url"`
	Pronouns         *string                `json:"pronouns"`
	NotesMD          *string                `json:"notes_md"`
	LicensingStatus  LicensingStatus        `json:"licensing_status"`
	LicensingNotesMD *string                `json:"licensing_notes_md"`
	IsStub           bool                   `json:"is_stub"`
	Aliases          []DocPersonAlias       `json:"aliases"`
	Roles            []PersonRole           `json:"roles"`
	Affiliations     []string               `json:"affiliations"`
	Relations        []EditorPersonRelation `json:"relations"`
	Links            []EditorPersonLink     `json:"links"`
	Preferences      []DocPreference        `json:"preferences"`
}

// EditorPersonRelation is a DocPersonRelation with the other person by slug.
type EditorPersonRelation struct {
	Person string             `json:"person"`
	Type   PersonRelationType `json:"type"`
}

// EditorPersonLink is a DocPersonLink with its performances in the editor format.
type EditorPersonLink struct {
	DocLink
	Performances []EditorPersonPerformance `json:"performances"`
}

// EditorPersonPerformance is a DocPersonPerformance with the piece, if any, by slug.
type EditorPersonPerformance struct {
	Piece *string `json:"piece"`
	EditorPerformance
}

// EditorTermDocument is a TermDocument in the editor format.
type EditorTermDocument struct {
	Facet              Facet    `json:"facet"`
	Name               string   `json:"name"`
	DisplayName        string   `json:"display_name"`
	Parent             *string  `json:"parent"`
	RelatedAffiliation *string  `json:"related_affiliation"`
	DescriptionMD      *string  `json:"description_md"`
	Position           int32    `json:"position"`
	Aliases            []string `json:"aliases"`
}

// EditorCollectionDocument is a CollectionDocument in the editor format. Its parent is by ID, as collections are
// addressed.
type EditorCollectionDocument struct {
	Name             string               `json:"name"`
	URL              *string              `json:"url"`
	Kind             CollectionKind       `json:"kind"`
	DescriptionMD    *string              `json:"description_md"`
	Region           *string              `json:"region"`
	ParentID         *int64               `json:"parent_id"`
	IsBardicResource bool                 `json:"is_bardic_resource"`
	Enumerable       CollectionEnumerable `json:"enumerable"`
}

// EditorEventDocument is an EventDocument in the editor format.
type EditorEventDocument struct {
	Name          string    `json:"name"`
	Kind          EventKind `json:"kind"`
	DescriptionMD *string   `json:"description_md"`
}

// EditorRefs are the references an editor document makes: slugs by entity type, and collection IDs. They may
// repeat.
type EditorRefs struct {
	PieceSlugs    []string
	PersonSlugs   []string
	TermSlugs     []string
	EventSlugs    []string
	CollectionIDs []int64
}

// RefMap resolves the references of editor documents that a caller may make: each slug of an entity they may
// reference to its ID, and the collection IDs they may reference.
type RefMap struct {
	Pieces      map[string]int64
	Persons     map[string]int64
	Terms       map[string]int64
	Events      map[string]int64
	Collections map[int64]bool
}

// SlugMap gives the slug of each referenced piece, person, term and event a caller may see, and the collections
// they may see, by ID.
type SlugMap struct {
	Pieces      map[int64]string
	Persons     map[int64]string
	Terms       map[int64]string
	Events      map[int64]string
	Collections map[int64]bool
}

// NewSlugMap returns the slugs and collections of refs.
func NewSlugMap(refs []EditorReference) SlugMap {
	s := SlugMap{Pieces: map[int64]string{}, Persons: map[int64]string{}, Terms: map[int64]string{},
		Events: map[int64]string{}, Collections: map[int64]bool{}}
	for _, r := range refs {
		switch r.EntityType {
		case EntityTypePiece:
			s.Pieces[r.ID] = r.Slug
		case EntityTypePerson:
			s.Persons[r.ID] = r.Slug
		case EntityTypeTerm:
			s.Terms[r.ID] = r.Slug
		case EntityTypeEvent:
			s.Events[r.ID] = r.Slug
		case EntityTypeCollection:
			s.Collections[r.ID] = true
		}
	}

	return s
}

// EditorReference is an entity a document references, as the caller sees it: by slug (or, for a collection, by
// ID), labelled with its public title or name, or for the caller's own pending new term or person with the name its
// pending document gives it. ID is the internal ID, which is never exposed.
type EditorReference struct {
	EntityType EntityType
	ID         int64
	Slug       string
	Label      string
	IsPending  bool
}

// editorSlugs maps the IDs of one entity type to slugs in ToEditor*. A reference the caller may not see has no
// slug; a projection holds none, and any other is left out (fail closed).
type editorSlugs map[int64]string

func (s editorSlugs) ref(id *int64) *string {
	if id == nil {
		return nil
	}
	if slug, ok := s[*id]; ok {
		return &slug
	}

	return nil
}

func (s editorSlugs) list(ids []int64) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if slug, ok := s[id]; ok {
			out = append(out, slug)
		}
	}

	return out
}

// visibleCollection is id if the caller may see the collection, and nil otherwise.
func (s SlugMap) visibleCollection(id *int64) *int64 {
	if id == nil || !s.Collections[*id] {
		return nil
	}

	return id
}

func (s SlugMap) link(l DocLink) DocLink {
	out := l
	out.Roles = slices.Clone(l.Roles)
	out.CollectionID = s.visibleCollection(l.CollectionID)

	return out
}

func (s SlugMap) performance(p DocPerformance) EditorPerformance {
	return EditorPerformance{Key: optionalKey(p.Key), Event: editorSlugs(s.Events).ref(p.EventID), Year: p.Year,
		Uploader: p.Uploader, DescriptionVerbatim: p.DescriptionVerbatim,
		Performers: editorSlugs(s.Persons).list(p.PerformerIDs)}
}

// ToEditorPiece returns d in the editor format.
func ToEditorPiece(d PieceDocument, s SlugMap) EditorPieceDocument {
	e := EditorPieceDocument{
		Title: d.Title, Kind: d.Kind, SecondaryKinds: nonNilClone(d.SecondaryKinds), Conservation: d.Conservation,
		IsStub: d.IsStub, NotesMD: d.NotesMD, VersionNotesMD: d.VersionNotesMD, LicensingStatus: d.LicensingStatus,
		LicensingNotesMD: d.LicensingNotesMD, Licence: d.Licence, LicenceVersion: d.LicenceVersion,
		HistoricalSourceMD: d.HistoricalSourceMD,
		PeriodStart:        d.PeriodStart, PeriodEnd: d.PeriodEnd, Aliases: nonNilClone(d.Aliases),
		Terms: []EditorPieceTerm{}, Relations: []EditorPieceRelation{}, Credits: []EditorCredit{},
		Links: []EditorPieceLink{}, Preferences: nonNilClone(d.Preferences),
	}
	for _, t := range d.Terms {
		if slug, ok := s.Terms[t.TermID]; ok {
			e.Terms = append(e.Terms, EditorPieceTerm{Term: slug, IsPrimary: t.IsPrimary, Part: t.Part})
		}
	}
	for _, r := range d.Relations {
		if slug, ok := s.Pieces[r.ToPieceID]; ok {
			e.Relations = append(e.Relations, EditorPieceRelation{Piece: slug, Type: r.Type})
		}
	}
	for _, c := range d.Credits {
		person := editorSlugs(s.Persons).ref(c.PersonID)
		if c.PersonID != nil && person == nil {
			continue
		}
		e.Credits = append(e.Credits, EditorCredit{Person: person, Role: c.Role, Attribution: c.Attribution,
			Certainty: c.Certainty, Part: c.Part, Note: c.Note})
	}
	for _, l := range d.Links {
		link := EditorPieceLink{DocLink: s.link(l.DocLink), Performances: []EditorPerformance{}}
		for _, p := range l.Performances {
			link.Performances = append(link.Performances, s.performance(p))
		}
		e.Links = append(e.Links, link)
	}

	return e
}

// ToEditorPerson returns d in the editor format.
func ToEditorPerson(d PersonDocument, s SlugMap) EditorPersonDocument {
	e := EditorPersonDocument{
		Name: d.Name, Kind: d.Kind, Status: d.Status, StatusDate: d.StatusDate, StatusSourceURL: d.StatusSourceURL,
		Pronouns: d.Pronouns, NotesMD: d.NotesMD, LicensingStatus: d.LicensingStatus,
		LicensingNotesMD: d.LicensingNotesMD, IsStub: d.IsStub, Aliases: nonNilClone(d.Aliases),
		Roles: nonNilClone(d.Roles), Affiliations: editorSlugs(s.Terms).list(d.AffiliationIDs),
		Relations: []EditorPersonRelation{}, Links: []EditorPersonLink{}, Preferences: nonNilClone(d.Preferences),
	}
	for _, r := range d.Relations {
		if slug, ok := s.Persons[r.ToPersonID]; ok {
			e.Relations = append(e.Relations, EditorPersonRelation{Person: slug, Type: r.Type})
		}
	}
	for _, l := range d.Links {
		link := EditorPersonLink{DocLink: s.link(l.DocLink), Performances: []EditorPersonPerformance{}}
		for _, p := range l.Performances {
			piece := editorSlugs(s.Pieces).ref(p.PieceID)
			if p.PieceID != nil && piece == nil {
				continue
			}
			link.Performances = append(link.Performances,
				EditorPersonPerformance{Piece: piece, EditorPerformance: s.performance(p.DocPerformance)})
		}
		e.Links = append(e.Links, link)
	}

	return e
}

// ToEditorTerm returns d in the editor format.
func ToEditorTerm(d TermDocument, s SlugMap) EditorTermDocument {
	return EditorTermDocument{
		Facet: d.Facet, Name: d.Name, DisplayName: d.DisplayName, Parent: editorSlugs(s.Terms).ref(d.ParentID),
		RelatedAffiliation: editorSlugs(s.Terms).ref(d.RelatedAffiliationID), DescriptionMD: d.DescriptionMD,
		Position: d.Position, Aliases: nonNilClone(d.Aliases),
	}
}

// ToEditorCollection returns d in the editor format.
func ToEditorCollection(d CollectionDocument, s SlugMap) EditorCollectionDocument {
	return EditorCollectionDocument{
		Name: d.Name, URL: d.URL, Kind: d.Kind, DescriptionMD: d.DescriptionMD,
		Region: editorSlugs(s.Terms).ref(d.RegionTermID), ParentID: s.visibleCollection(d.ParentID),
		IsBardicResource: d.IsBardicResource, Enumerable: d.Enumerable,
	}
}

// ToEditorEvent returns d in the editor format.
func ToEditorEvent(d EventDocument) EditorEventDocument {
	return EditorEventDocument{Name: d.Name, Kind: d.Kind, DescriptionMD: d.DescriptionMD}
}

// nonNilClone is a copy of list, empty rather than nil: the editor format's lists are always present.
func nonNilClone[E any](list []E) []E {
	if list == nil {
		return []E{}
	}

	return slices.Clone(list)
}

// refResolver resolves the references of one editor document, recording an unknown_reference problem at the path
// of each it cannot resolve.
type refResolver struct {
	refs     RefMap
	problems []DocumentProblem
}

func (r *refResolver) unknown(path string) {
	r.problems = append(r.problems, DocumentProblem{Path: path, Code: ProblemUnknownReference})
}

// id resolves slug in ids, or records a problem at path and returns false.
func (r *refResolver) id(ids map[string]int64, slug, path string) (int64, bool) {
	id, ok := ids[slug]
	if !ok {
		r.unknown(path)
	}

	return id, ok
}

// optional resolves an optional reference; an unresolved one is nil, with a problem at path.
func (r *refResolver) optional(ids map[string]int64, slug *string, path string) *int64 {
	if slug == nil {
		return nil
	}
	if id, ok := r.id(ids, *slug, path); ok {
		return &id
	}

	return nil
}

// list resolves a list of references; each unresolved one is left out, with a problem at its path.
func (r *refResolver) list(ids map[string]int64, slugs []string, path string) []int64 {
	out := make([]int64, 0, len(slugs))
	for i, slug := range slugs {
		if id, ok := r.id(ids, slug, fmt.Sprintf("%s/%d", path, i)); ok {
			out = append(out, id)
		}
	}

	return out
}

// collection resolves a collection ID: one the caller may not reference is nil, with a problem at path.
func (r *refResolver) collection(id *int64, path string) *int64 {
	if id == nil {
		return nil
	}
	if !r.refs.Collections[*id] {
		r.unknown(path)
		return nil
	}

	return id
}

func (r *refResolver) link(l DocLink, path string) DocLink {
	out := l
	out.Roles = slices.Clone(l.Roles)
	out.CollectionID = r.collection(l.CollectionID, path+"/collection_id")

	return out
}

func (r *refResolver) performance(p EditorPerformance, path string) DocPerformance {
	return DocPerformance{
		Key:     derefKey(p.Key),
		EventID: r.optional(r.refs.Events, p.Event, path+"/event"), Year: p.Year, Uploader: p.Uploader,
		DescriptionVerbatim: p.DescriptionVerbatim,
		PerformerIDs:        r.list(r.refs.Persons, p.Performers, path+"/performers"),
	}
}

// FromEditorPiece returns the document e describes, with the problems of the references it could not resolve.
func FromEditorPiece(e EditorPieceDocument, refs RefMap) (PieceDocument, []DocumentProblem) {
	r := &refResolver{refs: refs}
	d := PieceDocument{
		SchemaVersion: DocumentSchemaVersion, Title: e.Title, Kind: e.Kind,
		SecondaryKinds: slices.Clone(e.SecondaryKinds), Conservation: e.Conservation, IsStub: e.IsStub,
		NotesMD: e.NotesMD, VersionNotesMD: e.VersionNotesMD, LicensingStatus: e.LicensingStatus,
		LicensingNotesMD: e.LicensingNotesMD, Licence: e.Licence, LicenceVersion: e.LicenceVersion,
		HistoricalSourceMD: e.HistoricalSourceMD,
		PeriodStart:        e.PeriodStart, PeriodEnd: e.PeriodEnd, Aliases: slices.Clone(e.Aliases),
		Preferences: slices.Clone(e.Preferences),
	}
	for i, t := range e.Terms {
		if id, ok := r.id(refs.Terms, t.Term, fmt.Sprintf("/terms/%d/term", i)); ok {
			d.Terms = append(d.Terms, DocPieceTerm{TermID: id, IsPrimary: t.IsPrimary, Part: t.Part})
		}
	}
	for i, rel := range e.Relations {
		if id, ok := r.id(refs.Pieces, rel.Piece, fmt.Sprintf("/relations/%d/piece", i)); ok {
			d.Relations = append(d.Relations, DocPieceRelation{ToPieceID: id, Type: rel.Type})
		}
	}
	for i, c := range e.Credits {
		person := r.optional(refs.Persons, c.Person, fmt.Sprintf("/credits/%d/person", i))
		if c.Person != nil && person == nil {
			continue
		}
		d.Credits = append(d.Credits, DocCredit{PersonID: person, Role: c.Role, Attribution: c.Attribution,
			Certainty: c.Certainty, Part: c.Part, Note: c.Note})
	}
	for i, l := range e.Links {
		path := fmt.Sprintf("/links/%d", i)
		link := DocPieceLink{DocLink: r.link(l.DocLink, path)}
		for j, p := range l.Performances {
			link.Performances = append(link.Performances, r.performance(p, fmt.Sprintf("%s/performances/%d", path, j)))
		}
		d.Links = append(d.Links, link)
	}

	return d, r.problems
}

// FromEditorPerson returns the document e describes, with the problems of the references it could not resolve.
func FromEditorPerson(e EditorPersonDocument, refs RefMap) (PersonDocument, []DocumentProblem) {
	r := &refResolver{refs: refs}
	d := PersonDocument{
		SchemaVersion: DocumentSchemaVersion, Name: e.Name, Kind: e.Kind, Status: e.Status, StatusDate: e.StatusDate,
		StatusSourceURL: e.StatusSourceURL, Pronouns: e.Pronouns, NotesMD: e.NotesMD,
		LicensingStatus: e.LicensingStatus, LicensingNotesMD: e.LicensingNotesMD, IsStub: e.IsStub,
		Aliases: slices.Clone(e.Aliases), Roles: slices.Clone(e.Roles),
		AffiliationIDs: r.list(refs.Terms, e.Affiliations, "/affiliations"),
		Preferences:    slices.Clone(e.Preferences),
	}
	for i, rel := range e.Relations {
		if id, ok := r.id(refs.Persons, rel.Person, fmt.Sprintf("/relations/%d/person", i)); ok {
			d.Relations = append(d.Relations, DocPersonRelation{ToPersonID: id, Type: rel.Type})
		}
	}
	for i, l := range e.Links {
		path := fmt.Sprintf("/links/%d", i)
		link := DocPersonLink{DocLink: r.link(l.DocLink, path)}
		for j, p := range l.Performances {
			perfPath := fmt.Sprintf("%s/performances/%d", path, j)
			piece := r.optional(refs.Pieces, p.Piece, perfPath+"/piece")
			if p.Piece != nil && piece == nil {
				continue
			}
			link.Performances = append(link.Performances,
				DocPersonPerformance{PieceID: piece, DocPerformance: r.performance(p.EditorPerformance, perfPath)})
		}
		d.Links = append(d.Links, link)
	}

	return d, r.problems
}

// FromEditorTerm returns the document e describes, with the problems of the references it could not resolve.
func FromEditorTerm(e EditorTermDocument, refs RefMap) (TermDocument, []DocumentProblem) {
	r := &refResolver{refs: refs}
	d := TermDocument{
		SchemaVersion: DocumentSchemaVersion, Facet: e.Facet, Name: e.Name, DisplayName: e.DisplayName,
		ParentID:             r.optional(refs.Terms, e.Parent, "/parent"),
		RelatedAffiliationID: r.optional(refs.Terms, e.RelatedAffiliation, "/related_affiliation"),
		DescriptionMD:        e.DescriptionMD, Position: e.Position, Aliases: slices.Clone(e.Aliases),
	}

	return d, r.problems
}

// FromEditorCollection returns the document e describes, with the problems of the references it could not
// resolve.
func FromEditorCollection(e EditorCollectionDocument, refs RefMap) (CollectionDocument, []DocumentProblem) {
	r := &refResolver{refs: refs}
	d := CollectionDocument{
		SchemaVersion: DocumentSchemaVersion, Name: e.Name, URL: e.URL, Kind: e.Kind, DescriptionMD: e.DescriptionMD,
		RegionTermID: r.optional(refs.Terms, e.Region, "/region"), ParentID: r.collection(e.ParentID, "/parent_id"),
		IsBardicResource: e.IsBardicResource, Enumerable: e.Enumerable,
	}

	return d, r.problems
}

// FromEditorEvent returns the document e describes; an event references nothing.
func FromEditorEvent(e EditorEventDocument) EventDocument {
	return EventDocument{SchemaVersion: DocumentSchemaVersion, Name: e.Name, Kind: e.Kind, DescriptionMD: e.DescriptionMD}
}

// References returns the references e makes.
func (e EditorPieceDocument) References() EditorRefs {
	var refs EditorRefs
	for _, t := range e.Terms {
		refs.TermSlugs = append(refs.TermSlugs, t.Term)
	}
	for _, r := range e.Relations {
		refs.PieceSlugs = append(refs.PieceSlugs, r.Piece)
	}
	for _, c := range e.Credits {
		refs.PersonSlugs = appendID(refs.PersonSlugs, c.Person)
	}
	for _, l := range e.Links {
		refs.CollectionIDs = appendID(refs.CollectionIDs, l.CollectionID)
		for _, p := range l.Performances {
			p.addReferences(&refs)
		}
	}

	return refs
}

// References returns the references e makes.
func (e EditorPersonDocument) References() EditorRefs {
	refs := EditorRefs{TermSlugs: slices.Clone(e.Affiliations)}
	for _, r := range e.Relations {
		refs.PersonSlugs = append(refs.PersonSlugs, r.Person)
	}
	for _, l := range e.Links {
		refs.CollectionIDs = appendID(refs.CollectionIDs, l.CollectionID)
		for _, p := range l.Performances {
			refs.PieceSlugs = appendID(refs.PieceSlugs, p.Piece)
			p.addReferences(&refs)
		}
	}

	return refs
}

// References returns the references e makes.
func (e EditorTermDocument) References() EditorRefs {
	return EditorRefs{TermSlugs: appendID(appendID(nil, e.Parent), e.RelatedAffiliation)}
}

// References returns the references e makes.
func (e EditorCollectionDocument) References() EditorRefs {
	return EditorRefs{TermSlugs: appendID(nil, e.Region), CollectionIDs: appendID(nil, e.ParentID)}
}

func (p EditorPerformance) addReferences(refs *EditorRefs) {
	refs.EventSlugs = appendID(refs.EventSlugs, p.Event)
	refs.PersonSlugs = append(refs.PersonSlugs, p.Performers...)
}

// AnyEditorDocument is an editor document of one of the five types, for values whose entity type is known only at
// run time.
type AnyEditorDocument interface {
	// EntityType is the type of entity the document is of.
	EntityType() EntityType
	// References returns the references the document makes.
	References() EditorRefs
}

// EntityType is EntityTypePiece.
func (EditorPieceDocument) EntityType() EntityType { return EntityTypePiece }

// EntityType is EntityTypePerson.
func (EditorPersonDocument) EntityType() EntityType { return EntityTypePerson }

// EntityType is EntityTypeTerm.
func (EditorTermDocument) EntityType() EntityType { return EntityTypeTerm }

// EntityType is EntityTypeCollection.
func (EditorCollectionDocument) EntityType() EntityType { return EntityTypeCollection }

// EntityType is EntityTypeEvent.
func (EditorEventDocument) EntityType() EntityType { return EntityTypeEvent }

// References returns the references e makes: none, as an event references nothing.
func (EditorEventDocument) References() EditorRefs { return EditorRefs{} }

// ErrUnknownDocumentType is a document of no type the editor format has.
var ErrUnknownDocumentType = errors.New("unknown document type")

// ToEditor returns d in the editor format with the slugs s, as the ToEditor function of its type does.
func ToEditor(d AnyDocument, s SlugMap) (AnyEditorDocument, error) {
	switch d := d.(type) {
	case PieceDocument:
		return ToEditorPiece(d, s), nil
	case PersonDocument:
		return ToEditorPerson(d, s), nil
	case TermDocument:
		return ToEditorTerm(d, s), nil
	case CollectionDocument:
		return ToEditorCollection(d, s), nil
	case EventDocument:
		return ToEditorEvent(d), nil
	default:
		return nil, fmt.Errorf("%w [%T]", ErrUnknownDocumentType, d)
	}
}

// FromEditor returns the document e describes, with the problems of the references it could not resolve, as the
// FromEditor function of its type does.
func FromEditor(e AnyEditorDocument, refs RefMap) (AnyDocument, []DocumentProblem, error) {
	switch e := e.(type) {
	case EditorPieceDocument:
		d, problems := FromEditorPiece(e, refs)
		return d, problems, nil
	case EditorPersonDocument:
		d, problems := FromEditorPerson(e, refs)
		return d, problems, nil
	case EditorTermDocument:
		d, problems := FromEditorTerm(e, refs)
		return d, problems, nil
	case EditorCollectionDocument:
		d, problems := FromEditorCollection(e, refs)
		return d, problems, nil
	case EditorEventDocument:
		return FromEditorEvent(e), nil, nil
	default:
		return nil, nil, fmt.Errorf("%w [%T]", ErrUnknownDocumentType, e)
	}
}

// optionalKey is a performance key in the editor format: null for none.
func optionalKey(key string) *string {
	if key == "" {
		return nil
	}

	return &key
}

// derefKey is a performance key from the editor format: empty for null.
func derefKey(key *string) string {
	if key == nil {
		return ""
	}

	return *key
}
