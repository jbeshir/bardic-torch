package domain

import (
	"encoding/json"
	"fmt"
)

// Editor documents are what editors read and submit: the stored documents' JSON with every reference to a piece,
// person, term or event given by slug (*_slug) instead of ID. Collections have no slug and stay IDs. ToEditor
// leaves out a reference with no slug, as public reads do; FromEditor reports an unknown slug as
// ProblemUnknownReference at its path.

// Slugs are the slugs of the public entities a document references, by ID (the IDs AddReferences collects).
type Slugs struct {
	Pieces, Persons, Terms, Events map[int64]string
}

// IDs are the IDs of the entities an editor document references, by slug (the slugs References lists).
type IDs struct {
	Pieces, Persons, Terms, Events map[string]int64
}

// SlugReferences are the slugs an editor document references, by type. Slugs may repeat.
type SlugReferences struct {
	Pieces, Persons, Terms, Events []string
}

// EditorPieceDocument is a PieceDocument in the editor format.
type EditorPieceDocument struct {
	SchemaVersion      int                   `json:"schema_version"`
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
}

// EditorPieceTerm is a DocPieceTerm in the editor format.
type EditorPieceTerm struct {
	TermSlug  string     `json:"term_slug"`
	IsPrimary bool       `json:"is_primary"`
	Part      *PiecePart `json:"part"`
}

// EditorPieceRelation is a DocPieceRelation in the editor format.
type EditorPieceRelation struct {
	ToPieceSlug string            `json:"to_piece_slug"`
	Type        PieceRelationType `json:"type"`
}

// EditorCredit is a DocCredit in the editor format.
type EditorCredit struct {
	PersonSlug  *string     `json:"person_slug"`
	Role        CreditRole  `json:"role"`
	Attribution Attribution `json:"attribution"`
	Certainty   Certainty   `json:"certainty"`
	Part        *PiecePart  `json:"part"`
	Note        *string     `json:"note"`
}

// EditorPieceLink is a DocPieceLink in the editor format.
type EditorPieceLink struct {
	DocLink
	Performances []EditorPerformance `json:"performances"`
}

// EditorPersonLink is a DocPersonLink in the editor format.
type EditorPersonLink struct {
	DocLink
	Performances []EditorPersonPerformance `json:"performances"`
}

// EditorPerformance is a DocPerformance in the editor format.
type EditorPerformance struct {
	EventSlug           *string  `json:"event_slug"`
	Year                *int16   `json:"year"`
	Uploader            *string  `json:"uploader"`
	DescriptionVerbatim string   `json:"description_verbatim"`
	PerformerSlugs      []string `json:"performer_slugs"`
}

// EditorPersonPerformance is a DocPersonPerformance in the editor format.
type EditorPersonPerformance struct {
	PieceSlug *string `json:"piece_slug"`
	EditorPerformance
}

// EditorPersonDocument is a PersonDocument in the editor format.
type EditorPersonDocument struct {
	SchemaVersion    int                    `json:"schema_version"`
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
	AffiliationSlugs []string               `json:"affiliation_slugs"`
	Relations        []EditorPersonRelation `json:"relations"`
	Links            []EditorPersonLink     `json:"links"`
}

// EditorPersonRelation is a DocPersonRelation in the editor format.
type EditorPersonRelation struct {
	ToPersonSlug string             `json:"to_person_slug"`
	Type         PersonRelationType `json:"type"`
}

// EditorTermDocument is a TermDocument in the editor format.
type EditorTermDocument struct {
	SchemaVersion          int      `json:"schema_version"`
	Facet                  Facet    `json:"facet"`
	Name                   string   `json:"name"`
	DisplayName            string   `json:"display_name"`
	ParentSlug             *string  `json:"parent_slug"`
	RelatedAffiliationSlug *string  `json:"related_affiliation_slug"`
	DescriptionMD          *string  `json:"description_md"`
	Position               int32    `json:"position"`
	Aliases                []string `json:"aliases"`
}

// EditorCollectionDocument is a CollectionDocument in the editor format.
type EditorCollectionDocument struct {
	SchemaVersion    int                  `json:"schema_version"`
	Name             string               `json:"name"`
	URL              *string              `json:"url"`
	Kind             CollectionKind       `json:"kind"`
	DescriptionMD    *string              `json:"description_md"`
	RegionTermSlug   *string              `json:"region_term_slug"`
	ParentID         *int64               `json:"parent_id"`
	IsBardicResource bool                 `json:"is_bardic_resource"`
	Enumerable       CollectionEnumerable `json:"enumerable"`
}

// EditorEventDocument is an EventDocument in the editor format, which is the same: events reference nothing.
type EditorEventDocument EventDocument

// ToEditor returns the piece document in the editor format.
func (d PieceDocument) ToEditor(s Slugs) EditorPieceDocument {
	e := copyFields[EditorPieceDocument](d)
	for k, t := range d.Terms {
		e.Terms[k].TermSlug = s.Terms[t.TermID]
	}
	e.Terms = keep(e.Terms, func(k int) bool { return e.Terms[k].TermSlug != "" })
	for k, r := range d.Relations {
		e.Relations[k].ToPieceSlug = s.Pieces[r.ToPieceID]
	}
	e.Relations = keep(e.Relations, func(k int) bool { return e.Relations[k].ToPieceSlug != "" })
	for k, c := range d.Credits {
		e.Credits[k].PersonSlug = slugOf(s.Persons, c.PersonID)
	}
	e.Credits = keep(e.Credits, func(k int) bool {
		return d.Credits[k].PersonID == nil || e.Credits[k].PersonSlug != nil
	})
	for k, l := range d.Links {
		for j, p := range l.Performances {
			e.Links[k].Performances[j] = p.toEditor(e.Links[k].Performances[j], s)
		}
		e.Links[k].Performances = orEmpty(e.Links[k].Performances)
	}
	e.SecondaryKinds, e.Aliases, e.Links = orEmpty(e.SecondaryKinds), orEmpty(e.Aliases), orEmpty(e.Links)

	return e
}

// FromEditor returns the piece document of the editor document, and the slugs it does not know.
func (e EditorPieceDocument) FromEditor(ids IDs) (PieceDocument, []Problem) {
	d := copyFields[PieceDocument](e)
	var p problems
	for k, t := range e.Terms {
		d.Terms[k].TermID = p.resolve(ids.Terms, t.TermSlug, fmt.Sprintf("terms[%d].term_slug", k))
	}
	for k, r := range e.Relations {
		d.Relations[k].ToPieceID = p.resolve(ids.Pieces, r.ToPieceSlug, fmt.Sprintf("relations[%d].to_piece_slug", k))
	}
	for k, c := range e.Credits {
		d.Credits[k].PersonID = p.resolveOptional(ids.Persons, c.PersonSlug, fmt.Sprintf("credits[%d].person_slug", k))
	}
	for k, l := range e.Links {
		for j, perf := range l.Performances {
			d.Links[k].Performances[j] = p.resolvePerformance(perf, d.Links[k].Performances[j], ids,
				fmt.Sprintf("links[%d].performances[%d]", k, j))
		}
	}

	return d, p
}

// References returns the slugs the document references.
func (e EditorPieceDocument) References() SlugReferences {
	var r SlugReferences
	for _, t := range e.Terms {
		r.Terms = append(r.Terms, t.TermSlug)
	}
	for _, rel := range e.Relations {
		r.Pieces = append(r.Pieces, rel.ToPieceSlug)
	}
	for _, c := range e.Credits {
		r.Persons = appendSlug(r.Persons, c.PersonSlug)
	}
	for _, l := range e.Links {
		for _, p := range l.Performances {
			p.addReferences(&r)
		}
	}

	return r
}

// ToEditor returns the person document in the editor format.
func (d PersonDocument) ToEditor(s Slugs) EditorPersonDocument {
	e := copyFields[EditorPersonDocument](d)
	e.AffiliationSlugs = slugsOf(s.Terms, d.AffiliationIDs)
	for k, r := range d.Relations {
		e.Relations[k].ToPersonSlug = s.Persons[r.ToPersonID]
	}
	e.Relations = keep(e.Relations, func(k int) bool { return e.Relations[k].ToPersonSlug != "" })
	for k, l := range d.Links {
		for j, p := range l.Performances {
			e.Links[k].Performances[j].EditorPerformance = p.toEditor(e.Links[k].Performances[j].EditorPerformance, s)
			e.Links[k].Performances[j].PieceSlug = slugOf(s.Pieces, p.PieceID)
		}
		e.Links[k].Performances = keep(e.Links[k].Performances, func(j int) bool {
			return l.Performances[j].PieceID == nil || e.Links[k].Performances[j].PieceSlug != nil
		})
	}
	e.Aliases, e.Roles, e.Links = orEmpty(e.Aliases), orEmpty(e.Roles), orEmpty(e.Links)

	return e
}

// FromEditor returns the person document of the editor document, and the slugs it does not know.
func (e EditorPersonDocument) FromEditor(ids IDs) (PersonDocument, []Problem) {
	d := copyFields[PersonDocument](e)
	var p problems
	if e.AffiliationSlugs != nil {
		d.AffiliationIDs = make([]int64, len(e.AffiliationSlugs))
	}
	for k, slug := range e.AffiliationSlugs {
		d.AffiliationIDs[k] = p.resolve(ids.Terms, slug, fmt.Sprintf("affiliation_slugs[%d]", k))
	}
	for k, r := range e.Relations {
		d.Relations[k].ToPersonID = p.resolve(ids.Persons, r.ToPersonSlug,
			fmt.Sprintf("relations[%d].to_person_slug", k))
	}
	for k, l := range e.Links {
		for j, perf := range l.Performances {
			path := fmt.Sprintf("links[%d].performances[%d]", k, j)
			out := &d.Links[k].Performances[j]
			out.PieceID = p.resolveOptional(ids.Pieces, perf.PieceSlug, path+".piece_slug")
			out.DocPerformance = p.resolvePerformance(perf.EditorPerformance, out.DocPerformance, ids, path)
		}
	}

	return d, p
}

// References returns the slugs the document references.
func (e EditorPersonDocument) References() SlugReferences {
	r := SlugReferences{Terms: e.AffiliationSlugs}
	for _, rel := range e.Relations {
		r.Persons = append(r.Persons, rel.ToPersonSlug)
	}
	for _, l := range e.Links {
		for _, p := range l.Performances {
			r.Pieces = appendSlug(r.Pieces, p.PieceSlug)
			p.addReferences(&r)
		}
	}

	return r
}

// ToEditor returns the term document in the editor format.
func (d TermDocument) ToEditor(s Slugs) EditorTermDocument {
	e := copyFields[EditorTermDocument](d)
	e.ParentSlug = slugOf(s.Terms, d.ParentID)
	e.RelatedAffiliationSlug = slugOf(s.Terms, d.RelatedAffiliationID)
	e.Aliases = orEmpty(e.Aliases)

	return e
}

// FromEditor returns the term document of the editor document, and the slugs it does not know.
func (e EditorTermDocument) FromEditor(ids IDs) (TermDocument, []Problem) {
	d := copyFields[TermDocument](e)
	var p problems
	d.ParentID = p.resolveOptional(ids.Terms, e.ParentSlug, "parent_slug")
	d.RelatedAffiliationID = p.resolveOptional(ids.Terms, e.RelatedAffiliationSlug, "related_affiliation_slug")

	return d, p
}

// References returns the slugs the document references.
func (e EditorTermDocument) References() SlugReferences {
	return SlugReferences{Terms: appendSlug(appendSlug(nil, e.ParentSlug), e.RelatedAffiliationSlug)}
}

// ToEditor returns the collection document in the editor format.
func (d CollectionDocument) ToEditor(s Slugs) EditorCollectionDocument {
	e := copyFields[EditorCollectionDocument](d)
	e.RegionTermSlug = slugOf(s.Terms, d.RegionTermID)

	return e
}

// FromEditor returns the collection document of the editor document, and the slugs it does not know.
func (e EditorCollectionDocument) FromEditor(ids IDs) (CollectionDocument, []Problem) {
	d := copyFields[CollectionDocument](e)
	var p problems
	d.RegionTermID = p.resolveOptional(ids.Terms, e.RegionTermSlug, "region_term_slug")

	return d, p
}

// References returns the slugs the document references.
func (e EditorCollectionDocument) References() SlugReferences {
	return SlugReferences{Terms: appendSlug(nil, e.RegionTermSlug)}
}

// ToEditor returns the event document in the editor format.
func (d EventDocument) ToEditor(Slugs) EditorEventDocument { return EditorEventDocument(d) }

// FromEditor returns the event document of the editor document.
func (e EditorEventDocument) FromEditor(IDs) (EventDocument, []Problem) { return EventDocument(e), nil }

// References returns the slugs the document references: none.
func (e EditorEventDocument) References() SlugReferences { return SlugReferences{} }

// toEditor returns e, the performance's other fields in the editor format, with its event and performers.
func (p DocPerformance) toEditor(e EditorPerformance, s Slugs) EditorPerformance {
	e.EventSlug = slugOf(s.Events, p.EventID)
	e.PerformerSlugs = slugsOf(s.Persons, p.PerformerIDs)

	return e
}

// resolvePerformance returns d, the performance's other fields, with the event and performers of e.
func (p *problems) resolvePerformance(e EditorPerformance, d DocPerformance, ids IDs, path string) DocPerformance {
	d.EventID = p.resolveOptional(ids.Events, e.EventSlug, path+".event_slug")
	if e.PerformerSlugs != nil {
		d.PerformerIDs = make([]int64, len(e.PerformerSlugs))
	}
	for k, slug := range e.PerformerSlugs {
		d.PerformerIDs[k] = p.resolve(ids.Persons, slug, fmt.Sprintf("%s.performer_slugs[%d]", path, k))
	}

	return d
}

func (e EditorPerformance) addReferences(r *SlugReferences) {
	r.Events = appendSlug(r.Events, e.EventSlug)
	r.Persons = append(r.Persons, e.PerformerSlugs...)
}

// resolve returns the ID of slug, reporting ProblemUnknownReference at path if ids does not have it.
func (p *problems) resolve(ids map[string]int64, slug, path string) int64 {
	id, ok := ids[slug]
	p.check(ok, path, ProblemUnknownReference)

	return id
}

// resolveOptional is resolve for an optional reference.
func (p *problems) resolveOptional(ids map[string]int64, slug *string, path string) *int64 {
	if slug == nil {
		return nil
	}
	id := p.resolve(ids, *slug, path)

	return &id
}

// slugOf returns the slug of the optional reference id, or nil if it has none.
func slugOf(slugs map[int64]string, id *int64) *string {
	if id == nil {
		return nil
	}
	if s, ok := slugs[*id]; ok {
		return &s
	}

	return nil
}

// slugsOf returns the slugs of ids, leaving out those with none.
func slugsOf(slugs map[int64]string, ids []int64) []string {
	out := []string{}
	for _, id := range ids {
		if s, ok := slugs[id]; ok {
			out = append(out, s)
		}
	}

	return out
}

func appendSlug(slugs []string, slug *string) []string {
	if slug == nil {
		return slugs
	}

	return append(slugs, *slug)
}

// keep returns the elements of list whose index ok holds for.
func keep[T any](list []T, ok func(k int) bool) []T {
	out := make([]T, 0, len(list))
	for k, x := range list {
		if ok(k) {
			out = append(out, x)
		}
	}

	return out
}

// orEmpty returns list, or an empty list if it is nil, so that editor documents encode lists as [].
func orEmpty[T any](list []T) []T {
	if list == nil {
		return []T{}
	}

	return list
}

// copyFields returns the JSON fields of from that T has: the editor and stored formats share every field but the
// references, which the caller sets. Documents always encode, so it cannot fail.
func copyFields[T any](from any) T {
	var to T
	data, err := json.Marshal(from)
	if err == nil {
		err = json.Unmarshal(data, &to)
	}
	if err != nil {
		panic(fmt.Sprintf("copying document fields: %v", err))
	}

	return to
}
