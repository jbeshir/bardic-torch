//go:build integration

package mysql_test

import (
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// needsWorkCatalogue holds pieces and persons needing work for each reason, alone and together, and ones that
// must not be listed although they would be for their fields: restricted pieces that are not stubs, pieces and
// persons whose only dead link is hidden, persons who are not community bards with unknown licensing, a person
// who prefers no page, and unapproved entities.
var needsWorkCatalogue = catalogtest.NewShared(func(c *catalogtest.Catalog) struct{} {
	// Sort keys that differ from the titles and names, and entities inserted out of order, so that neither the
	// title, the name nor the ID gives the order the list must have.
	sortTitles := map[string]string{"The Dirge of Dust": "Dirge of Dust"}
	sortNames := map[string]string{"Eda Deadlink": "Aaron Eda"}
	piece := func(title string, status domain.ConservationStatus, licensing domain.LicensingStatus, stub bool,
		links ...string) {
		doc := domain.PieceDocument{Title: title, IsStub: stub, LicensingStatus: licensing,
			Conservation: domain.DocConservation{Status: status}}
		for _, url := range links {
			doc.Links = append(doc.Links, domain.DocPieceLink{DocLink: domain.DocLink{URL: url,
				Roles: []domain.LinkRole{domain.LinkRoleLyrics}}})
		}
		c.Piece(catalogtest.Piece{Doc: doc, SortTitle: sortTitles[title]})
	}
	person := func(name string, kind domain.PersonKind, licensing domain.LicensingStatus, stub bool,
		status domain.PersonStatus, links ...string) {
		doc := domain.PersonDocument{Name: name, Kind: kind, LicensingStatus: licensing, IsStub: stub, Status: status}
		for _, url := range links {
			doc.Links = append(doc.Links, domain.DocPersonLink{DocLink: domain.DocLink{URL: url,
				Roles: []domain.LinkRole{domain.LinkRoleWebsite}}})
		}
		c.Person(catalogtest.Person{Doc: doc, SortName: sortNames[name]})
	}
	const (
		dead       = "https://example.org/dead"
		hiddenDead = "https://example.org/hidden-dead"
		alive      = "https://example.org/alive"
	)
	known, unknown := domain.LicensingStatusKnown, domain.LicensingStatusUnknown
	lowRisk, restricted := domain.ConservationStatusLowRisk, domain.ConservationStatusRestricted

	piece("Barley Song", domain.ConservationStatusUnassessed, known, false)
	piece("Candle Song", lowRisk, unknown, false)
	piece("The Dirge of Dust", lowRisk, known, false, alive, dead)
	piece("Ember Song", lowRisk, known, false, hiddenDead)
	piece("Fen Song", restricted, unknown, true, dead)
	piece("Gale Song", restricted, unknown, false, dead)
	piece("Hollow Song", lowRisk, known, false, alive)
	piece("Iron Song", domain.ConservationStatusUnassessed, unknown, true, dead, hiddenDead)
	piece("Anvil Song", lowRisk, known, true)
	c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "Jade Song", IsStub: true}, Unapproved: true})

	active, noPage := domain.PersonStatusActive, domain.PersonStatusPrefersNoPage
	person("Bryn Unsure", domain.PersonKindCommunityBard, unknown, false, active)
	person("Cass Ancient", domain.PersonKindHistorical, unknown, false, active)
	person("Dell Choir", domain.PersonKindGroup, unknown, false, active)
	person("Eda Deadlink", domain.PersonKindCommunityBard, known, false, active, dead)
	person("Fay Quiet", domain.PersonKindCommunityBard, unknown, true, noPage, dead)
	person("Gil Hidden", domain.PersonKindCommunityBard, known, false, active, hiddenDead)
	person("Hal Deceased", domain.PersonKindCommunityBard, known, false, domain.PersonStatusDeceased, alive)
	person("Ada Stubbs", domain.PersonKindHistorical, known, true, active)
	c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: "Ivo Pending", IsStub: true}, Unapproved: true})

	c.SetLinkState(dead, catalogtest.LinkState{Liveness: domain.LivenessDead})
	c.SetLinkState(hiddenDead, catalogtest.LinkState{Liveness: domain.LivenessDead, IsHidden: true})
	c.SetLinkState(alive, catalogtest.LinkState{Liveness: domain.LivenessOK})

	return struct{}{}
})

func needsWorkEntry(entityType domain.EntityType, slug, title string, reasons ...domain.NeedsWorkReason,
) domain.NeedsWorkEntry {
	return domain.NeedsWorkEntry{EntityType: entityType, Slug: slug, Title: title, Reasons: reasons}
}

// TestRepository_ListPublicNeedsWork checks the needs-work list: every reason, alone and together;
// restricted pieces only for being stubs; hidden dead links ignored; licensing only for community bards; no
// person who prefers no page and nothing unapproved; pieces first, by sort key (not by title, name or ID); the reason filter; counts
// for every reason whatever the filter; and pages.
func TestRepository_ListPublicNeedsWork(t *testing.T) {
	t.Parallel()

	db, _ := needsWorkCatalogue.Get(t)
	repo := mysql.New(db)

	piece, person := domain.EntityTypePiece, domain.EntityTypePerson
	stub, unassessed := domain.NeedsWorkReasonStub, domain.NeedsWorkReasonUnassessed
	licensing, deadLinks := domain.NeedsWorkReasonMissingLicensing, domain.NeedsWorkReasonDeadLinks
	anvil := needsWorkEntry(piece, "anvil-song", "Anvil Song", stub)
	barley := needsWorkEntry(piece, "barley-song", "Barley Song", unassessed)
	candle := needsWorkEntry(piece, "candle-song", "Candle Song", licensing)
	dirge := needsWorkEntry(piece, "the-dirge-of-dust", "The Dirge of Dust", deadLinks)
	fen := needsWorkEntry(piece, "fen-song", "Fen Song", stub)
	iron := needsWorkEntry(piece, "iron-song", "Iron Song", stub, unassessed, licensing, deadLinks)
	ada := needsWorkEntry(person, "ada-stubbs", "Ada Stubbs", stub)
	bryn := needsWorkEntry(person, "bryn-unsure", "Bryn Unsure", licensing)
	eda := needsWorkEntry(person, "eda-deadlink", "Eda Deadlink", deadLinks)
	counts := []domain.NeedsWorkCount{
		{Reason: stub, Count: 4}, {Reason: unassessed, Count: 2}, {Reason: licensing, Count: 3},
		{Reason: deadLinks, Count: 3},
	}
	all := domain.Page{Limit: domain.DefaultPageLimit}

	for _, tc := range []struct {
		name   string
		reason *domain.NeedsWorkReason
		page   domain.Page
		want   domain.NeedsWorkList
	}{
		{name: "every_reason", page: all, want: domain.NeedsWorkList{
			Entries: []domain.NeedsWorkEntry{anvil, barley, candle, dirge, fen, iron, eda, ada, bryn},
			Total:   9, Counts: counts,
		}},
		{name: "stub", reason: &stub, page: all, want: domain.NeedsWorkList{
			Entries: []domain.NeedsWorkEntry{anvil, fen, iron, ada}, Total: 4, Counts: counts,
		}},
		{name: "unassessed", reason: &unassessed, page: all, want: domain.NeedsWorkList{
			Entries: []domain.NeedsWorkEntry{barley, iron}, Total: 2, Counts: counts,
		}},
		{name: "missing_licensing", reason: &licensing, page: all, want: domain.NeedsWorkList{
			Entries: []domain.NeedsWorkEntry{candle, iron, bryn}, Total: 3, Counts: counts,
		}},
		{name: "dead_links", reason: &deadLinks, page: all, want: domain.NeedsWorkList{
			Entries: []domain.NeedsWorkEntry{dirge, iron, eda}, Total: 3, Counts: counts,
		}},
		{name: "page_across_types", page: domain.Page{Limit: 2, Offset: 5}, want: domain.NeedsWorkList{
			Entries: []domain.NeedsWorkEntry{iron, eda}, Total: 9, Counts: counts,
		}},
		{name: "past_the_end", reason: &stub, page: domain.Page{Limit: 2, Offset: 4}, want: domain.NeedsWorkList{
			Entries: []domain.NeedsWorkEntry{}, Total: 4, Counts: counts,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			list, err := repo.ListPublicNeedsWork(t.Context(), tc.reason, tc.page)
			require.NoError(t, err)
			assert.Equal(t, tc.want, list)
		})
	}
}
