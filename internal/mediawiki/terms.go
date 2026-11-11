package mediawiki

import (
	"maps"
	"regexp"
	"slices"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// topLevelFacets maps the wiki's top-level categories to the facets of their terms. True Events, a single
// "Other" tag the wiki put at the top level, is itself a term of the other facet.
var topLevelFacets = map[string]domain.Facet{
	"Moods": domain.FacetMood, "Topics": domain.FacetTopic, "Cultures": domain.FacetCulture,
	"Languages": domain.FacetLanguage, "Legendaria": domain.FacetLegendaria, "Forms": domain.FacetForm,
	"Source Category": domain.FacetOrigin, "True Events": domain.FacetOther,
}

// facetOrder is the order terms are planned in, facet by facet.
var facetOrder = []domain.Facet{
	domain.FacetMood, domain.FacetTopic, domain.FacetCulture, domain.FacetLanguage, domain.FacetLegendaria,
	domain.FacetForm, domain.FacetOrigin, domain.FacetOther, domain.FacetAffiliation,
}

// unparentedTerm is where PLAN §2.10 puts a category the wiki gave no parent: a facet and a parent term (empty
// for a root of the facet).
type unparentedTerm struct {
	category string
	facet    domain.Facet
	parent   string
}

// unparentedTerms are the PLAN §2.10 parents, in the order they are placed among their siblings.
var unparentedTerms = []unparentedTerm{
	{"Seasons", domain.FacetTopic, ""},
	{"Fire", domain.FacetTopic, ""},
	{"Hope", domain.FacetTopic, ""},
	{"Archery", domain.FacetTopic, "War"},
	{"Short Pieces", domain.FacetOther, ""},
	{"Jewish", domain.FacetCulture, ""},
	{"Sephardic", domain.FacetCulture, "Jewish"},
	{"Portuguese", domain.FacetCulture, ""},
}

// affiliationRootNames are the names of the organisations at the root of the affiliation tree, by category.
var affiliationRootNames = map[string]string{CategorySCABards: "SCA", CategoryAmtgard: CategoryAmtgard}

var (
	// disambiguationSuffix is the suffix the wiki adds to a category name that another facet also uses.
	disambiguationSuffix = regexp.MustCompile(`[` + jsSpaces + `]+\((?:Topic|Language|Mood)\)$`)
	patrioticTopic       = regexp.MustCompile(`^Patriotic \((.+)\)$`)
	categoryTag          = regexp.MustCompile(`(?i)\[\[[` + jsSpaces + `]*Category:[^\]]*\]\]`)
)

// TermSpec is a term to import, planned from a category. Category is the category it comes from, which identifies
// the term in the plan; Parent and RelatedAffiliation name other terms by their category (empty for none).
// Position is the term's place among its siblings, in wiki order. Aliases are the redirect categories that lead to
// it (and, for an organisation, its category's name). DescriptionMD is the category page's description as
// Markdown, without category tags and inline lyrics, or empty.
type TermSpec struct {
	Category           string
	Facet              domain.Facet
	Name               string
	DisplayName        string
	Parent             string
	RelatedAffiliation string
	DescriptionMD      string
	Position           int
	Aliases            []string
}

// TermPlan is the terms to import, roots before their children.
type TermPlan struct {
	Terms []TermSpec
}

// Term returns the planned term of a category.
func (p TermPlan) Term(category string) (TermSpec, bool) {
	for _, t := range p.Terms {
		if t.Category == category {
			return t, true
		}
	}

	return TermSpec{}, false
}

// PlanTerms plans the terms of the taxonomy (PLAN §2.2, §2.10, §2.12, §4.1):
//   - every facet term, under its wiki parent, its facet taken from its top-level ancestor;
//   - the unparented categories PLAN §2.10 places;
//   - the affiliation tree: each organisation (SCA Bards as SCA, Amtgard), its kingdoms and the local groups,
//     each under the one kingdom its bards share;
//   - a "Patriotic (Kingdom)" topic is related to that kingdom's affiliation term;
//   - redirect categories become aliases of the term they lead to.
//
// Genres (piece kinds), conservation statuses, bard-name categories (credits), performer types, In Memoriam and
// Venue are not terms; placeholders and unclassified categories are dropped. A term's display name is its name
// without a "(Topic)", "(Language)" or "(Mood)" suffix.
func PlanTerms(t Taxonomy) TermPlan {
	p := termPlanner{taxonomy: t, children: map[string][]string{}, specs: map[string]*TermSpec{}}
	p.planFacetTerms()
	p.planAffiliations()
	p.linkPatrioticTopics()
	p.addRedirectAliases()

	return p.result()
}

type termPlanner struct {
	taxonomy Taxonomy
	specs    map[string]*TermSpec
	children map[string][]string // by parent category; facet roots under "facet:<facet>"
}

func (p *termPlanner) add(category string, facet domain.Facet, name, parent string) {
	if _, ok := p.specs[category]; ok {
		return
	}
	node := p.taxonomy.Nodes[category]
	p.specs[category] = &TermSpec{
		Category: category, Facet: facet, Name: name, Parent: parent,
		DisplayName:   disambiguationSuffix.ReplaceAllString(name, ""),
		DescriptionMD: categoryDescription(node.DescriptionWikitext),
	}
	key := parent
	if parent == "" {
		key = "facet:" + string(facet)
	}
	p.children[key] = append(p.children[key], category)
}

func (p *termPlanner) planFacetTerms() {
	// Facet terms in wiki order: each top-level category's subtree, depth first.
	for _, top := range p.taxonomy.TopLevel {
		facet, ok := topLevelFacets[top]
		if !ok {
			continue
		}
		if p.taxonomy.Class(top) == ClassFacetTerm {
			p.add(top, facet, top, "")
		}
		p.addSubtree(top, facet, "")
	}
	p.planUnreached()
	p.planUnparented()
}

// planUnreached adds the facet terms the walk from the top level did not reach, because their parent is no term,
// as roots of their facet.
func (p *termPlanner) planUnreached() {
	for _, name := range sortedCategories(p.taxonomy) {
		node := p.taxonomy.Nodes[name]
		if _, done := p.specs[name]; done || node.Class != ClassFacetTerm {
			continue
		}
		facet, ok := facetOfNode(node)
		if !ok {
			continue
		}
		p.add(name, facet, name, "")
		p.addSubtree(name, facet, name)
	}
}

// planUnparented places the unparented categories by the PLAN §2.10 rules.
func (p *termPlanner) planUnparented() {
	for _, u := range unparentedTerms {
		if p.taxonomy.Class(u.category) != ClassFacetTermUnparented {
			continue
		}
		parent := u.parent
		if _, ok := p.specs[parent]; parent != "" && !ok {
			parent = ""
		}
		p.add(u.category, u.facet, u.category, parent)
	}
}

// addSubtree adds the facet terms below category, which is the term parent or, for a top-level category that is
// not a term, the facet root (parent empty).
func (p *termPlanner) addSubtree(category string, facet domain.Facet, parent string) {
	for _, sub := range p.taxonomy.Nodes[category].Subcategories {
		node, ok := p.taxonomy.Nodes[sub]
		if !ok || node.Class != ClassFacetTerm {
			continue
		}
		if _, done := p.specs[sub]; done {
			continue
		}
		p.add(sub, facet, sub, parent)
		p.addSubtree(sub, facet, sub)
	}
}

func facetOfNode(n CategoryNode) (domain.Facet, bool) {
	for _, f := range n.Facets {
		if facet, ok := topLevelFacets[f]; ok {
			return facet, true
		}
	}

	return "", false
}

func (p *termPlanner) planAffiliations() {
	root := p.planOrganisations()
	if root == "" {
		return
	}
	// Kingdoms in the wiki's order under SCA Kingdom, then the tag-only ones by name.
	kingdoms := append([]string(nil), p.taxonomy.Nodes[CategorySCAKingdom].Subcategories...)
	for _, name := range sortedCategories(p.taxonomy) {
		if !slices.Contains(kingdoms, name) {
			kingdoms = append(kingdoms, name)
		}
	}
	for _, k := range kingdoms {
		if p.taxonomy.Class(k) == ClassSCAKingdom {
			p.add(k, domain.FacetAffiliation, k, root)
		}
	}
	for _, name := range sortedCategories(p.taxonomy) {
		if p.taxonomy.Class(name) != ClassBardAffiliationGroup {
			continue
		}
		parent := p.groupKingdom(name)
		if parent == "" {
			parent = root
		}
		p.add(name, domain.FacetAffiliation, name, parent)
	}
}

// planOrganisations adds the organisations at the root of the affiliation tree and returns the SCA's category,
// or empty if the wiki has none.
func (p *termPlanner) planOrganisations() string {
	var root string
	for _, org := range []string{CategorySCABards, CategoryAmtgard} {
		if p.taxonomy.Class(org) != ClassBardAffiliation {
			continue
		}
		name := affiliationRootNames[org]
		p.add(org, domain.FacetAffiliation, name, "")
		p.specs[org].Aliases = []string{org}
		if org == CategorySCABards {
			root = org
		}
	}

	return root
}

// groupKingdom returns the one kingdom every member of a group is in, or empty.
func (p *termPlanner) groupKingdom(group string) string {
	var shared []string
	members := p.taxonomy.Nodes[group].Pages
	for k, m := range members {
		var in []string
		for _, name := range sortedCategories(p.taxonomy) {
			if p.taxonomy.Class(name) == ClassSCAKingdom && slices.ContainsFunc(p.taxonomy.Nodes[name].Pages,
				func(c CategoryMember) bool { return c.Title == m.Title }) {
				in = append(in, name)
			}
		}
		if k == 0 {
			shared = in
		} else {
			shared = slices.DeleteFunc(shared, func(s string) bool { return !slices.Contains(in, s) })
		}
	}
	if len(shared) != 1 {
		return ""
	}

	return shared[0]
}

func (p *termPlanner) linkPatrioticTopics() {
	for _, name := range p.children["Patriotic"] {
		m := patrioticTopic.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		if spec, ok := p.specs[m[1]]; ok && spec.Facet == domain.FacetAffiliation {
			p.specs[name].RelatedAffiliation = m[1]
		}
	}
}

func (p *termPlanner) addRedirectAliases() {
	for _, name := range sortedCategories(p.taxonomy) {
		if p.taxonomy.Class(name) != ClassRedirect {
			continue
		}
		target := p.taxonomy.Resolve(name)
		if spec, ok := p.specs[target]; ok {
			spec.Aliases = append(spec.Aliases, name)
		}
	}
}

// result orders the terms facet by facet, each tree depth first in sibling order, moves terms deeper than
// domain.MaxTermDepth up and numbers siblings.
func (p *termPlanner) result() TermPlan {
	var terms []TermSpec
	var walk func(key string, depth int, under string)
	walk = func(key string, depth int, under string) {
		for k, category := range p.children[key] {
			spec := *p.specs[category]
			spec.Position = k
			if depth > domain.MaxTermDepth {
				spec.Parent = under
			}
			terms = append(terms, spec)
			next := under
			if depth < domain.MaxTermDepth {
				next = category
			}
			walk(category, depth+1, next)
		}
	}
	for _, f := range facetOrder {
		walk("facet:"+string(f), 0, "")
	}

	return TermPlan{Terms: terms}
}

// categoryDescription is a category page's description as Markdown, without its category tags and lyrics.
func categoryDescription(wikitext string) string {
	text := WithoutInlineLyrics(wikitext, InlineLyricsBlocks(wikitext).Blocks)

	return WikitextToMarkdown(categoryTag.ReplaceAllString(text, ""))
}

func sortedCategories(t Taxonomy) []string {
	return slices.Sorted(maps.Keys(t.Nodes))
}
