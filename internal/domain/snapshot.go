package domain

import (
	"errors"
	"fmt"
)

// Snapshot views are what the public API shows of a stored snapshot document. A document is never served as
// stored: it references other entities by internal ID and may hold what a public read must not show. A view
// holds the document's editor-owned fields, with:
//   - each reference resolved to the entity's summary as it is now, or left out if the entity is not public now;
//   - each link shown as written in the document, unless the link it is (the links row approval upserts it to,
//     by normalized URL) is hidden now;
//   - a person's name and aliases as the document has them;
//   - credits, performers and relations of persons who are not public now left out, and of persons who prefer no
//     page now named without a link;
//   - of a person who prefers no page, now or in the document, only the identity.
//
// Redaction follows the catalogue's current state, not the document's, so that hiding a link also hides it in
// every earlier revision.

// SnapshotReferences are the other entities and the links that one or more documents reference, which the
// SnapshotVisibility for their views must cover. IDs and URLs may repeat.
type SnapshotReferences struct {
	PieceIDs      []int64
	PersonIDs     []int64
	TermIDs       []int64
	CollectionIDs []int64
	EventIDs      []int64
	LinkURLs      []string
}

// SnapshotVisibility is the current state of the entities and links some documents reference, as far as it
// decides what their views show. Each map holds only the referenced entities it describes: an ID in no map is not
// public.
type SnapshotVisibility struct {
	// Pieces are the public pieces.
	Pieces map[int64]PieceSummary
	// Persons are the public persons. NoLink marks those who prefer no page now, of whom a person snapshot shows
	// only the identity.
	Persons map[int64]PersonSummary
	// Terms are the public terms: approved, with every ancestor approved.
	Terms map[int64]TermSummary
	// Collections are the public collections.
	Collections map[int64]CollectionSummary
	// Events are the public events and venues.
	Events map[int64]EventSummary
	// HiddenLinks holds each referenced URL, as the documents write it, whose link is hidden now; a view leaves those
	// links out.
	HiddenLinks map[string]bool
}

// PieceSnapshot is the public view of a PieceDocument.
type PieceSnapshot struct {
	Title                 string
	Kind                  PieceKind
	SecondaryKinds        []PieceKind
	ConservationStatus    ConservationStatus
	ConservationSourceURL *string
	IsStub                bool
	NotesMD               *string
	VersionNotesMD        *string
	LicensingStatus       LicensingStatus
	LicensingNotesMD      *string
	Licence               *Licence
	LicenceVersion        *LicenceVersion
	HistoricalSourceMD    *string
	PeriodStart           *int16
	PeriodEnd             *int16
	Aliases               []PieceAlias
	// Terms are the public terms, in the document's order.
	Terms []PieceTerm
	// Credits are in the document's order; a named credit to a person who is not public is left out.
	Credits []PieceCredit
	// Relations are the piece's relations to public pieces: a document holds its outgoing relations only.
	Relations []SnapshotPieceRelation
	// Links are the visible links, in the document's order.
	Links []SnapshotPieceLink
}

// SnapshotPieceRelation is a relation of the snapshot's piece to another public piece.
type SnapshotPieceRelation struct {
	Type  PieceRelationType
	Piece PieceSummary
}

// SnapshotLink is the editor-owned part of a visible link in a snapshot: no liveness or other system state.
type SnapshotLink struct {
	URL         string
	Title       *string
	Description *string
	Platform    *string
	PlaceType   *LinkPlaceType
	// Collection is the link's collection, if it is public.
	Collection *CollectionRef
	Locator    *string
	Roles      []LinkRole
}

// SnapshotPieceLink is a visible link of a piece snapshot, with the performances of the piece it records.
type SnapshotPieceLink struct {
	SnapshotLink
	Performances []PiecePerformance
}

// SnapshotPersonLink is a visible link of a person snapshot, with the performances it records. A performance of
// a piece that is not public is left out.
type SnapshotPersonLink struct {
	SnapshotLink
	Performances []SnapshotPersonPerformance
}

// SnapshotPersonPerformance is a performance a person's link records, of Piece if it is not nil.
type SnapshotPersonPerformance struct {
	Piece *PieceSummary
	PiecePerformance
}

// PersonSnapshot is the public view of a PersonDocument. Profile is nil, and IsStub true, for a person who prefers
// no page now or in the document, of whom the view shows the identity only.
type PersonSnapshot struct {
	Name            string
	Kind            PersonKind
	Status          PersonStatus
	StatusDate      *Date
	StatusSourceURL *string
	IsStub          bool
	Profile         *PersonSnapshotProfile
}

// PersonSnapshotProfile is everything a person snapshot shows beyond the identity.
type PersonSnapshotProfile struct {
	Pronouns         *string
	NotesMD          *string
	LicensingStatus  LicensingStatus
	LicensingNotesMD *string
	// Aliases are the document's aliases.
	Aliases      []PersonAlias
	Roles        []PersonRole
	Affiliations []TermSummary
	// Relations are the person's relations to public persons: a document holds its outgoing relations only.
	Relations []SnapshotPersonRelation
	Links     []SnapshotPersonLink
}

// SnapshotPersonRelation is a relation of the snapshot's person to another public person.
type SnapshotPersonRelation struct {
	Type   PersonRelationType
	Person PersonSummary
}

// TermSnapshot is the public view of a TermDocument.
type TermSnapshot struct {
	Facet       Facet
	Name        string
	DisplayName string
	// Parent is the term's parent, if it is public.
	Parent *TermSummary
	// RelatedAffiliation is the affiliation term the term implies, if it is public.
	RelatedAffiliation *TermSummary
	DescriptionMD      *string
	Position           int32
	Aliases            []string
}

// CollectionSnapshot is the public view of a CollectionDocument.
type CollectionSnapshot struct {
	Name          string
	URL           *string
	Kind          CollectionKind
	DescriptionMD *string
	// Region is the collection's region term, if it is public.
	Region *TermSummary
	// Parent is the collection's parent, if it is public.
	Parent           *CollectionSummary
	IsBardicResource bool
	Enumerable       CollectionEnumerable
}

// EventSnapshot is the public view of an EventDocument.
type EventSnapshot struct {
	Name          string
	Kind          EventKind
	DescriptionMD *string
}

// AddReferences adds what the document references to refs.
func (d PieceDocument) AddReferences(refs *SnapshotReferences) {
	for _, term := range d.Terms {
		refs.TermIDs = append(refs.TermIDs, term.TermID)
	}
	for _, relation := range d.Relations {
		refs.PieceIDs = append(refs.PieceIDs, relation.ToPieceID)
	}
	for _, credit := range d.Credits {
		if credit.PersonID != nil {
			refs.PersonIDs = append(refs.PersonIDs, *credit.PersonID)
		}
	}
	for _, link := range d.Links {
		link.addReferences(refs)
		for _, performance := range link.Performances {
			performance.addReferences(refs)
		}
	}
}

// AddReferences adds what the document of the person personID references to refs, including the person.
func (d PersonDocument) AddReferences(personID int64, refs *SnapshotReferences) {
	refs.PersonIDs = append(refs.PersonIDs, personID)
	refs.TermIDs = append(refs.TermIDs, d.AffiliationIDs...)
	for _, relation := range d.Relations {
		refs.PersonIDs = append(refs.PersonIDs, relation.ToPersonID)
	}
	for _, link := range d.Links {
		link.addReferences(refs)
		for _, performance := range link.Performances {
			if performance.PieceID != nil {
				refs.PieceIDs = append(refs.PieceIDs, *performance.PieceID)
			}
			performance.addReferences(refs)
		}
	}
}

// AddReferences adds what the document references to refs.
func (d TermDocument) AddReferences(refs *SnapshotReferences) {
	refs.TermIDs = appendID(refs.TermIDs, d.ParentID)
	refs.TermIDs = appendID(refs.TermIDs, d.RelatedAffiliationID)
}

// AddReferences adds what the document references to refs.
func (d CollectionDocument) AddReferences(refs *SnapshotReferences) {
	refs.TermIDs = appendID(refs.TermIDs, d.RegionTermID)
	refs.CollectionIDs = appendID(refs.CollectionIDs, d.ParentID)
}

func (l DocLink) addReferences(refs *SnapshotReferences) {
	refs.LinkURLs = append(refs.LinkURLs, l.URL)
	refs.CollectionIDs = appendID(refs.CollectionIDs, l.CollectionID)
}

func (p DocPerformance) addReferences(refs *SnapshotReferences) {
	refs.EventIDs = appendID(refs.EventIDs, p.EventID)
	refs.PersonIDs = append(refs.PersonIDs, p.PerformerIDs...)
}

// appendID appends *id to ids if id is not nil.
func appendID(ids []int64, id *int64) []int64 {
	if id == nil {
		return ids
	}

	return append(ids, *id)
}

// NewPieceSnapshot returns the public view of a piece document.
func NewPieceSnapshot(d PieceDocument, vis SnapshotVisibility) PieceSnapshot {
	snapshot := PieceSnapshot{
		Title:                 d.Title,
		Kind:                  d.Kind,
		SecondaryKinds:        d.SecondaryKinds,
		ConservationStatus:    d.Conservation.Status,
		ConservationSourceURL: d.Conservation.SourceURL,
		IsStub:                d.IsStub,
		NotesMD:               d.NotesMD,
		VersionNotesMD:        d.VersionNotesMD,
		LicensingStatus:       d.LicensingStatus,
		LicensingNotesMD:      d.LicensingNotesMD,
		Licence:               d.Licence,
		LicenceVersion:        VisibleLicenceVersion(d.Licence, d.LicenceVersion),
		HistoricalSourceMD:    d.HistoricalSourceMD,
		PeriodStart:           d.PeriodStart,
		PeriodEnd:             d.PeriodEnd,
	}
	for _, alias := range d.Aliases {
		snapshot.Aliases = append(snapshot.Aliases, PieceAlias(alias))
	}
	for _, term := range d.Terms {
		if summary, ok := vis.Terms[term.TermID]; ok {
			snapshot.Terms = append(snapshot.Terms, PieceTerm{Term: summary, IsPrimary: term.IsPrimary, Part: term.Part})
		}
	}
	for _, credit := range d.Credits {
		if c, ok := vis.credit(credit); ok {
			snapshot.Credits = append(snapshot.Credits, c)
		}
	}
	for _, relation := range d.Relations {
		if piece, ok := vis.Pieces[relation.ToPieceID]; ok {
			snapshot.Relations = append(snapshot.Relations, SnapshotPieceRelation{Type: relation.Type, Piece: piece})
		}
	}
	for _, link := range d.Links {
		if vis.HiddenLinks[link.URL] {
			continue
		}
		pieceLink := SnapshotPieceLink{SnapshotLink: vis.link(link.DocLink)}
		for _, performance := range link.Performances {
			pieceLink.Performances = append(pieceLink.Performances, vis.performance(performance))
		}
		snapshot.Links = append(snapshot.Links, pieceLink)
	}

	return snapshot
}

// credit returns the view of a credit, or false if it is a named credit to a person who is not public.
// A credit that is not named never shows a person.
func (v SnapshotVisibility) credit(c DocCredit) (PieceCredit, bool) {
	credit := PieceCredit{
		Role: c.Role, Attribution: c.Attribution, Certainty: c.Certainty, Part: c.Part, Note: c.Note,
	}
	if c.Attribution != AttributionNamed {
		return credit, true
	}
	if c.PersonID == nil {
		return PieceCredit{}, false
	}
	person, ok := v.Persons[*c.PersonID]
	if !ok {
		return PieceCredit{}, false
	}
	credit.Person = &person

	return credit, true
}

// link returns the view of l.
func (v SnapshotVisibility) link(l DocLink) SnapshotLink {
	link := SnapshotLink{
		URL:         l.URL,
		Title:       l.Title,
		Description: l.Description,
		Platform:    l.Platform,
		PlaceType:   l.PlaceType,
		Locator:     l.Locator,
		Roles:       l.Roles,
	}
	if l.CollectionID != nil {
		if collection, ok := v.Collections[*l.CollectionID]; ok {
			link.Collection = &CollectionRef{ID: collection.ID, Name: collection.Name}
		}
	}

	return link
}

func (v SnapshotVisibility) performance(p DocPerformance) PiecePerformance {
	performance := PiecePerformance{Year: p.Year, Uploader: p.Uploader, DescriptionVerbatim: p.DescriptionVerbatim}
	if p.EventID != nil {
		if event, ok := v.Events[*p.EventID]; ok {
			performance.Event = &event
		}
	}
	for _, id := range p.PerformerIDs {
		if person, ok := v.Persons[id]; ok {
			performance.Performers = append(performance.Performers, person)
		}
	}

	return performance
}

// ErrPersonNotPublic is returned by NewPersonSnapshot for a person vis does not hold as public.
var ErrPersonNotPublic = errors.New("person is not public")

// NewPersonSnapshot returns the public view of the document of the person personID. It fails with
// ErrPersonNotPublic if vis does not hold the person as public.
func NewPersonSnapshot(personID int64, d PersonDocument, vis SnapshotVisibility) (PersonSnapshot, error) {
	person, public := vis.Persons[personID]
	if !public {
		return PersonSnapshot{}, fmt.Errorf("%w [%d]", ErrPersonNotPublic, personID)
	}

	snapshot := PersonSnapshot{
		Name:            d.Name,
		Kind:            d.Kind,
		Status:          d.Status,
		StatusDate:      d.StatusDate,
		StatusSourceURL: d.StatusSourceURL,
		IsStub:          d.IsStub,
	}

	if person.NoLink || d.Status == PersonStatusPrefersNoPage {
		snapshot.IsStub = true
		return snapshot, nil
	}

	profile := newPersonSnapshotProfile(d, vis)
	snapshot.Profile = &profile

	return snapshot, nil
}

// newPersonSnapshotProfile returns the profile of the view of the document of a person who has a page.
func newPersonSnapshotProfile(d PersonDocument, vis SnapshotVisibility) PersonSnapshotProfile {
	profile := PersonSnapshotProfile{
		Pronouns:         d.Pronouns,
		NotesMD:          d.NotesMD,
		LicensingStatus:  d.LicensingStatus,
		LicensingNotesMD: d.LicensingNotesMD,
		Roles:            d.Roles,
	}
	for _, alias := range d.Aliases {
		profile.Aliases = append(profile.Aliases, PersonAlias(alias))
	}
	for _, id := range d.AffiliationIDs {
		if term, ok := vis.Terms[id]; ok {
			profile.Affiliations = append(profile.Affiliations, term)
		}
	}
	for _, relation := range d.Relations {
		if person, ok := vis.Persons[relation.ToPersonID]; ok {
			profile.Relations = append(profile.Relations, SnapshotPersonRelation{Type: relation.Type, Person: person})
		}
	}
	for _, link := range d.Links {
		if vis.HiddenLinks[link.URL] {
			continue
		}
		personLink := SnapshotPersonLink{SnapshotLink: vis.link(link.DocLink)}
		for _, performance := range link.Performances {
			if p, ok := vis.personPerformance(performance); ok {
				personLink.Performances = append(personLink.Performances, p)
			}
		}
		profile.Links = append(profile.Links, personLink)
	}

	return profile
}

// personPerformance returns the view of a performance a person's link records, or false if it is of a piece that
// is not public: its description may name the piece.
func (v SnapshotVisibility) personPerformance(p DocPersonPerformance) (SnapshotPersonPerformance, bool) {
	performance := SnapshotPersonPerformance{PiecePerformance: v.performance(p.DocPerformance)}
	if p.PieceID != nil {
		piece, ok := v.Pieces[*p.PieceID]
		if !ok {
			return SnapshotPersonPerformance{}, false
		}
		performance.Piece = &piece
	}

	return performance, true
}

// NewTermSnapshot returns the public view of a term document.
func NewTermSnapshot(d TermDocument, vis SnapshotVisibility) TermSnapshot {
	return TermSnapshot{
		Facet:              d.Facet,
		Name:               d.Name,
		DisplayName:        d.DisplayName,
		Parent:             lookup(vis.Terms, d.ParentID),
		RelatedAffiliation: lookup(vis.Terms, d.RelatedAffiliationID),
		DescriptionMD:      d.DescriptionMD,
		Position:           d.Position,
		Aliases:            d.Aliases,
	}
}

// NewCollectionSnapshot returns the public view of a collection document.
func NewCollectionSnapshot(d CollectionDocument, vis SnapshotVisibility) CollectionSnapshot {
	return CollectionSnapshot{
		Name:             d.Name,
		URL:              d.URL,
		Kind:             d.Kind,
		DescriptionMD:    d.DescriptionMD,
		Region:           lookup(vis.Terms, d.RegionTermID),
		Parent:           lookup(vis.Collections, d.ParentID),
		IsBardicResource: d.IsBardicResource,
		Enumerable:       d.Enumerable,
	}
}

// NewEventSnapshot returns the public view of an event document, which references nothing.
func NewEventSnapshot(d EventDocument) EventSnapshot {
	return EventSnapshot{Name: d.Name, Kind: d.Kind, DescriptionMD: d.DescriptionMD}
}

// lookup returns the entry of entities for *id, or nil if id is nil or has no entry.
func lookup[T any](entities map[int64]T, id *int64) *T {
	if id == nil {
		return nil
	}
	entity, ok := entities[*id]
	if !ok {
		return nil
	}

	return &entity
}
