package mediawiki

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/wikidumptest"
)

func TestClassifyCategories_Classes(t *testing.T) {
	w := standardWiki()
	w.category("Singers")
	w.category("In Memoriam")
	w.category(CategoryVenue)
	w.category(catKingdomName)
	w.category("Ansteorra")
	w.page(Page{Title: ash, Categories: []string{CategoryBards, "Singers", "In Memoriam", catKingdomName,
		"Ansteorra", groupBarony}})
	w.page(Page{Title: aSong, Wikitext: pieceWikitext(aSong, [][2]string{{lyricsByParam, "[[" + rowan + "]]"}}),
		Categories: pieceCategories(wren, rowan, catHope, catMixed)})
	w.page(Page{Title: "Green Inn", Categories: []string{CategoryVenue, catMixed}})
	w.page(Page{Title: "Other Bard", Categories: []string{CategoryBards, catMixed}})
	w.page(Page{Title: "Other Piece", Categories: []string{CategoryPieces, catMixed}})
	tax := ClassifyCategories(w.dump())

	for name, want := range map[string]CategoryClass{
		CategoryTopLevel:             ClassRoot,
		catMoods:                     ClassFacetRoot,
		CategorySCAKingdom:           ClassFacetRoot,
		CategorySongs:                ClassKind,
		catEndangered:                ClassConservationStatus,
		wren:                         ClassBardName,
		rowan:                        ClassBardName,
		kingdom:                      ClassSCAKingdom,
		"Ansteorra":                  ClassSCAKingdom,
		"Singers":                    ClassBardPerformerType,
		CategorySCABards:             ClassBardAffiliation,
		catPatrioticKingdom:          ClassFacetTerm,
		catTrueEvents:                ClassFacetTerm,
		catKingdomName:               ClassTemplatePlaceholder,
		"In Memoriam":                ClassBardStatus,
		CategoryVenue:                ClassEntityType,
		catTraditionalRedirect:       ClassRedirect,
		groupBarony:                  ClassBardAffiliationGroup,
		catHope:                      ClassFacetTermUnparented,
		catMixed:                     ClassUnclassified,
		"Not a category of the wiki": ClassUnclassified,
	} {
		assert.Equal(t, want, tax.Class(name), name)
	}
	assert.Equal(t, []string{patriotic, catTopics, CategoryTopLevel}, tax.Nodes[catPatrioticKingdom].Ancestors)
	assert.Equal(t, []string{catTopics}, tax.Nodes[catPatrioticKingdom].Facets)
	assert.Equal(t, []string{catTrueEvents}, tax.Nodes[catTrueEvents].Facets)
	assert.Equal(t, catTraditional, tax.Nodes[catTraditionalRedirect].RedirectTo)
	assert.Equal(t, catTraditional, tax.Resolve(catTraditionalRedirect))
	assert.Equal(t, catWarlike, tax.Resolve(catWarlike))
}

func TestClassifyCategories_BardNameNeedsNoFacet(t *testing.T) {
	w := standardWiki()
	w.category(wren, catTopics)
	tax := ClassifyCategories(w.dump())

	assert.Equal(t, ClassFacetTerm, tax.Class(wren), "a category under a facet is a term even if a bard has the name")
}

func termCategories(plan TermPlan) map[string]TermSpec {
	out := map[string]TermSpec{}
	for _, t := range plan.Terms {
		out[t.Category] = t
	}

	return out
}

func TestTermPlan_FacetsAndParents(t *testing.T) {
	plan := PlanTerms(ClassifyCategories(standardWiki().dump()))
	terms := termCategories(plan)

	for category, want := range map[string]struct {
		facet  domain.Facet
		parent string
	}{
		catWarlike:             {domain.FacetMood, ""},
		catPatrioticKingdom:    {domain.FacetTopic, patriotic},
		cat13thCentury:         {domain.FacetOrigin, "Pre-1600 Sources"},
		"10th Century (Topic)": {domain.FacetTopic, ""},
		catTrueEvents:          {domain.FacetOther, ""},
		catWelsh:               {domain.FacetCulture, ""},
		CategorySCABards:       {domain.FacetAffiliation, ""},
		kingdom:                {domain.FacetAffiliation, CategorySCABards},
	} {
		require.Contains(t, terms, category)
		assert.Equal(t, want.facet, terms[category].Facet, category)
		assert.Equal(t, want.parent, terms[category].Parent, category)
	}
	for _, notTerm := range []string{CategorySongs, catEndangered, wren, CategoryTopLevel, catMoods,
		CategorySCAKingdom, catTraditionalRedirect} {
		assert.NotContains(t, terms, notTerm)
	}
	assert.Equal(t, "SCA", terms[CategorySCABards].Name)
	// Parents come before their children, and siblings are numbered in wiki order.
	seen := map[string]bool{}
	for _, term := range plan.Terms {
		if term.Parent != "" {
			assert.True(t, seen[term.Parent], term.Category)
		}
		seen[term.Category] = true
	}
	assert.Equal(t, 0, terms[catWarlike].Position)
	assert.Equal(t, 1, terms[catComedic].Position)
}

func TestTermPlan_UnparentedParents(t *testing.T) {
	w := standardWiki()
	for _, name := range []string{"Seasons", "Fire", catHope, "Archery", "Short Pieces", catJewish, catSephardic,
		"Portuguese", "Unruled"} {
		w.page(Page{Title: "Piece in " + name, Categories: []string{CategoryPieces, name}})
	}
	plan := PlanTerms(ClassifyCategories(w.dump()))
	terms := termCategories(plan)

	for category, want := range map[string]struct {
		facet  domain.Facet
		parent string
	}{
		"Seasons":      {domain.FacetTopic, ""},
		"Fire":         {domain.FacetTopic, ""},
		catHope:        {domain.FacetTopic, ""},
		"Archery":      {domain.FacetTopic, catWar},
		"Short Pieces": {domain.FacetOther, ""},
		catJewish:      {domain.FacetCulture, ""},
		catSephardic:   {domain.FacetCulture, catJewish},
		"Portuguese":   {domain.FacetCulture, ""},
	} {
		require.Contains(t, terms, category)
		assert.Equal(t, want.facet, terms[category].Facet, category)
		assert.Equal(t, want.parent, terms[category].Parent, category)
	}
	assert.NotContains(t, terms, "Unruled")
}

func TestTermPlan_DisplayNameDropsSuffix(t *testing.T) {
	w := standardWiki()
	w.category("Story (Mood)", catMoods)
	w.category("Welsh (Language)", "Cultures")
	w.category("Religion (Norse)", catTopics)
	terms := termCategories(PlanTerms(ClassifyCategories(w.dump())))

	for category, want := range map[string]string{
		"10th Century (Topic)": "10th Century",
		"Story (Mood)":         "Story",
		"Welsh (Language)":     catWelsh,
		"Religion (Norse)":     "Religion (Norse)",
		catPatrioticKingdom:    catPatrioticKingdom,
	} {
		assert.Equal(t, category, terms[category].Name)
		assert.Equal(t, want, terms[category].DisplayName, category)
	}
}

func TestTermPlan_PatrioticLinksKingdom(t *testing.T) {
	plan := PlanTerms(ClassifyCategories(standardWiki().dump()))
	terms := termCategories(plan)

	assert.Equal(t, kingdom, terms[catPatrioticKingdom].RelatedAffiliation)
	assert.Equal(t, domain.FacetAffiliation, terms[kingdom].Facet)
	assert.Empty(t, terms[catPatrioticWales].RelatedAffiliation)
}

func TestTermPlan_GroupsUnderTheirBardsKingdom(t *testing.T) {
	w := standardWiki()
	w.page(Page{Title: ash, Categories: []string{CategoryBards, kingdom, groupBarony}})
	w.page(Page{Title: rowan, Categories: []string{CategoryBards, "Placeholder Canton"}})
	plan := PlanTerms(ClassifyCategories(w.dump()))
	terms := termCategories(plan)

	assert.Equal(t, kingdom, terms[groupBarony].Parent)
	assert.Equal(t, CategorySCABards, terms["Placeholder Canton"].Parent)
}

func TestTermPlan_PlaceholderDropped(t *testing.T) {
	w := standardWiki()
	w.page(Page{Title: ash, Categories: []string{CategoryBards, catKingdomName}})
	plan := PlanTerms(ClassifyCategories(w.dump()))

	assert.NotContains(t, termCategories(plan), catKingdomName)
}

func TestTermPlan_DescriptionIsMarkdownWithoutTagsOrLyrics(t *testing.T) {
	w := standardWiki()
	w.describe(catWarlike, "Songs for '''battle'''. See [[:Category:War|War]].\n<poem>\nscrubbedlyric one\n"+
		"scrubbedlyric two\n</poem>\n[[Category:Moods]]")
	terms := termCategories(PlanTerms(ClassifyCategories(w.dump())))

	assert.Equal(t, "Songs for **battle**. See War.", terms[catWarlike].DescriptionMD)
}

func TestTermPlan_DepthAtMostThree(t *testing.T) {
	w := standardWiki()
	w.category("Level 1", catTopics)
	w.category("Level 2", "Level 1")
	w.category("Level 3", "Level 2")
	w.category("Level 4", "Level 3")
	w.category("Level 5", "Level 4")
	plan := PlanTerms(ClassifyCategories(w.dump()))
	terms := termCategories(plan)

	assert.Equal(t, "Level 3", terms["Level 4"].Parent)
	assert.Equal(t, "Level 3", terms["Level 5"].Parent)
	assertClosure(t, plan)
	assertClosure(t, scrubbedSite(t).Terms)
}

// assertClosure checks the plan's tree is one domain.TermClosure accepts: known parents, no cycle, not too deep.
func assertClosure(t *testing.T, plan TermPlan) {
	t.Helper()
	ids := map[string]int64{}
	for k, term := range plan.Terms {
		ids[term.Category] = int64(k + 1)
	}
	parents := map[int64]*int64{}
	for _, term := range plan.Terms {
		var parent *int64
		if term.Parent != "" {
			id, ok := ids[term.Parent]
			require.True(t, ok, term.Category)
			parent = &id
		}
		parents[ids[term.Category]] = parent
	}
	_, err := domain.TermClosure(parents)
	require.NoError(t, err)
}

func scrubbedSite(t *testing.T) Site {
	t.Helper()
	d, err := LoadDump(wikidumptest.Scrubbed(t))
	require.NoError(t, err)

	return NewSite(d)
}

func TestTermPlan_ScrubbedCopy(t *testing.T) {
	plan := scrubbedSite(t).Terms
	terms := termCategories(plan)
	counts := map[domain.Facet]int{}
	for _, term := range plan.Terms {
		counts[term.Facet]++
	}

	// 103 facet terms and 8 unparented ones; SCA, 14 kingdoms and 3 local groups.
	assert.Equal(t, 111, len(plan.Terms)-counts[domain.FacetAffiliation])
	assert.Equal(t, 18, counts[domain.FacetAffiliation])
	assert.Equal(t, catJewish, terms[catSephardic].Parent)
	assert.Equal(t, catWar, terms["Archery"].Parent)
	assert.Equal(t, "Pre-1600 Sources", terms[cat13thCentury].Parent)
	assert.Equal(t, domain.FacetOrigin, terms[cat13thCentury].Facet)
	assert.Equal(t, domain.FacetTopic, terms["10th Century (Topic)"].Facet)
	assert.Equal(t, kingdom, terms[catPatrioticKingdom].RelatedAffiliation)
	assert.NotContains(t, terms, catKingdomName)
}
