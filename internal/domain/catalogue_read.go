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
	NotesMD            *string
	VersionNotesMD     *string
	LicensingStatus    LicensingStatus
	LicensingNotesMD   *string
	Licence            *Licence
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
	Status    ConservationStatus
	Reason    string
	SourceURL *string
	At        time.Time
}

// Person is a public person. Profile is nil exactly for a person who prefers not to have a page, of whom only
// the identity is shown.
type Person struct {
	PersonSummary
	Status          PersonStatus
	StatusDate      *time.Time
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
	URL      string
	Title    *string
	Platform *string
}
