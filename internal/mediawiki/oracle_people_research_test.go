package mediawiki

import (
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/wikidumptest"
)

type oraclePersonItem struct {
	Type       ValueItemType `json:"type"`
	Target     string        `json:"target"`
	Label      string        `json:"label"`
	Qualifies  string        `json:"qualifies"`
	ResolvesTo string        `json:"resolves_to"`
}

type oraclePeoplePiece struct {
	PageID int64                         `json:"pageid"`
	Title  string                        `json:"title"`
	People map[string][]oraclePersonItem `json:"people"`
}

type oracleWork struct {
	Title      string `json:"title"`
	Exists     bool   `json:"exists"`
	ResolvesTo string `json:"resolves_to"`
}

type oracleBardRecord struct {
	PageID                   int64        `json:"pageid"`
	Title                    string       `json:"title"`
	RedirectsHere            []string     `json:"redirects_here"`
	WorksListed              []oracleWork `json:"works_listed"`
	PerformerTypes           []string     `json:"performer_types"`
	KingdomOrGroupCategories []string     `json:"kingdom_or_group_categories"`
	InMemoriam               bool         `json:"in_memoriam"`
	Licensing                *struct {
		Status string `json:"status"`
	} `json:"licensing_etiquette"`
}

type oracleBardWork struct {
	Bard string `json:"bard"`
	Work string `json:"work"`
}

type oracleCreditQuality struct {
	MissingBardCategory   []oracleTitled   `json:"missing_bard_category"`
	CategoryWithoutLink   []oracleTitled   `json:"bard_category_without_infobox_link"`
	WorksWithoutPiecePage []oracleBardWork `json:"works_listed_on_bard_pages_without_piece_page"`
}

type peopleOracle struct {
	site    Site
	pieces  []PieceRecord
	people  People
	ref     []oraclePeoplePiece
	bards   []oracleBardRecord
	quality oracleCreditQuality
	links   map[string][]string
}

func loadPeopleOracle(t *testing.T) peopleOracle {
	t.Helper()
	dumpFS, analysisFS := wikidumptest.Research(t)
	dump, err := LoadDump(dumpFS)
	wikidumptest.NoError(t, "dump", err)
	o := peopleOracle{site: NewSite(dump)}
	o.pieces = o.site.Pieces()
	o.people = ParsePeople(o.site, o.pieces)
	var pieces struct {
		Pieces []oraclePeoplePiece `json:"pieces"`
	}
	wikidumptest.NoError(t, "pieces.json", decodeAnalysis(analysisFS, "pieces.json", &pieces))
	o.ref = pieces.Pieces
	var bards struct {
		Bards []oracleBardRecord `json:"bards"`
	}
	wikidumptest.NoError(t, "bards.json", decodeAnalysis(analysisFS, "bards.json", &bards))
	o.bards = bards.Bards
	wikidumptest.NoError(t, "quality.json", decodeAnalysis(analysisFS, "quality.json", &o.quality))
	wikidumptest.NoError(t, "redlinks.json", decodeAnalysis(analysisFS, "redlinks.json", &o.links))

	return o
}

func TestResearchOracle_PeopleItems(t *testing.T) {
	o := loadPeopleOracle(t)
	deviation(t, "credits.repaired_markup")
	byID := map[int64]PieceRecord{}
	for _, p := range o.pieces {
		byID[p.PageID] = p
	}
	for _, want := range o.ref {
		piece := byID[want.PageID]
		if slices.ContainsFunc(piece.MarkupIssues, func(m MarkupIssue) bool { return m.Kind == MarkupSingleBracketCategory }) {
			continue
		}
		got := map[string][]oraclePersonItem{}
		for _, b := range piece.Parsed.Infoboxes {
			for _, param := range personParams {
				if p, ok := b.Param(param.name); ok {
					got[param.name] = personItems(p.Value, o.site)
				}
			}
		}
		wikidumptest.Equal(t, wikidumptest.Location{PageID: want.PageID, Field: "people", Offset: -1}, want.People, got)
	}
}

func personItems(v Value, s Site) []oraclePersonItem {
	out := []oraclePersonItem{}
	for _, it := range v.Items {
		item := oraclePersonItem{Type: it.Type, Target: it.Target, Label: it.Label, Qualifies: it.Qualifies}
		if it.Type == ValuePageLink && s.ResolveRedirect(it.Target) != it.Target {
			item.ResolvesTo = s.ResolveRedirect(it.Target)
		}
		out = append(out, item)
	}

	return out
}

func TestResearchOracle_CreditNamesFollowTheItems(t *testing.T) {
	o := loadPeopleOracle(t)
	for _, piece := range o.pieces {
		var want, got []string
		for _, b := range piece.Parsed.Infoboxes {
			for _, param := range personParams {
				p, _ := b.Param(param.name)
				for _, it := range p.Value.Items {
					if it.Type == ValuePageLink && it.Qualifies == "" {
						if _, pseudo := attributionWords[strings.ToLower(o.site.ResolveRedirect(it.Target))]; !pseudo {
							want = append(want, o.site.ResolveRedirect(it.Target))
						}
					}
				}
			}
		}
		for _, c := range piece.Credits {
			if c.Name.Form == NamePageLink {
				got = append(got, c.Name.Name)
			}
		}
		wikidumptest.Equal(t, wikidumptest.Location{PageID: piece.PageID, Field: "credits.page_links", Offset: -1}, want,
			got)
	}
}

func TestResearchOracle_Bards(t *testing.T) {
	o := loadPeopleOracle(t)
	byID := map[int64]BardRecord{}
	for _, b := range o.people.Bards {
		byID[b.PageID] = b
	}
	wikidumptest.Equal(t, wikidumptest.Location{Field: "bards.count", Offset: -1}, len(o.bards), len(byID))
	for _, want := range o.bards {
		compareBard(t, want, byID[want.PageID])
	}
}

func compareBard(t *testing.T, want oracleBardRecord, b BardRecord) {
	t.Helper()
	at := func(field string) wikidumptest.Location {
		return wikidumptest.Location{PageID: want.PageID, Field: field, Offset: -1}
	}
	wikidumptest.Equal(t, at("redirects_here"), sorted(want.RedirectsHere), sorted(redirectNames(b)))
	wikidumptest.Equal(t, at("works_listed"), want.WorksListed, oracleWorks(b))
	var roles []string
	for _, c := range want.PerformerTypes {
		roles = append(roles, string(performerRoles[c]))
	}
	wikidumptest.Equal(t, at("performer_types"), sorted(roles), sorted(roleNames(b)))
	wikidumptest.Equal(t, at("kingdom_or_group_categories"), sorted(want.KingdomOrGroupCategories),
		sorted(groupCategories(b)))
	wikidumptest.Equal(t, at("in_memoriam"), want.InMemoriam, b.Status == domain.PersonStatusDeceased)
	// The reference's "filled" sections keep notes; its "cannot find" placeholders leave the status unknown.
	filled := want.Licensing != nil && want.Licensing.Status == "filled"
	wikidumptest.Equal(t, at("licensing.filled_has_notes"), filled, filled && b.LicensingNotesMD != "")
	wikidumptest.Equal(t, at("licensing.placeholder_is_unknown"), !filled,
		!filled && b.LicensingStatus == domain.LicensingStatusUnknown)
}

func redirectNames(b BardRecord) []string {
	var out []string
	for _, a := range b.Aliases {
		if a.Source == AliasFromRedirect {
			out = append(out, a.Name)
		}
	}

	return out
}

func oracleWorks(b BardRecord) []oracleWork {
	out := []oracleWork{}
	for _, w := range b.Works {
		ow := oracleWork{Title: w.Title, Exists: w.Exists}
		if w.Target != w.Title {
			ow.ResolvesTo = w.Target
		}
		out = append(out, ow)
	}

	return out
}

func roleNames(b BardRecord) []string {
	var out []string
	for _, r := range b.Roles {
		out = append(out, string(r))
	}

	return out
}

// groupCategories are the categories the reference calls kingdom_or_group: affiliation terms other than the
// organisations, and the dropped placeholders.
func groupCategories(b BardRecord) []string {
	var out []string
	for _, c := range append(slices.Clone(b.Affiliations), b.DroppedCategories...) {
		if !bardAffiliations[c] {
			out = append(out, c)
		}
	}

	return out
}

func TestResearchOracle_RedLinks(t *testing.T) {
	o := loadPeopleOracle(t)
	want := map[string][]string{}
	for k, pages := range o.links {
		want[k] = sorted(pages)
	}
	wikidumptest.Equal(t, wikidumptest.Location{Field: "redlinks", Offset: -1}, want, RedLinks(o.site))
}

func TestResearchOracle_CreditSignals(t *testing.T) {
	o := loadPeopleOracle(t)
	pieces := map[CreditDisagreementKind][]string{}
	for _, d := range o.people.Disagreements {
		if !slices.Contains(pieces[d.Kind], d.Piece) {
			pieces[d.Kind] = append(pieces[d.Kind], d.Piece)
		}
	}
	titles := func(list []oracleTitled) []string {
		var out []string
		for _, x := range list {
			out = append(out, x.Title)
		}

		return sorted(out)
	}
	wikidumptest.Equal(t, wikidumptest.Location{Field: "missing_bard_category", Offset: -1},
		titles(o.quality.MissingBardCategory), sorted(pieces[DisagreementMissingBardCategory]))
	wikidumptest.Equal(t, wikidumptest.Location{Field: "bard_category_without_infobox_link", Offset: -1},
		titles(o.quality.CategoryWithoutLink), sorted(pieces[DisagreementCategoryWithoutLinkedCredit]))
	works := []oracleBardWork{}
	for _, b := range o.people.Bards {
		for _, w := range b.Works {
			if !w.Exists {
				works = append(works, oracleBardWork{Bard: b.Title, Work: w.Title})
			}
		}
	}
	byBard := func(list []oracleBardWork) []oracleBardWork {
		out := slices.Clone(list)
		sort.SliceStable(out, func(a, b int) bool { return out[a].Bard < out[b].Bard })

		return out
	}
	wikidumptest.Equal(t, wikidumptest.Location{Field: "works_listed_on_bard_pages_without_piece_page", Offset: -1},
		byBard(o.quality.WorksWithoutPiecePage), byBard(works))
}

// peopleCounts are the counts the scrubbed copy must preserve.
type peopleCounts struct {
	Bards, StubPieces, Stubs int
	RedLinks                 map[RedLinkClass]int
	Disagreements            map[CreditDisagreementKind]int
	Credits                  map[string]int
}

func countPeople(p People) peopleCounts {
	c := peopleCounts{Bards: len(p.Bards), StubPieces: len(p.StubPieces), Stubs: len(p.Persons.Stubs),
		RedLinks: map[RedLinkClass]int{}, Disagreements: disagreementPieces(p), Credits: map[string]int{}}
	for _, r := range p.RedLinks {
		c.RedLinks[r.Class]++
	}
	for _, credits := range p.Credits {
		for _, pc := range credits {
			c.Credits[string(pc.Attribution)+"/"+string(pc.Certainty)]++
		}
	}

	return c
}

func TestResearchPeople_ScrubbedCopyPreservesCounts(t *testing.T) {
	o := loadPeopleOracle(t)
	dump, err := LoadDump(wikidumptest.Scrubbed(t))
	wikidumptest.NoError(t, "scrubbed dump", err)
	s := NewSite(dump)
	wikidumptest.Equal(t, wikidumptest.Location{Field: "people counts", Offset: -1}, countPeople(o.people),
		countPeople(ParsePeople(s, s.Pieces())))
}

func sorted(list []string) []string {
	out := append([]string{}, list...)
	sort.Strings(out)

	return out
}
