//go:build integration

package app

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) { catalogtest.Main(m) }

// leakFixture is what the leak matrix's requests address: public entities that refer to everything a public
// read must hide, and the hidden entities themselves.
type leakFixture struct {
	piece, group, person catalogtest.Ref
	// noPage prefers not to have a page: their own page shows their identity, which is marked, and nothing else.
	noPage catalogtest.Ref
	// noPageAdmirer prefers not to have a page either; person lists show their identity as a stub.
	noPageAdmirer catalogtest.Ref
	// noPageBusker prefers no page and has a link with a performance by the public person.
	noPageBusker                                      catalogtest.Ref
	unapprovedPiece, unapprovedPerson, unapprovedEcho catalogtest.Ref
	// lanterns is a public topic with an unapproved child and related affiliation, tagging unapproved pieces;
	// westmarch is a public affiliation of the no-page person and an unapproved person.
	lanterns, westmarch                              catalogtest.Ref
	unapprovedTerm, underUnapproved, unapprovedChild catalogtest.Ref
	unapprovedAffiliation                            catalogtest.Ref
	// archive is a public collection citing links of every hidden kind, under an unapproved collection.
	archive, unapprovedCollection catalogtest.Ref
	// moot is a public event at which every hidden kind of performance was recorded.
	moot, unapprovedEvent catalogtest.Ref
	// renamed and renamedNoPage were renamed since their first revision; their former names are hidden aliases.
	renamed, renamedNoPage catalogtest.Ref
	// ember is a public piece whose first revision holds links that are no visible link's now.
	ember catalogtest.Ref
	// turned chose not to have a page after their revisions were written.
	turned    catalogtest.Ref
	revisions leakRevisions
}

// leakRevisions are revisions beyond catalogtest's: each shows something hidden now, or is not public.
type leakRevisions struct {
	// rowanAliasPublic shows Rowan's hidden alias as public, and a relation to turned.
	rowanAliasPublic int64
	// lanternTurned credits turned.
	lanternTurned int64
	// turnedEdit is of turned, with their profile.
	turnedEdit int64
	// pendingLantern is pending, and unapprovedPieceApproved is of an unapproved piece.
	pendingLantern, unapprovedPieceApproved int64
	// renamedFormer and renamedNoPageFormer show the renamed persons' former names; renamedEdit and
	// renamedNoPageEdit are based on them.
	renamedFormer, renamedEdit, renamedNoPageFormer, renamedNoPageEdit int64
	// rejectedLantern is a rejected revision of the Lantern Song, and rebasedLantern a pending one that a
	// moderator rebased, whose original submission alone is marked.
	rejectedLantern, rebasedLantern int64
}

// turnedName is the name of a person who chose not to have a page after their revisions were written.
const turnedName = "Leakmark Turned Nopage"

// leakAuthorID is the user ID of the author of revisions: no response may show it.
const leakAuthorID = "google-oauth2|leakmark-author"

// leakModeratorID is the user ID of the moderator who rejected and rebased revisions: no response may show it.
const leakModeratorID = "google-oauth2|leakmark-moderator"

// Marked text that only moderators may read: a rejected revision's notes, the original submission of a pending
// revision a moderator rebased, and a link report's message, which the revision leak matrix reuses.
const (
	rejectedNotes   = "leakmark-rejected-notes"
	rebasedOriginal = "leakmark-rebased-original"
	reportMessage   = "leakmark-report-message"
)

// An open contact request's personal data, which only interactive moderators may read, and which is never logged.
const (
	contactName    = "Leakmark Contact Name"
	contactEmail   = "leakmark-contact@example.invalid"
	contactMessage = "leakmark-contact-message"
)

// namedCredit is a certain, named credit of personID, with its defaults written out.
func namedCredit(personID int64, role domain.CreditRole, note *string) domain.DocCredit {
	return domain.DocCredit{PersonID: &personID, Role: role, Attribution: domain.AttributionNamed,
		Certainty: domain.CertaintyCertain, Note: note}
}

// noPageName is the no-page person's name, which only their own page shows.
const noPageName = "Leakmark Nopage"

// noPagePublicAlias is a public alias of the no-page person. A person who prefers no page is found only by name,
// so no contributor's duplicate check compares it.
const noPagePublicAlias = "Leakmark Nopage Public Alias"

// noPageAdmirerName is the name of another no-page person, which only person lists and their own page show.
const noPageAdmirerName = "Leakmark Nopage Admirer"

// noPageBuskerName is the name of a third no-page person, who is listed as a stub like the others.
const noPageBuskerName = "Leakmark Nopage Busker"

const hiddenLinkURL = "https://example.org/leakmark-hidden-link"

// The URLs of links a revision holds that are no visible link's now: one deleted, one given another URL and
// hidden, one hidden under the lower-cased URL, and one respelled (to respelledLinkURLNow, with the same
// normalized URL) and hidden. Each is in normalized form but respelledLinkURL.
const (
	deletedLinkURL      = "https://example.org/leakmark-deleted-link"
	movedLinkURL        = "https://example.org/leakmark-moved-link"
	recasedLinkURL      = "https://example.org/Leakmark-Recased-Link"
	respelledLinkURL    = "https://Example.org/leakmark-respelled-link?utm_source=leakmark"
	respelledLinkURLNow = "https://EXAMPLE.org:443/leakmark-respelled-link?fbclid=1"
)

// enteredLinkURL is a visible link's URL as entered, which ember shows; its normalized URL,
// normalizedLinkURL, is a system field no read exposes.
const (
	enteredLinkURL    = "https://example.org/leakmark-normalized?utm_source=x"
	normalizedLinkURL = "https://example.org/leakmark-normalized"
)

// The former names of renamed persons, now hidden legal aliases of theirs.
const (
	renamedFormerName       = "Leakmark Renamed Legal Name"
	renamedNoPageFormerName = "Leakmark Renamed Nopage Legal Name"
)

// leakCatalogue holds, each marked:
//   - a hidden legal alias of a public person, and of the no-page person;
//   - a prefers_no_page person with notes, pronouns, licensing notes, a public alias, a role, an affiliation,
//     relations in both directions, a credit, a performance as performer and a link with a performance;
//   - a hidden link attached to a piece and a person, each with a performance;
//   - an unapproved piece, person, term (and an approved term under it), affiliation, collection and event, each
//     referred to by public entities, and unapproved entities referring to public ones: an unapproved child and
//     related affiliation of a public term, unapproved pieces tagged with it, an unapproved parent, child and
//     region of a public collection, and unapproved and no-page persons affiliated with a public affiliation;
//   - in a public collection, and at a public event, links and performances of the hidden link, the no-page
//     person and unapproved pieces and persons;
//   - performances a public person performs in, on links attached only to a no-page or an unapproved person;
//   - persons renamed since their first revision, with and without a page, whose former name is a hidden alias;
//   - a public piece whose first revision holds links since deleted, given another URL and hidden, and hidden
//     under a URL in another case;
//   - system fields that are never exposed: legacy titles, a claim by a user and normalized URLs.
var leakCatalogue = catalogtest.NewShared(func(c *catalogtest.Catalog) leakFixture {
	unapprovedAffiliation := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetAffiliation,
		Name: "Leakmark Unapproved Affiliation"}, Unapproved: true})
	lanterns := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic, Name: "Lanterns",
		RelatedAffiliationID: &unapprovedAffiliation.ID}})
	unapprovedChild := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic,
		Name: "Leakmark Unapproved Child", ParentID: &lanterns.ID}, Unapproved: true})
	unapprovedTerm := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic,
		Name: "Leakmark Unapproved Term"}, Unapproved: true})
	underUnapproved := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic,
		Name: "Leakmark Under Unapproved", ParentID: &unapprovedTerm.ID}})
	westmarch := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetAffiliation, Name: "Westmarch"}})
	unapprovedCollection := c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{
		Name: "Leakmark Unapproved Collection"}, Unapproved: true})
	// A public collection under an unapproved one, in an unapproved region, citing everything below.
	archive := c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: "Lantern Archive",
		ParentID: &unapprovedCollection.ID, RegionTermID: &unapprovedAffiliation.ID}})
	c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: "Lantern Annex", ParentID: &archive.ID}})
	c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: "Leakmark Unapproved Annex",
		ParentID: &archive.ID}, Unapproved: true})
	unapprovedEvent := c.Event(catalogtest.Event{Doc: domain.EventDocument{Name: "Leakmark Unapproved Event"},
		Unapproved: true})
	// A public event every performance below is at.
	moot := c.Event(catalogtest.Event{Doc: domain.EventDocument{Name: "Lantern Moot"}})

	group := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Lantern Band", Kind: domain.PersonKindGroup}})
	unapprovedPerson := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Leakmark Unapproved Person",
		AffiliationIDs: []int64{westmarch.ID},
		Relations:      []domain.DocPersonRelation{{ToPersonID: group.ID, Type: domain.PersonRelationTypeMemberOf}},
		Links: []domain.DocPersonLink{{
			DocLink: domain.DocLink{URL: "https://example.org/leakmark-unapproved-person-link",
				CollectionID: &archive.ID, Roles: []domain.LinkRole{domain.LinkRoleWebsite}},
			Performances: []domain.DocPersonPerformance{{DocPerformance: domain.DocPerformance{EventID: &moot.ID,
				DescriptionVerbatim: "Leakmark unapproved person performance"}}},
		}}},
		Unapproved: true})
	unapprovedPiece := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Leakmark Unapproved Piece",
		Terms: []domain.DocPieceTerm{{TermID: lanterns.ID}},
		Links: []domain.DocPieceLink{{
			DocLink: domain.DocLink{URL: "https://example.org/leakmark-unapproved-piece-link",
				CollectionID: &archive.ID, Roles: []domain.LinkRole{domain.LinkRoleLyrics}},
			Performances: []domain.DocPerformance{{EventID: &moot.ID,
				DescriptionVerbatim: "Leakmark unapproved piece moot performance"}},
		}}},
		Unapproved: true})
	noPage := c.Person(catalogtest.Person{Doc: domain.PersonDocument{
		Name:             noPageName,
		Status:           domain.PersonStatusPrefersNoPage,
		StatusSourceURL:  new("https://example.org/requests/9"),
		Pronouns:         new("leakmark/pronoun"),
		NotesMD:          new("Leakmark nopage notes"),
		LicensingStatus:  domain.LicensingStatusKnown,
		LicensingNotesMD: new("Leakmark nopage licensing notes"),
		Aliases: []domain.DocPersonAlias{
			{Name: noPagePublicAlias, Kind: domain.PersonAliasKindStage, IsPublic: true},
			{Name: "Leakmark Nopage Legal Alias", Kind: domain.PersonAliasKindLegal},
		},
		Roles:          []domain.PersonRole{domain.PersonRoleJester},
		AffiliationIDs: []int64{westmarch.ID},
		Relations:      []domain.DocPersonRelation{{ToPersonID: group.ID, Type: domain.PersonRelationTypeMemberOf}},
		Links: []domain.DocPersonLink{{
			DocLink: domain.DocLink{URL: "https://example.org/leakmark-nopage-link", Title: new("Leakmark Nopage Link"),
				CollectionID: &archive.ID, Roles: []domain.LinkRole{domain.LinkRoleWebsite}},
			Performances: []domain.DocPersonPerformance{{DocPerformance: domain.DocPerformance{EventID: &moot.ID,
				DescriptionVerbatim: "Leakmark nopage performance"}}},
		}},
	}})
	// Rowan's and the Lantern Song's documents are written out in full, defaults included, as later revisions
	// store variants of them as given.
	rowanDoc := domain.PersonDocument{
		SchemaVersion:   domain.DocumentSchemaVersion,
		Name:            "Rowan Ashby",
		Kind:            domain.PersonKindCommunityBard,
		Status:          domain.PersonStatusActive,
		LicensingStatus: domain.LicensingStatusUnknown,
		Aliases: []domain.DocPersonAlias{
			{Name: "Rowan", Kind: domain.PersonAliasKindStage, IsPublic: true},
			{Name: "Leakmark Hidden Legal Alias", Kind: domain.PersonAliasKindLegal},
		},
		Roles:          []domain.PersonRole{domain.PersonRoleSinger},
		AffiliationIDs: []int64{westmarch.ID, unapprovedAffiliation.ID},
		Relations: []domain.DocPersonRelation{
			{ToPersonID: group.ID, Type: domain.PersonRelationTypeMemberOf},
			{ToPersonID: noPage.ID, Type: domain.PersonRelationTypeHeirOf},
			{ToPersonID: unapprovedPerson.ID, Type: domain.PersonRelationTypeSameAs},
		},
		Links: []domain.DocPersonLink{
			{
				DocLink: domain.DocLink{URL: hiddenLinkURL, Title: new("Leakmark Hidden Link"),
					CollectionID: &archive.ID, Roles: []domain.LinkRole{domain.LinkRoleOther}},
				Performances: []domain.DocPersonPerformance{{DocPerformance: domain.DocPerformance{EventID: &moot.ID,
					DescriptionVerbatim: "Leakmark hidden link performance"}}},
			},
			{
				DocLink: domain.DocLink{URL: "https://example.org/rowan", CollectionID: &unapprovedCollection.ID,
					Roles: []domain.LinkRole{domain.LinkRoleWebsite}},
				Performances: []domain.DocPersonPerformance{
					{PieceID: &unapprovedPiece.ID, DocPerformance: domain.DocPerformance{EventID: &moot.ID,
						DescriptionVerbatim: "Leakmark unapproved piece performance"}},
					{DocPerformance: domain.DocPerformance{EventID: &unapprovedEvent.ID,
						DescriptionVerbatim: "Rowan at a moot"}},
				},
			},
		},
	}
	person := c.Person(catalogtest.Person{Doc: rowanDoc})
	lanternDoc := domain.PieceDocument{
		SchemaVersion:   domain.DocumentSchemaVersion,
		Title:           "The Lantern Song",
		Kind:            domain.PieceKindSong,
		Conservation:    domain.DocConservation{Status: domain.ConservationStatusUnassessed},
		LicensingStatus: domain.LicensingStatusUnknown,
		Terms: []domain.DocPieceTerm{
			{TermID: lanterns.ID, IsPrimary: true}, {TermID: unapprovedTerm.ID}, {TermID: underUnapproved.ID},
		},
		Relations: []domain.DocPieceRelation{{ToPieceID: unapprovedPiece.ID, Type: domain.PieceRelationTypeVariantOf}},
		Credits: []domain.DocCredit{
			namedCredit(person.ID, domain.CreditRoleLyrics, nil),
			namedCredit(noPage.ID, domain.CreditRoleTune, new("Leakmark nopage credit note")),
			namedCredit(unapprovedPerson.ID, domain.CreditRoleMusic, new("Leakmark unapproved credit note")),
		},
		Links: []domain.DocPieceLink{
			{
				DocLink: domain.DocLink{URL: "https://example.org/lantern", CollectionID: &unapprovedCollection.ID,
					Roles: []domain.LinkRole{domain.LinkRolePerformance}},
				Performances: []domain.DocPerformance{
					{EventID: &unapprovedEvent.ID, DescriptionVerbatim: "Sung by Rowan",
						PerformerIDs: []int64{person.ID, noPage.ID, unapprovedPerson.ID}},
					{EventID: &moot.ID, DescriptionVerbatim: "Sung at the moot",
						PerformerIDs: []int64{person.ID, noPage.ID, unapprovedPerson.ID}},
				},
			},
			{
				DocLink: domain.DocLink{URL: hiddenLinkURL, Roles: []domain.LinkRole{domain.LinkRoleLyrics}},
				Performances: []domain.DocPerformance{{EventID: &moot.ID,
					DescriptionVerbatim: "Leakmark hidden piece performance", PerformerIDs: []int64{person.ID}}},
			},
			{DocLink: domain.DocLink{URL: "https://example.org/lantern-archive", CollectionID: &archive.ID,
				Locator: new("shelf 3"), Roles: []domain.LinkRole{domain.LinkRoleText}}},
		},
	}
	piece := c.Piece(catalogtest.Piece{Doc: lanternDoc})
	// Hidden after the documents above were written, and dead, so that needs-work would count it if it counted
	// hidden links.
	c.SetLinkState(hiddenLinkURL, catalogtest.LinkState{Liveness: domain.LivenessDead, IsHidden: true})

	// Unapproved and no-page entities referring to the public ones, seeded after them.
	unapprovedEcho := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Leakmark Unapproved Echo",
		Terms:     []domain.DocPieceTerm{{TermID: lanterns.ID}},
		Relations: []domain.DocPieceRelation{{ToPieceID: piece.ID, Type: domain.PieceRelationTypeContrafactumOf}},
		Credits:   []domain.DocCredit{{PersonID: &person.ID, Role: domain.CreditRoleLyrics}}}, Unapproved: true})
	c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Leakmark Unapproved Admirer",
		Relations: []domain.DocPersonRelation{{ToPersonID: person.ID, Type: domain.PersonRelationTypeHeirOf}}},
		Unapproved: true})
	noPageAdmirer := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: noPageAdmirerName,
		Status:    domain.PersonStatusPrefersNoPage,
		Relations: []domain.DocPersonRelation{{ToPersonID: person.ID, Type: domain.PersonRelationTypeHeirOf}}}})

	// Performances Rowan performs in, on links attached only to a no-page person and to an unapproved person:
	// Rowan's page and the moot's must not show them, as the links' URLs, titles and descriptions are theirs.
	noPageBusker := c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: noPageBuskerName,
		Status: domain.PersonStatusPrefersNoPage,
		Links: []domain.DocPersonLink{{
			DocLink: domain.DocLink{URL: "https://example.org/leakmark-nopage-busker-link",
				Title: new("Leakmark Nopage Busker Link"), Roles: []domain.LinkRole{domain.LinkRolePerformance}},
			Performances: []domain.DocPersonPerformance{{DocPerformance: domain.DocPerformance{EventID: &moot.ID,
				DescriptionVerbatim: "Leakmark nopage busker performance", PerformerIDs: []int64{person.ID}}}},
		}}}})
	c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Leakmark Unapproved Busker",
		Links: []domain.DocPersonLink{{
			DocLink: domain.DocLink{URL: "https://example.org/leakmark-unapproved-busker-link",
				Title: new("Leakmark Unapproved Busker Link"), Roles: []domain.LinkRole{domain.LinkRolePerformance}},
			Performances: []domain.DocPersonPerformance{{DocPerformance: domain.DocPerformance{
				DescriptionVerbatim: "Leakmark unapproved busker performance", PerformerIDs: []int64{person.ID}}}},
		}}},
		Unapproved: true})

	// Persons renamed since their first revision, whose former name is now a hidden legal alias: one with a page
	// and one without, whose identity their revisions show.
	renamedDoc := domain.PersonDocument{
		SchemaVersion:   domain.DocumentSchemaVersion,
		Name:            "Ashen Wren",
		Kind:            domain.PersonKindCommunityBard,
		Status:          domain.PersonStatusActive,
		LicensingStatus: domain.LicensingStatusUnknown,
		Aliases:         []domain.DocPersonAlias{{Name: renamedFormerName, Kind: domain.PersonAliasKindLegal}},
	}
	renamed := c.Person(catalogtest.Person{Doc: renamedDoc})
	renamedNoPageDoc := renamedDoc
	renamedNoPageDoc.Name = "Quiet Fen"
	renamedNoPageDoc.Status = domain.PersonStatusPrefersNoPage
	renamedNoPageDoc.Aliases = []domain.DocPersonAlias{{Name: renamedNoPageFormerName,
		Kind: domain.PersonAliasKindLegal}}
	renamedNoPage := c.Person(catalogtest.Person{Doc: renamedNoPageDoc})
	formerly := func(ref catalogtest.Ref, doc domain.PersonDocument, formerName string) (int64, int64) {
		former := doc
		former.Name, former.Aliases = formerName, nil
		formerID := c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePerson, EntityID: ref.ID,
			Snapshot: former})
		editID := c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePerson, EntityID: ref.ID,
			Snapshot: doc, BaseRevisionID: formerID, Source: domain.RevisionSourceUser, AuthorUserID: leakAuthorID})
		return formerID, editID
	}

	// A public piece whose first revision's links have since been deleted, given another URL and hidden, and
	// hidden under a URL that differs from the revision's in case only, and respelled and hidden: none may show
	// in that revision. It also has a visible link entered with tracking parameters, whose normalized URL no
	// read may show.
	ember := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{
		SchemaVersion:   domain.DocumentSchemaVersion,
		Title:           "The Ember Song",
		Kind:            domain.PieceKindSong,
		Conservation:    domain.DocConservation{Status: domain.ConservationStatusUnassessed},
		LicensingStatus: domain.LicensingStatusUnknown,
		Links: []domain.DocPieceLink{
			{DocLink: domain.DocLink{URL: deletedLinkURL, Title: new("Leakmark Deleted Link"),
				Roles: []domain.LinkRole{domain.LinkRoleLyrics}}},
			{DocLink: domain.DocLink{URL: movedLinkURL, Title: new("Leakmark Moved Link"),
				Roles: []domain.LinkRole{domain.LinkRoleLyrics}}},
			{DocLink: domain.DocLink{URL: recasedLinkURL, Title: new("Leakmark Recased Link"),
				Roles: []domain.LinkRole{domain.LinkRoleLyrics}}},
			{DocLink: domain.DocLink{URL: respelledLinkURL, Title: new("Leakmark Respelled Link"),
				Roles: []domain.LinkRole{domain.LinkRoleLyrics}}},
			{DocLink: domain.DocLink{URL: "https://example.org/ember", Roles: []domain.LinkRole{domain.LinkRoleText}}},
			{DocLink: domain.DocLink{URL: enteredLinkURL, Roles: []domain.LinkRole{domain.LinkRoleText}}},
		},
	}})
	// Each link edit keeps normalized_url = NormalizeURL(url), as every writer must; these URLs are in normalized
	// form.
	c.Exec("DELETE FROM links WHERE url = ?", deletedLinkURL)
	c.Exec("UPDATE links SET url = ?, normalized_url = ?, is_hidden = TRUE WHERE url = ?", movedLinkURL+"-moved",
		movedLinkURL+"-moved", movedLinkURL)
	c.Exec("UPDATE links SET url = LOWER(url), normalized_url = LOWER(url), is_hidden = TRUE WHERE url = ?",
		recasedLinkURL)
	c.Exec("UPDATE links SET url = ?, is_hidden = TRUE WHERE url = ?", respelledLinkURLNow, respelledLinkURL)

	// Revisions whose snapshots show what is hidden now, but was not when they were written: the hidden alias as
	// public, credits to a person who has since chosen not to have a page, and the hidden link (in every
	// document above). Plus revisions that are not public: a pending one, and an approved one of an unapproved
	// piece.
	c.Exec("INSERT INTO users (id, display_name, role, created_at) VALUES (?, ?, 'contributor', ?)",
		leakAuthorID, "Morwen", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	turnedDoc := domain.PersonDocument{
		SchemaVersion:   domain.DocumentSchemaVersion,
		Name:            turnedName,
		Kind:            domain.PersonKindCommunityBard,
		Status:          domain.PersonStatusActive,
		NotesMD:         new("Leakmark turned notes"),
		LicensingStatus: domain.LicensingStatusUnknown,
		Aliases: []domain.DocPersonAlias{{Name: "Leakmark Turned Alias", Kind: domain.PersonAliasKindStage,
			IsPublic: true}},
		Relations: []domain.DocPersonRelation{{ToPersonID: person.ID, Type: domain.PersonRelationTypeHeirOf}},
		Links: []domain.DocPersonLink{{DocLink: domain.DocLink{URL: "https://example.org/leakmark-turned-link",
			Roles: []domain.LinkRole{domain.LinkRoleWebsite}}}},
	}
	turned := c.Person(catalogtest.Person{Doc: turnedDoc})
	turnedEdit := c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePerson, EntityID: turned.ID,
		Snapshot: turnedDoc, Source: domain.RevisionSourceUser, AuthorUserID: leakAuthorID,
		BaseRevisionID: turned.RevisionID})
	c.Exec("UPDATE persons SET status = 'prefers_no_page' WHERE id = ?", turned.ID)

	rowanAliasPublic := rowanDoc
	rowanAliasPublic.Aliases = []domain.DocPersonAlias{rowanDoc.Aliases[0], rowanDoc.Aliases[1]}
	rowanAliasPublic.Aliases[1].IsPublic = true
	rowanAliasPublic.Relations = append(append([]domain.DocPersonRelation{}, rowanDoc.Relations...),
		domain.DocPersonRelation{ToPersonID: turned.ID, Type: domain.PersonRelationTypeSameAs})
	lanternTurned := lanternDoc
	lanternTurned.Credits = append(append([]domain.DocCredit{}, lanternDoc.Credits...),
		namedCredit(turned.ID, domain.CreditRoleTranslation, new("Leakmark turned credit note")))
	pendingLantern := lanternDoc
	pendingLantern.Title = "Leakmark Pending Title"
	revisions := leakRevisions{
		rowanAliasPublic: c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePerson, EntityID: person.ID,
			Snapshot: rowanAliasPublic, BaseRevisionID: person.RevisionID, Source: domain.RevisionSourceUser,
			AuthorUserID: leakAuthorID}),
		lanternTurned: c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece, EntityID: piece.ID,
			Snapshot: lanternTurned, BaseRevisionID: piece.RevisionID, Source: domain.RevisionSourceUser,
			AuthorUserID: leakAuthorID, Contributors: []string{"Archivist"}}),
		pendingLantern: c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece, EntityID: piece.ID,
			Snapshot: pendingLantern, Status: domain.RevisionStatusPending, AuthorUserID: leakAuthorID}),
		unapprovedPieceApproved: c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece,
			EntityID: unapprovedPiece.ID, Snapshot: lanternDoc}),
		turnedEdit: turnedEdit,
	}
	revisions.renamedFormer, revisions.renamedEdit = formerly(renamed, renamedDoc, renamedFormerName)
	revisions.renamedNoPageFormer, revisions.renamedNoPageEdit = formerly(renamedNoPage, renamedNoPageDoc,
		renamedNoPageFormerName)
	revisions.rejectedLantern, revisions.rebasedLantern = seedModeratedLantern(c, piece, lanternDoc)

	// System fields no document holds and no read exposes.
	c.Exec("INSERT INTO users (id, display_name, role, created_at) VALUES (?, ?, 'contributor', ?)",
		"google-oauth2|leakmark-claimer", "Leakmark Claimer", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	c.Exec("UPDATE persons SET claimed_by_user_id = ?, legacy_title = ?, legacy_wiki_pageid = id",
		"google-oauth2|leakmark-claimer", "Leakmark Legacy Person")
	c.Exec("UPDATE pieces SET legacy_title = ?, legacy_wiki_pageid = id", "Leakmark Legacy Piece")
	c.Exec("UPDATE terms SET legacy_title = ?", "Leakmark Legacy Term")
	c.Exec("UPDATE events SET legacy_title = ?", "Leakmark Legacy Event")

	return leakFixture{
		piece: piece, group: group, person: person, noPage: noPage, noPageAdmirer: noPageAdmirer, noPageBusker: noPageBusker,
		unapprovedPiece: unapprovedPiece, unapprovedPerson: unapprovedPerson, unapprovedEcho: unapprovedEcho,
		lanterns: lanterns, westmarch: westmarch, unapprovedTerm: unapprovedTerm, underUnapproved: underUnapproved,
		unapprovedChild: unapprovedChild, unapprovedAffiliation: unapprovedAffiliation,
		archive: archive, unapprovedCollection: unapprovedCollection, moot: moot, unapprovedEvent: unapprovedEvent,
		turned: turned, renamed: renamed, renamedNoPage: renamedNoPage, ember: ember, revisions: revisions,
	}
})

// seedModeratedLantern seeds what a moderator did with A's revisions of piece, whose current document is doc: a
// rejected revision with marked notes; a pending revision the moderator rebased, whose original submission holds
// marked notes that the rebased document does not; and an open report, with a marked message, of one of piece's
// visible links. It returns the IDs of the rejected and the rebased revisions.
func seedModeratedLantern(c *catalogtest.Catalog, piece catalogtest.Ref, doc domain.PieceDocument) (int64, int64) {
	reviewedAt := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	c.Exec("INSERT INTO users (id, display_name, role, created_at) VALUES (?, ?, 'moderator', ?)",
		leakModeratorID, "Wren Moderator", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	rejected := doc
	rejected.NotesMD = new(rejectedNotes)
	rejectedID := c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece, EntityID: piece.ID,
		Snapshot: rejected, BaseRevisionID: piece.RevisionID, Status: domain.RevisionStatusRejected,
		Source: domain.RevisionSourceUser, AuthorUserID: leakAuthorID})
	c.Exec("UPDATE revisions SET reviewer_user_id = ?, reviewed_at = ? WHERE id = ?", leakModeratorID, reviewedAt,
		rejectedID)

	// Written as submitted, then rebased: the submission is kept beside the rebased document, as a rebase does.
	submitted := doc
	submitted.NotesMD = new(rebasedOriginal)
	rebasedID := c.Revision(catalogtest.Revision{EntityType: domain.EntityTypePiece, EntityID: piece.ID,
		Snapshot: submitted, BaseRevisionID: piece.RevisionID, Status: domain.RevisionStatusPending,
		Source: domain.RevisionSourceUser, AuthorUserID: leakAuthorID})
	c.Exec("UPDATE revisions SET submitted_snapshot_json = snapshot_json, "+
		"snapshot_json = JSON_SET(snapshot_json, '$.notes_md', 'Notes as the moderator rebased them.'), "+
		"submitted_base_revision_id = base_revision_id, flags_json = JSON_ARRAY(), submitted_at = created_at, "+
		"rebased_by_user_id = ?, rebased_at = ? WHERE id = ?", leakModeratorID, reviewedAt, rebasedID)

	c.Exec("INSERT INTO link_reports (link_id, reporter_user_id, reason, message, created_at) "+
		"SELECT id, ?, 'other', ?, ? FROM links WHERE url = ?", leakAuthorID, reportMessage, reviewedAt,
		"https://example.org/lantern")
	c.Exec("INSERT INTO contact_requests (kind, name, email, message, url, created_at) "+
		"VALUES ('takedown', ?, ?, ?, ?, ?)", contactName, contactEmail, contactMessage,
		"https://example.org/leakmark-contact-url", reviewedAt)

	return rejectedID, rebasedID
}

// leakRequest is a request the leak matrix sends, and its expected status.
type leakRequest struct {
	path   string
	status int
	// reveals are marked strings the response may show, such as the no-page person's identity on their own page.
	reveals []string
	// absent are unmarked strings the response must not show either.
	absent []string
}

// leakMatrix has, for each public catalogue read in the spec, the requests to send it against the leak fixture:
// at least the public entities that refer to hidden ones, and the hidden entities themselves. TestHTTP_LeakMatrix
// fails for a public /v1 GET without a row, so each new catalogue endpoint adds one here, with requests that
// reach every list it shows.
var leakMatrix = map[openapitest.Operation]func(fx leakFixture) []leakRequest{
	{Method: http.MethodGet, Path: "/v1/pieces/{slug}"}: func(fx leakFixture) []leakRequest {
		return []leakRequest{
			{path: "/v1/pieces/" + fx.piece.Slug, status: http.StatusOK},
			// The links of its first revision that are no visible link's now, and a link entered with tracking
			// parameters, shown as entered.
			{path: "/v1/pieces/" + fx.ember.Slug, status: http.StatusOK, reveals: []string{enteredLinkURL}},
			{path: "/v1/pieces/" + fx.unapprovedPiece.Slug, status: http.StatusNotFound},
			{path: "/v1/pieces/" + fx.unapprovedEcho.Slug, status: http.StatusNotFound},
		}
	},
	{Method: http.MethodGet, Path: "/v1/persons/{slug}"}: func(fx leakFixture) []leakRequest {
		return []leakRequest{
			// Performances Rowan performs in on links attached only to no-page or unapproved persons.
			{path: "/v1/persons/" + fx.person.Slug, status: http.StatusOK},
			// Former names that are hidden aliases now.
			{path: "/v1/persons/" + fx.renamed.Slug, status: http.StatusOK},
			{path: "/v1/persons/" + fx.renamedNoPage.Slug, status: http.StatusOK},
			{path: "/v1/persons/" + fx.group.Slug, status: http.StatusOK},
			{path: "/v1/persons/" + fx.noPage.Slug, status: http.StatusOK, reveals: []string{fx.noPage.Slug, noPageName}},
			{path: "/v1/persons/" + fx.turned.Slug, status: http.StatusOK, reveals: []string{fx.turned.Slug, turnedName}},
			{path: "/v1/persons/" + fx.unapprovedPerson.Slug, status: http.StatusNotFound},
		}
	},
	{Method: http.MethodGet, Path: piecesPath}: func(fx leakFixture) []leakRequest {
		// An unknown term's 400 names the slug the request gave.
		unknownTerm := func(term catalogtest.Ref) leakRequest {
			return leakRequest{path: piecesPath + "?term=" + term.Slug, status: http.StatusBadRequest,
				reveals: []string{"unknown term: " + term.Slug}}
		}
		return []leakRequest{
			// Facet counts must not list unapproved terms, or terms under them, nor count unapproved pieces.
			{path: piecesPath, status: http.StatusOK},
			{path: piecesPath + "?term=" + fx.lanterns.Slug, status: http.StatusOK},
			{path: piecesPath + "?q=leakmark", status: http.StatusOK},
			{path: piecesPath + "?q=Le", status: http.StatusOK},
			unknownTerm(fx.unapprovedTerm),
			unknownTerm(fx.underUnapproved),
			unknownTerm(fx.unapprovedChild),
			unknownTerm(fx.westmarch),
		}
	},
	{Method: http.MethodGet, Path: personsPath}: func(fx leakFixture) []leakRequest {
		// Persons who prefer no page are listed as stubs, by name.
		stubs := []string{fx.noPage.Slug, noPageName, fx.noPageAdmirer.Slug, noPageAdmirerName,
			fx.noPageBusker.Slug, noPageBuskerName, fx.turned.Slug, turnedName}
		return []leakRequest{
			{path: personsPath, status: http.StatusOK, reveals: stubs},
			{path: personsPath + "?q=leakmark", status: http.StatusOK, reveals: stubs},
			{path: personsPath + "?q=Le", status: http.StatusOK, reveals: stubs},
			// The no-page person's role and affiliation, and the unapproved person's, must not match.
			{path: personsPath + "?role=jester", status: http.StatusOK},
			{path: personsPath + "?affiliation=" + fx.westmarch.Slug, status: http.StatusOK},
			{path: personsPath + "?affiliation=" + fx.unapprovedAffiliation.Slug, status: http.StatusBadRequest,
				reveals: []string{"unknown affiliation: " + fx.unapprovedAffiliation.Slug}},
		}
	},
	{Method: http.MethodGet, Path: collectionsPath}: func(leakFixture) []leakRequest {
		return []leakRequest{
			{path: collectionsPath, status: http.StatusOK},
			{path: collectionsPath + "?kind=other&letter=L", status: http.StatusOK},
		}
	},
	{Method: http.MethodGet, Path: termsPath}: func(leakFixture) []leakRequest {
		return []leakRequest{
			{path: termsPath, status: http.StatusOK},
			{path: "/v1/terms?facet=topic", status: http.StatusOK},
			{path: "/v1/terms?facet=affiliation", status: http.StatusOK},
		}
	},
	{Method: http.MethodGet, Path: "/v1/terms/{slug}"}: func(fx leakFixture) []leakRequest {
		return []leakRequest{
			// Pieces, without the unapproved ones tagged with it; children and related affiliation, unapproved.
			{path: "/v1/terms/" + fx.lanterns.Slug, status: http.StatusOK},
			// Persons, without the no-page and unapproved ones affiliated with it.
			{path: "/v1/terms/" + fx.westmarch.Slug, status: http.StatusOK},
			{path: "/v1/terms/" + fx.unapprovedTerm.Slug, status: http.StatusNotFound},
			{path: "/v1/terms/" + fx.underUnapproved.Slug, status: http.StatusNotFound},
			{path: "/v1/terms/" + fx.unapprovedChild.Slug, status: http.StatusNotFound},
			{path: "/v1/terms/" + fx.unapprovedAffiliation.Slug, status: http.StatusNotFound},
		}
	},
	{Method: http.MethodGet, Path: "/v1/collections/{collection_id}"}: func(fx leakFixture) []leakRequest {
		return []leakRequest{
			// Citations of a public piece only; the parent, region and a child are unapproved.
			{path: "/v1/collections/" + strconv.FormatInt(fx.archive.ID, 10), status: http.StatusOK},
			{path: "/v1/collections/" + strconv.FormatInt(fx.unapprovedCollection.ID, 10), status: http.StatusNotFound},
		}
	},
	{Method: http.MethodGet, Path: eventsPath}: func(leakFixture) []leakRequest {
		return []leakRequest{
			{path: eventsPath, status: http.StatusOK},
			{path: "/v1/events?kind=event", status: http.StatusOK},
		}
	},
	{Method: http.MethodGet, Path: "/v1/events/{slug}"}: func(fx leakFixture) []leakRequest {
		return []leakRequest{
			// Performances of public pieces on visible links, with public performers only.
			{path: "/v1/events/" + fx.moot.Slug, status: http.StatusOK},
			{path: "/v1/events/" + fx.unapprovedEvent.Slug, status: http.StatusNotFound},
		}
	},
	{Method: http.MethodGet, Path: "/v1/pieces/{slug}/history"}: func(fx leakFixture) []leakRequest {
		return []leakRequest{
			// Authors by display name, never by user ID.
			{path: historyPath("pieces", fx.piece.Slug), status: http.StatusOK},
			// An unapproved piece with an approved revision.
			{path: historyPath("pieces", fx.unapprovedPiece.Slug), status: http.StatusNotFound},
			{path: historyPath("pieces", fx.unapprovedEcho.Slug), status: http.StatusNotFound},
		}
	},
	{Method: http.MethodGet, Path: "/v1/persons/{slug}/history"}: func(fx leakFixture) []leakRequest {
		return []leakRequest{
			{path: historyPath("persons", fx.person.Slug), status: http.StatusOK},
			{path: historyPath("persons", fx.noPage.Slug), status: http.StatusOK},
			{path: historyPath("persons", fx.turned.Slug), status: http.StatusOK},
			{path: historyPath("persons", fx.renamed.Slug), status: http.StatusOK},
			{path: historyPath("persons", fx.unapprovedPerson.Slug), status: http.StatusNotFound},
		}
	},
	{Method: http.MethodGet, Path: "/v1/terms/{slug}/history"}: func(fx leakFixture) []leakRequest {
		return []leakRequest{
			{path: historyPath("terms", fx.lanterns.Slug), status: http.StatusOK},
			{path: historyPath("terms", fx.westmarch.Slug), status: http.StatusOK},
			{path: historyPath("terms", fx.unapprovedTerm.Slug), status: http.StatusNotFound},
			// Approved, under an unapproved term.
			{path: historyPath("terms", fx.underUnapproved.Slug), status: http.StatusNotFound},
		}
	},
	{Method: http.MethodGet, Path: "/v1/collections/{collection_id}/history"}: func(fx leakFixture) []leakRequest {
		return []leakRequest{
			{path: historyPath("collections", strconv.FormatInt(fx.archive.ID, 10)), status: http.StatusOK},
			{path: historyPath("collections", strconv.FormatInt(fx.unapprovedCollection.ID, 10)),
				status: http.StatusNotFound},
		}
	},
	{Method: http.MethodGet, Path: "/v1/events/{slug}/history"}: func(fx leakFixture) []leakRequest {
		return []leakRequest{
			{path: historyPath("events", fx.moot.Slug), status: http.StatusOK},
			{path: historyPath("events", fx.unapprovedEvent.Slug), status: http.StatusNotFound},
		}
	},
	{Method: http.MethodGet, Path: "/v1/revisions/{revision_id}"}: func(fx leakFixture) []leakRequest {
		revision := func(id int64, status int, reveals ...string) leakRequest {
			return leakRequest{path: "/v1/revisions/" + strconv.FormatInt(id, 10), status: status, reveals: reveals}
		}
		noPage := []string{fx.noPage.Slug, noPageName}
		turned := []string{fx.turned.Slug, turnedName}
		return []leakRequest{
			// Credits to the no-page and unapproved persons, unapproved terms, relation and collection, the
			// hidden link and its performances, unapproved performers and event: in the snapshot, and with a
			// credit to a person who chose not to have a page since, in a revision and its base.
			revision(fx.piece.RevisionID, http.StatusOK),
			revision(fx.revisions.lanternTurned, http.StatusOK),
			// The hidden alias, hidden in the snapshot, and public in a revision and hidden since; relations to
			// no-page and unapproved persons; the hidden link.
			revision(fx.person.RevisionID, http.StatusOK),
			revision(fx.revisions.rowanAliasPublic, http.StatusOK),
			revision(fx.group.RevisionID, http.StatusOK),
			// Persons who prefer no page, now or in the snapshot: the identity only.
			revision(fx.noPage.RevisionID, http.StatusOK, noPage...),
			revision(fx.noPageAdmirer.RevisionID, http.StatusOK, fx.noPageAdmirer.Slug, noPageAdmirerName),
			revision(fx.turned.RevisionID, http.StatusOK, turned...),
			revision(fx.revisions.turnedEdit, http.StatusOK, turned...),
			// A former name that is a hidden alias now, in a snapshot and in a base, with and without a page.
			revision(fx.revisions.renamedFormer, http.StatusOK),
			revision(fx.revisions.renamedEdit, http.StatusOK),
			revision(fx.revisions.renamedNoPageFormer, http.StatusOK),
			revision(fx.revisions.renamedNoPageEdit, http.StatusOK),
			// Links deleted, moved and hidden, hidden under another case, and respelled and hidden, since the
			// snapshot; a link entered with tracking parameters, shown as entered.
			revision(fx.ember.RevisionID, http.StatusOK, enteredLinkURL),
			// An unapproved child and related affiliation; an unapproved parent and region.
			revision(fx.lanterns.RevisionID, http.StatusOK),
			revision(fx.westmarch.RevisionID, http.StatusOK),
			revision(fx.archive.RevisionID, http.StatusOK),
			revision(fx.moot.RevisionID, http.StatusOK),
			revision(fx.underUnapproved.RevisionID, http.StatusNotFound),
			revision(fx.revisions.pendingLantern, http.StatusNotFound),
			// A rejected revision, and a rebased pending one whose original submission is marked.
			revision(fx.revisions.rejectedLantern, http.StatusNotFound),
			revision(fx.revisions.rebasedLantern, http.StatusNotFound),
			revision(fx.revisions.unapprovedPieceApproved, http.StatusNotFound),
		}
	},
	{Method: http.MethodGet, Path: needsWorkPath}: func(fx leakFixture) []leakRequest {
		return []leakRequest{
			// Persons who prefer no page would be listed for their unknown licensing.
			{path: needsWorkPath, status: http.StatusOK},
			{path: needsWorkPath + "?reason=missing_licensing", status: http.StatusOK},
			{path: needsWorkPath + "?reason=stub", status: http.StatusOK},
			// The Lantern Song's and Rowan's only dead link is hidden.
			{path: needsWorkPath + "?reason=dead_links", status: http.StatusOK,
				absent: []string{fx.piece.Slug, fx.person.Slug}},
		}
	},
}

// historyPath is the path of the history of the entity of collection with address.
func historyPath(collection, address string) string {
	return "/v1/" + collection + "/" + address + "/history"
}

// Reasons an operation is in leakExempt.
const (
	exemptAccount      = "the caller's own account"
	exemptAPIAuth      = "the caller's API tokens"
	exemptOwnRevisions = "the caller's own drafts and revisions; covered by TestHTTP_RevisionLeakMatrix"
	exemptModeration   = "moderation: full data for moderators; covered by TestHTTP_RevisionLeakMatrix"
)

// leakExempt are the operations that read no catalogue data, and why. A public /v1 GET cannot be exempt.
var leakExempt = map[openapitest.Operation]string{
	{Method: http.MethodGet, Path: healthzPath}:                                     "health check",
	{Method: http.MethodGet, Path: readyzPath}:                                      "health check",
	{Method: http.MethodGet, Path: mePath}:                                          exemptAccount,
	{Method: http.MethodPatch, Path: mePath}:                                        exemptAccount,
	{Method: http.MethodPut, Path: "/v1/users/{user_id}/blocked"}:                   "moderation of accounts",
	{Method: http.MethodPost, Path: tokensPath}:                                     exemptAPIAuth,
	{Method: http.MethodGet, Path: tokensPath}:                                      exemptAPIAuth,
	{Method: http.MethodDelete, Path: "/v1/tokens/{token_id}"}:                      exemptAPIAuth,
	{Method: http.MethodPost, Path: draftsPath}:                                     exemptOwnRevisions,
	{Method: http.MethodGet, Path: draftPath}:                                       exemptOwnRevisions,
	{Method: http.MethodPut, Path: draftPath}:                                       exemptOwnRevisions,
	{Method: http.MethodDelete, Path: draftPath}:                                    exemptOwnRevisions,
	{Method: http.MethodPost, Path: revisionsPath}:                                  exemptOwnRevisions,
	{Method: http.MethodPost, Path: "/v1/revisions/{revision_id}/withdraw"}:         exemptOwnRevisions,
	{Method: http.MethodPost, Path: "/v1/revisions/{revision_id}/revert"}:           exemptOwnRevisions,
	{Method: http.MethodGet, Path: myRevisionsPath}:                                 exemptOwnRevisions,
	{Method: http.MethodGet, Path: myRevisionPath}:                                  exemptOwnRevisions,
	{Method: http.MethodGet, Path: "/v1/pieces/{slug}/editable"}:                    exemptOwnRevisions,
	{Method: http.MethodGet, Path: "/v1/persons/{slug}/editable"}:                   exemptOwnRevisions,
	{Method: http.MethodGet, Path: "/v1/terms/{slug}/editable"}:                     exemptOwnRevisions,
	{Method: http.MethodGet, Path: "/v1/events/{slug}/editable"}:                    exemptOwnRevisions,
	{Method: http.MethodGet, Path: "/v1/collections/{collection_id}/editable"}:      exemptOwnRevisions,
	{Method: http.MethodGet, Path: "/v1/pieces/{slug}/pending"}:                     exemptOwnRevisions,
	{Method: http.MethodGet, Path: "/v1/persons/{slug}/pending"}:                    exemptOwnRevisions,
	{Method: http.MethodGet, Path: "/v1/terms/{slug}/pending"}:                      exemptOwnRevisions,
	{Method: http.MethodGet, Path: "/v1/events/{slug}/pending"}:                     exemptOwnRevisions,
	{Method: http.MethodGet, Path: "/v1/collections/{collection_id}/pending"}:       exemptOwnRevisions,
	{Method: http.MethodPost, Path: reportLinkPath}:                                 "writes a link report; returns only its receipt",
	{Method: http.MethodGet, Path: moderationQueuePath}:                             exemptModeration,
	{Method: http.MethodGet, Path: moderationRevisionRoute}:                         exemptModeration,
	{Method: http.MethodPost, Path: "/v1/revisions/{revision_id}/approve"}:          exemptModeration,
	{Method: http.MethodPost, Path: "/v1/revisions/{revision_id}/reject"}:           exemptModeration,
	{Method: http.MethodPost, Path: "/v1/revisions/{revision_id}/rebase"}:           exemptModeration,
	{Method: http.MethodGet, Path: moderationReportsPath}:                           exemptModeration,
	{Method: http.MethodPost, Path: "/v1/moderation/link-reports/{report_id}"}:      exemptModeration,
	{Method: http.MethodGet, Path: moderationLinksPath}:                             exemptModeration,
	{Method: http.MethodPut, Path: "/v1/links/{link_id}/hidden"}:                    exemptModeration,
	{Method: http.MethodPost, Path: "/v1/contact"}:                                  "writes a contact request; returns no catalogue data",
	{Method: http.MethodGet, Path: "/v1/moderation/contact-requests"}:               exemptModeration,
	{Method: http.MethodPost, Path: "/v1/moderation/contact-requests/{request_id}"}: exemptModeration,
}

// TestHTTP_LeakMatrix sends every leakMatrix request to the production handler over a real repository holding
// the leak fixture, and checks that no raw response body contains a marked string it does not reveal, and that
// every response honours the contract. Every operation in the spec needs a row or an exemption.
func TestHTTP_LeakMatrix(t *testing.T) {
	t.Parallel()

	db, fx := leakCatalogue.Get(t)
	requireLeakMarkersStored(t, db)
	handler := NewHTTPHandler(repositoryHTTPDependencies(mysql.New(db), nil, passingTurnstile{}))
	contract := openapitest.New(t)

	operations := contract.Operations()
	for op := range leakMatrix {
		assert.Contains(t, operations, op, "leakMatrix has a row for an operation the spec does not have")
		assert.True(t, op.Method == http.MethodGet && contract.Public(op), "%s %s is not a public read", op.Method,
			op.Path)
	}
	for op := range leakExempt {
		assert.Contains(t, operations, op, "leakExempt lists an operation the spec does not have")
	}

	for _, op := range operations {
		requests, hasRow := leakMatrix[op]
		_, exempt := leakExempt[op]
		switch {
		case hasRow && exempt:
			t.Errorf("%s %s has a row in leakMatrix and is in leakExempt", op.Method, op.Path)
			continue
		case exempt:
			assert.False(t, op.Method == http.MethodGet && contract.Public(op) && strings.HasPrefix(op.Path, "/v1/"),
				"%s %s is a public catalogue read, so it needs a row in leakMatrix rather than an exemption",
				op.Method, op.Path)
			continue
		case !hasRow:
			t.Errorf("%s %s has no row in leakMatrix and is not in leakExempt", op.Method, op.Path)
			continue
		}

		t.Run(op.Method+" "+op.Path, func(t *testing.T) {
			t.Parallel()

			for _, req := range requests(fx) {
				t.Run(req.path, func(t *testing.T) {
					t.Parallel()
					assertNoLeak(t, contract, handler, req)
				})
			}
		})
	}
}

func assertNoLeak(t *testing.T, contract *openapitest.Validator, handler http.Handler, req leakRequest) {
	t.Helper()

	rec := contract.Serve(t, handler, httptest.NewRequestWithContext(t.Context(), http.MethodGet, req.path, nil))
	require.Equal(t, req.status, rec.Code, rec.Body.String())

	assert.Empty(t, unusedReveals(rec.Body.String(), req.reveals), "the response does not show these revealed strings")
	assert.Empty(t, leakedMarker(rec.Body.String(), req.reveals), "the response shows a marked string:\n%s",
		rec.Body.String())
	for _, absent := range req.absent {
		assert.NotContains(t, rec.Body.String(), absent, "the response shows something it must not")
	}
}

// requireLeakMarkersStored checks that the fixture really stores a marker of each kind, so that the matrix
// cannot pass because a marker went missing from the fixture.
func requireLeakMarkersStored(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, query := range []string{
		"SELECT COUNT(*) FROM person_aliases WHERE NOT is_public AND kind = 'legal' AND name LIKE '%Leakmark%'",
		"SELECT COUNT(*) FROM persons WHERE status = 'prefers_no_page' AND notes_md LIKE '%Leakmark%'",
		"SELECT COUNT(*) FROM links WHERE is_hidden AND url LIKE '%leakmark%'",
		"SELECT COUNT(*) FROM pieces WHERE current_revision_id IS NULL AND title LIKE '%Leakmark%'",
		"SELECT COUNT(*) FROM persons WHERE current_revision_id IS NULL AND name LIKE '%Leakmark%'",
		"SELECT COUNT(*) FROM terms WHERE current_revision_id IS NULL AND name LIKE '%Leakmark%'",
		"SELECT COUNT(*) FROM persons WHERE claimed_by_user_id LIKE '%leakmark%'",
		"SELECT COUNT(*) FROM links WHERE url = '" + enteredLinkURL + "' " +
			"AND normalized_url = '" + normalizedLinkURL + "'",
		"SELECT COUNT(*) FROM collections WHERE current_revision_id IS NULL AND name LIKE '%Leakmark%'",
		"SELECT COUNT(*) FROM events WHERE current_revision_id IS NULL AND name LIKE '%Leakmark%'",
		// Marked performances at a public event, and marked links in a public collection.
		"SELECT COUNT(*) FROM performances perf JOIN events e ON e.id = perf.event_id " +
			"WHERE e.current_revision_id IS NOT NULL AND perf.description_verbatim LIKE '%Leakmark%'",
		"SELECT COUNT(*) FROM links l JOIN collections c ON c.id = l.collection_id " +
			"WHERE c.current_revision_id IS NOT NULL AND l.url LIKE '%leakmark%'",
		// Approved revisions of public entities showing, as they were then, what is hidden now: the hidden alias
		// as public, a person who has since chosen not to have a page with their profile, and the hidden link.
		"SELECT COUNT(*) FROM revisions r JOIN persons p ON p.id = r.entity_id AND r.entity_type = 'person' " +
			"WHERE r.status = 'approved' AND p.current_revision_id IS NOT NULL AND JSON_CONTAINS(r.snapshot_json, " +
			"JSON_OBJECT('name', 'Leakmark Hidden Legal Alias', 'is_public', TRUE), '$.aliases')",
		"SELECT COUNT(*) FROM revisions r JOIN persons p ON p.id = r.entity_id AND r.entity_type = 'person' " +
			"WHERE r.status = 'approved' AND p.status = 'prefers_no_page' " +
			"AND r.snapshot_json->>'$.status' = 'active' AND r.snapshot_json->>'$.notes_md' LIKE '%Leakmark%'",
		"SELECT COUNT(*) FROM revisions r JOIN pieces p ON p.id = r.entity_id AND r.entity_type = 'piece' " +
			"WHERE r.status = 'approved' AND p.current_revision_id IS NOT NULL " +
			"AND JSON_SEARCH(r.snapshot_json, 'one', '" + hiddenLinkURL + "', NULL, '$.links[*].url') IS NOT NULL",
		// A user ID of revision authors, and a hidden link that is dead.
		"SELECT COUNT(*) FROM revisions WHERE author_user_id LIKE '%leakmark%'",
		"SELECT COUNT(*) FROM links WHERE is_hidden AND liveness = 'dead'",
		// A public person performing on a link attached to a no-page person only, and on one attached to an
		// unapproved person only.
		performedOnHiddenAttachments + "'%Leakmark nopage busker%'",
		performedOnHiddenAttachments + "'%Leakmark unapproved busker%'",
		// Approved revisions of public persons, with and without a page, named with what is a hidden alias now.
		"SELECT COUNT(*) FROM revisions r JOIN person_aliases a ON a.person_id = r.entity_id " +
			"JOIN persons p ON p.id = r.entity_id AND p.status = 'active' AND p.current_revision_id IS NOT NULL " +
			"WHERE r.entity_type = 'person' AND r.status = 'approved' AND NOT a.is_public " +
			"AND r.snapshot_json->>'$.name' = a.name",
		"SELECT COUNT(*) FROM revisions r JOIN person_aliases a ON a.person_id = r.entity_id " +
			"JOIN persons p ON p.id = r.entity_id AND p.status = 'prefers_no_page' " +
			"WHERE r.entity_type = 'person' AND r.status = 'approved' AND NOT a.is_public " +
			"AND r.snapshot_json->>'$.name' = a.name",
		// An approved revision of a public piece holding a URL no link has, one a hidden link had, and one a
		// hidden link has but in another case.
		"SELECT COUNT(*) FROM revisions r WHERE r.status = 'approved' AND JSON_SEARCH(r.snapshot_json, 'one', '" +
			deletedLinkURL + "', NULL, '$.links[*].url') IS NOT NULL " +
			"AND NOT EXISTS (SELECT 1 FROM links WHERE url = '" + deletedLinkURL + "')",
		"SELECT COUNT(*) FROM links WHERE is_hidden AND url = '" + movedLinkURL + "-moved'",
		"SELECT COUNT(*) FROM links WHERE is_hidden AND CAST(url AS BINARY) = LOWER('" + recasedLinkURL + "') " +
			"AND CAST(url AS BINARY) <> '" + recasedLinkURL + "'",
		// A link the revision holds under another spelling, with the same normalized URL, that is hidden now.
		"SELECT COUNT(*) FROM revisions r JOIN links l ON l.is_hidden AND l.url = '" + respelledLinkURLNow + "' " +
			"AND l.normalized_url = 'https://example.org/leakmark-respelled-link' WHERE r.status = 'approved' " +
			"AND JSON_SEARCH(r.snapshot_json, 'one', '" + respelledLinkURL + "', NULL, '$.links[*].url') IS NOT NULL",
		// A rejected revision with marked notes; a pending revision a moderator rebased, whose original submission
		// alone holds its marker; a marked message of an open report of a visible link; an open contact request.
		"SELECT COUNT(*) FROM revisions WHERE status = 'rejected' AND snapshot_json->>'$.notes_md' = '" +
			rejectedNotes + "'",
		"SELECT COUNT(*) FROM revisions WHERE status = 'pending' AND rebased_by_user_id IS NOT NULL " +
			"AND submitted_snapshot_json->>'$.notes_md' = '" + rebasedOriginal + "' " +
			"AND snapshot_json->>'$.notes_md' <> '" + rebasedOriginal + "'",
		"SELECT COUNT(*) FROM link_reports r JOIN links l ON l.id = r.link_id " +
			"WHERE r.status = 'open' AND NOT l.is_hidden AND r.message = '" + reportMessage + "'",
		"SELECT COUNT(*) FROM contact_requests WHERE status = 'open' AND name = '" + contactName + "' " +
			"AND email = '" + contactEmail + "' AND message = '" + contactMessage + "'",
	} {
		var count int
		require.NoError(t, db.QueryRowContext(t.Context(), query).Scan(&count), query)
		require.Positive(t, count, query)
	}
}

// performedOnHiddenAttachments, followed by a LIKE pattern, counts the performances with a description matching
// it that a public person with a page performs in, on a link attached to no public piece and no public person
// with a page.
const performedOnHiddenAttachments = "SELECT COUNT(*) FROM performances perf " +
	"JOIN performance_performers pp ON pp.performance_id = perf.id JOIN persons p ON p.id = pp.person_id " +
	"WHERE p.current_revision_id IS NOT NULL AND p.status <> 'prefers_no_page' " +
	"AND NOT EXISTS (SELECT 1 FROM link_attachments a LEFT JOIN pieces ap ON ap.id = a.piece_id " +
	"LEFT JOIN persons apr ON apr.id = a.person_id WHERE a.link_id = perf.link_id " +
	"AND (ap.current_revision_id IS NOT NULL OR (apr.current_revision_id IS NOT NULL " +
	"AND apr.status <> 'prefers_no_page'))) AND perf.description_verbatim LIKE "

// TestHTTP_LeakMatrix_PlantedLeakFails shows that the matrix's check can fail on the rows that reveal the no-page
// person's name: the real response passes, and the same response with a no-page note planted next to the name,
// which starts with the revealed name, does not.
func TestHTTP_LeakMatrix_PlantedLeakFails(t *testing.T) {
	t.Parallel()

	db, fx := leakCatalogue.Get(t)
	handler := NewHTTPHandler(repositoryHTTPDependencies(mysql.New(db), nil, passingTurnstile{}))

	req := leakRequest{path: "/v1/persons/" + fx.noPage.Slug, status: http.StatusOK,
		reveals: []string{fx.noPage.Slug, noPageName}}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, req.path, nil))
	require.Equal(t, req.status, rec.Code, rec.Body.String())
	body := rec.Body.String()
	require.Contains(t, body, `"`+noPageName+`"`, "the page no longer shows the name, so this test proves nothing")
	require.Empty(t, leakedMarker(body, req.reveals))

	planted := strings.Replace(body, `"`+noPageName+`"`,
		`"`+noPageName+`","planted":"`+noPageName+` notes"`, 1)
	assert.NotEmpty(t, leakedMarker(planted, req.reveals))
}

// TestHTTP_LeakMatrix_RedactedOnlyRevisionsChangeNothing checks, through the handler, that a revision whose only
// differences from its base are things the public view hides lists no changed fields: the changed fields are
// computed on the redacted views, so they cannot give away what was redacted.
func TestHTTP_LeakMatrix_RedactedOnlyRevisionsChangeNothing(t *testing.T) {
	t.Parallel()

	db, fx := leakCatalogue.Get(t)
	handler := NewHTTPHandler(repositoryHTTPDependencies(mysql.New(db), nil, passingTurnstile{}))

	for name, id := range map[string]int64{
		// Adds an alias that is hidden now, and a relation to a person who now prefers no page.
		"hidden_alias_and_no_page_relation": fx.revisions.rowanAliasPublic,
		// Adds a credit to a person who now prefers no page.
		"credit_to_no_page_person": fx.revisions.lanternTurned,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet,
				"/v1/revisions/"+strconv.FormatInt(id, 10), nil))
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

			var body struct {
				ChangedFields *[]string `json:"changed_fields"`
				Base          any       `json:"base_snapshot"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.NotNil(t, body.ChangedFields, "changed_fields is present")
			assert.Empty(t, *body.ChangedFields)
			assert.NotNil(t, body.Base, "the revision has a base, so an empty list means no change")
		})
	}
}
