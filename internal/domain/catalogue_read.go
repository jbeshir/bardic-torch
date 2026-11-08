package domain

import "time"

// The public read models of the catalogue: what the API shows of public entities. They hold only public data:
// the datasources leave out everything a public read must not show (unapproved entities and references to them,
// hidden aliases and links, prefers_no_page persons' profiles and references to them) and every system field
// that is never exposed (internal IDs other than collections', legacy fields, claims, normalized URLs).

// PieceSummary is how a public piece is shown where another entity refers to it.
type PieceSummary struct {
	Slug               string
	Title              string
	Kind               PieceKind
	SecondaryKinds     []PieceKind
	ConservationStatus ConservationStatus
	IsStub             bool
}

// PersonSummary is how a public person is shown where another entity refers to it.
type PersonSummary struct {
	Slug string
	Name string
	Kind PersonKind
	// IsStub is true for a prefers_no_page person, whose page is only ever a stub; see PersonIsStub.
	IsStub bool
}

// PersonIsStub returns whether a person is shown as a stub: if they are marked as one, or prefer not to have a
// page.
func PersonIsStub(isStub bool, status PersonStatus) bool {
	return isStub || status == PersonStatusPrefersNoPage
}

// TermSummary is how a public term is shown where another entity refers to it.
type TermSummary struct {
	Slug        string
	Facet       Facet
	DisplayName string
}

// CollectionRef is how a public collection is shown where another entity refers to it. Collections are
// addressed by ID.
type CollectionRef struct {
	ID   int64
	Name string
}

// EventSummary is how a public event or venue is shown where another entity refers to it.
type EventSummary struct {
	Slug string
	Name string
	Kind EventKind
}

// RelationDirection is which way a relation between two pieces or two persons points from the entity shown.
type RelationDirection string

const (
	// RelationDirectionOutgoing is a relation the entity shown has to the other: "this is a variant of that".
	RelationDirectionOutgoing RelationDirection = "outgoing"
	// RelationDirectionIncoming is a relation the other entity has to the entity shown.
	RelationDirectionIncoming RelationDirection = "incoming"
)

// Piece is a public piece with everything its page shows.
type Piece struct {
	PieceSummary
	// UpdatedAt is when the piece's current revision was approved.
	UpdatedAt        time.Time
	NotesMD          *string
	VersionNotesMD   *string
	LicensingStatus  LicensingStatus
	LicensingNotesMD *string
	Licence          *Licence
	// LicenceVersion is the version of Licence, nil unless it is a Creative Commons licence (ShownLicenceVersion).
	LicenceVersion     *LicenceVersion
	HistoricalSourceMD *string
	PeriodStart        *int16
	PeriodEnd          *int16
	Aliases            []PieceAlias
	// Terms are ordered by facet, in Facet's declaration order, then by position.
	Terms []PieceTerm
	// Credits are in position order. A credit to a person who is not public or prefers no page is left out.
	Credits []PieceCredit
	// Relations are the outgoing relations, then the incoming ones, each by type and the other piece's sort
	// title.
	Relations []PieceRelation
	// Links are ordered by their first attachment's position.
	Links []PieceLink
	// ConservationHistory is oldest first.
	ConservationHistory []ConservationChange
	// Preferences are the piece's stated preferences whose source a read shows (ShownPreferences), in dimension
	// order.
	Preferences []DocPreference
	// ConservationSuggestion is the status the piece's visible links suggest, nil for a restricted piece.
	ConservationSuggestion *ConservationSuggestion
}

// PieceAlias is another title of a piece.
type PieceAlias struct {
	Text string
	Kind PieceAliasKind
}

// PieceTerm is a public term a piece is classified with.
type PieceTerm struct {
	Term      TermSummary
	IsPrimary bool
	Part      *PiecePart
}

// PieceCredit is a credit of a piece. Person is set exactly for a named credit.
type PieceCredit struct {
	Role        CreditRole
	Attribution Attribution
	Certainty   Certainty
	Part        *PiecePart
	Note        *string
	Person      *PersonSummary
}

// PieceRelation is a relation between the piece shown and another public piece.
type PieceRelation struct {
	Type      PieceRelationType
	Direction RelationDirection
	Piece     PieceSummary
}

// Link is a visible link attached to a piece or person, once however many roles it is attached with.
type Link struct {
	URL           string
	Title         *string
	Description   *string
	Platform      *string
	PlaceType     *LinkPlaceType
	Collection    *CollectionRef
	Locator       *string
	Liveness      Liveness
	LastCheckedAt *time.Time
	ArchivedURL   *string
	// Roles are in attachment order.
	Roles []LinkRole
}

// PieceLink is a link attached to a piece, with the piece's performances it records.
type PieceLink struct {
	Link
	Performances []PiecePerformance
}

// PiecePerformance is a recorded performance of a piece. Performers who are not public or prefer no page are
// left out.
type PiecePerformance struct {
	Year                *int16
	Uploader            *string
	DescriptionVerbatim string
	Event               *EventSummary
	Performers          []PersonSummary
}

// ConservationChange is an entry in a piece's conservation history.
type ConservationChange struct {
	Status ConservationStatus
	Reason string
	// SourceURL is nil when the source is a hidden link, or does not normalize (ShownSource).
	SourceURL *string
	At        time.Time
}

// Person is a public person. Profile is nil exactly for a person who prefers not to have a page, of whom only
// the identity is shown.
type Person struct {
	PersonSummary
	// UpdatedAt is when the person's current revision was approved.
	UpdatedAt  time.Time
	Status     PersonStatus
	StatusDate *time.Time
	// StatusSourceURL is nil when the source is a hidden link, or does not normalize (ShownSource).
	StatusSourceURL *string
	Profile         *PersonProfile
}

// PersonProfile is everything a person's page shows beyond their identity.
type PersonProfile struct {
	Pronouns         *string
	NotesMD          *string
	LicensingStatus  LicensingStatus
	LicensingNotesMD *string
	// Aliases are the public aliases only.
	Aliases []PersonAlias
	// Roles are in PersonRole's declaration order.
	Roles []PersonRole
	// Affiliations are ordered by position, then display name.
	Affiliations []TermSummary
	// Relations are the outgoing relations, then the incoming ones, each by type and the other person's sort
	// name. Persons who prefer no page are left out.
	Relations []PersonRelation
	// Links are ordered by their first attachment's position.
	Links []Link
	// Works are the person's credits on public pieces, by the piece's sort title, at most MaxNestedListItems.
	Works      []PersonWork
	WorksTotal int64
	// Performances are those the person performs in or that links attached to them record, newest year first
	// (unknown years last), at most MaxNestedListItems. A performance of a piece that is not public is left out.
	Performances      []PersonPerformance
	PerformancesTotal int64
	// Preferences are the person's stated preferences whose source a read shows (ShownPreferences), in dimension
	// order.
	Preferences []DocPreference
}

// MaxNestedListItems caps nested lists that grow with the catalogue, such as a person's works; each capped list
// has a total beside it.
const MaxNestedListItems = 200

// PersonAlias is another public name of a person.
type PersonAlias struct {
	Name string
	Kind PersonAliasKind
}

// PersonRelation is a relation between the person shown and another public person.
type PersonRelation struct {
	Type      PersonRelationType
	Direction RelationDirection
	Person    PersonSummary
}

// PersonWork is a credit of a person on a public piece. Its attribution is always named.
type PersonWork struct {
	Piece     PieceSummary
	Role      CreditRole
	Certainty Certainty
	Part      *PiecePart
}

// PersonPerformance is a performance on a person's page. Piece is nil for a performance of no particular piece.
type PersonPerformance struct {
	Piece               *PieceSummary
	Link                PerformanceLink
	Year                *int16
	Event               *EventSummary
	DescriptionVerbatim string
}

// PerformanceLink is the link recording a performance, as a person's or event's page shows it.
type PerformanceLink struct {
	URL           string
	Title         *string
	Platform      *string
	Liveness      Liveness
	LastCheckedAt *time.Time
	ArchivedURL   *string
}

// Page is the window of a paginated list to return: at most Limit items, after skipping Offset.
type Page struct {
	Limit  int32
	Offset int32
}

// The bounds of a Page that requests may ask for.
const (
	// DefaultPageLimit is the limit of a request that sets none.
	DefaultPageLimit = 50
	// MaxPageLimit is the largest limit a request may set.
	MaxPageLimit = 100
	// MaxPageOffset is the largest offset a request may set.
	MaxPageOffset = 10000
)

// TermListItem is a public term in the list of terms, with how many public pieces and persons it classifies.
type TermListItem struct {
	TermSummary
	Name string
	// ParentSlug is the slug of the term's parent, or nil for a root term. A public term's parent is public.
	ParentSlug *string
	TermCounts
}

// TermCounts are how many distinct public pieces, and public persons who do not prefer no page, a term or any
// of its public descendants classifies.
type TermCounts struct {
	PieceCount  int64
	PersonCount int64
}

// Term is a public term's page.
type Term struct {
	TermSummary
	// UpdatedAt is when the term's current revision was approved.
	UpdatedAt     time.Time
	Name          string
	DescriptionMD *string
	Aliases       []string
	// Ancestors are the term's ancestors, root first.
	Ancestors []TermSummary
	// Children are the term's public children, by position, display name and ID.
	Children []TermChild
	// RelatedAffiliation is the public affiliation term this term implies, if any.
	RelatedAffiliation *TermSummary
	TermCounts
	// Pieces are one page of the public pieces the term or a descendant classifies, by sort title, for a term
	// that classifies pieces; otherwise empty. Their total is PieceCount.
	Pieces []PieceSummary
	// Persons are one page of the public persons, other than those who prefer no page, affiliated with the term
	// or a descendant, by sort name, for an affiliation term; otherwise empty. Their total is PersonCount.
	Persons []PersonSummary
}

// TermChild is a public child of a term, with its counts.
type TermChild struct {
	Term TermSummary
	TermCounts
}

// CollectionSummary is how a public collection is shown in lists of collections.
type CollectionSummary struct {
	ID               int64
	Name             string
	Kind             CollectionKind
	URL              *string
	IsBardicResource bool
}

// Collection is a public collection's page.
type Collection struct {
	CollectionSummary
	// UpdatedAt is when the collection's current revision was approved.
	UpdatedAt     time.Time
	DescriptionMD *string
	Enumerable    CollectionEnumerable
	// Region is the collection's public region term, if any.
	Region *TermSummary
	// Parent is the collection's public parent, if any.
	Parent *CollectionSummary
	// Children are the collection's public children, by sort name.
	Children []CollectionSummary
	// Citations are the visible links in the collection, once per public piece or person each is attached to,
	// by the piece's sort title or person's sort name, at most MaxNestedListItems. Persons who prefer no page are
	// left out.
	Citations      []Citation
	CitationsTotal int64
}

// Citation is a link in a collection attached to a piece or person: exactly one of Piece and Person is set.
type Citation struct {
	Piece         *PieceSummary
	Person        *PersonSummary
	URL           string
	Title         *string
	Locator       *string
	Liveness      Liveness
	LastCheckedAt *time.Time
	ArchivedURL   *string
	// Roles are the roles the link is attached to the piece or person with, in attachment order.
	Roles []LinkRole
}

// EventList is one page of the public events and venues, by name, with how many there are in all.
type EventList struct {
	Events []EventSummary
	Total  int64
}

// Event is a public event's or venue's page.
type Event struct {
	EventSummary
	// UpdatedAt is when the event's current revision was approved.
	UpdatedAt     time.Time
	DescriptionMD *string
	// Performances are the performances at the event on visible links, newest year first (unknown years last),
	// at most MaxNestedListItems. A performance of a piece that is not public is left out, as is one whose link
	// is attached to no public piece and no public person with a page.
	Performances      []EventPerformance
	PerformancesTotal int64
}

// EventPerformance is a performance on an event's page. Piece is nil for a performance of no particular piece.
// Performers who are not public or prefer no page are left out.
type EventPerformance struct {
	Piece               *PieceSummary
	Link                PerformanceLink
	Year                *int16
	Performers          []PersonSummary
	DescriptionVerbatim string
}

// LetterCount is how many entities of a list sort under a letter of the A–Z index.
type LetterCount struct {
	Letter Letter
	Count  int64
}

// PieceList is one page of the public pieces matching a search, with how many match in all and the facet counts.
type PieceList struct {
	Pieces []PieceSummary
	Total  int64
	Facets PieceFacets
}

// PieceFacets are the facet counts of a piece search. Each is disjunctive: a value's count is how many public
// pieces match every filter of the search except the selection in the value's own facet, and match the value.
// Only values with a count above zero are listed.
type PieceFacets struct {
	// Terms are the public piece terms, counting pieces tagged with the term or any of its descendants once, by
	// slug.
	Terms []TermCount
	// Kinds count pieces with the kind as their primary or a secondary kind, in PieceKind's declaration order.
	Kinds []PieceKindCount
	// ConservationStatuses are in ConservationStatus's declaration order.
	ConservationStatuses []ConservationStatusCount
	// Letters are the letters the pieces' sort titles sort under, "0" first.
	Letters []LetterCount
}

// TermCount is how many pieces of a search a term classifies.
type TermCount struct {
	Slug  string
	Count int64
}

// PieceKindCount is how many pieces of a search are of a kind.
type PieceKindCount struct {
	Kind  PieceKind
	Count int64
}

// ConservationStatusCount is how many pieces of a search have a conservation status.
type ConservationStatusCount struct {
	ConservationStatus ConservationStatus
	Count              int64
}

// PersonList is one page of the public persons matching a search, with how many match in all and how many of
// those sort under each letter, ignoring the search's own letter filter. Persons who prefer no page are listed
// as stubs.
type PersonList struct {
	Persons []PersonSummary
	Total   int64
	Letters []LetterCount
}

// CollectionList is one page of the public collections matching a filter, by sort name, with how many match in
// all and how many of those sort under each letter, ignoring the filter's own letter.
type CollectionList struct {
	Collections []CollectionSummary
	Total       int64
	Letters     []LetterCount
}
