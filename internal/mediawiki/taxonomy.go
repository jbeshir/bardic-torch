package mediawiki

import (
	"regexp"
	"slices"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// CategoryClass is what a category of the wiki is for, as the reference's classifyCategory decides it.
type CategoryClass string

// The category classes.
const (
	// ClassRoot is Top Level Categories, the root of the category tree.
	ClassRoot CategoryClass = "root"
	// ClassFacetRoot is a structural category: a facet (Moods, Topics…), Pieces, Bards, Genres, Conservation
	// Status or SCA Kingdom.
	ClassFacetRoot CategoryClass = "facet_root"
	// ClassKind is a genre (Songs, Poems…), which is a piece kind, not a term.
	ClassKind CategoryClass = "kind"
	// ClassConservationStatus is a conservation status category (Endangered…).
	ClassConservationStatus CategoryClass = "conservation_status"
	// ClassBardName is a category named after a credited person, which is a credit signal.
	ClassBardName CategoryClass = "bard_name"
	// ClassSCAKingdom is an SCA kingdom, an affiliation of bards.
	ClassSCAKingdom CategoryClass = "sca_kingdom"
	// ClassBardPerformerType is a bard role (Singers, Poets…).
	ClassBardPerformerType CategoryClass = "bard_performer_type"
	// ClassBardAffiliation is an organisation bards belong to (SCA Bards, Amtgard).
	ClassBardAffiliation CategoryClass = "bard_affiliation"
	// ClassFacetTerm is a term of a facet: a category below one of the top-level categories.
	ClassFacetTerm CategoryClass = "facet_term"
	// ClassTemplatePlaceholder is a placeholder copied from a page skeleton (Kingdom Name…).
	ClassTemplatePlaceholder CategoryClass = "template_placeholder"
	// ClassBardStatus is In Memoriam, a person status.
	ClassBardStatus CategoryClass = "bard_status"
	// ClassEntityType is Venue, whose pages are events.
	ClassEntityType CategoryClass = "entity_type"
	// ClassRedirect is a category whose page redirects to another category.
	ClassRedirect CategoryClass = "redirect"
	// ClassBardAffiliationGroup is a category with no parent whose members are bards only: a local SCA group.
	ClassBardAffiliationGroup CategoryClass = "bard_affiliation_group"
	// ClassFacetTermUnparented is a category with no parent whose members are pieces only: a term whose facet
	// the wiki never gave.
	ClassFacetTermUnparented CategoryClass = "facet_term_unparented"
	// ClassUnclassified is any other category.
	ClassUnclassified CategoryClass = "unclassified"
)

// The structural categories the classification depends on.
const (
	CategoryTopLevel           = "Top Level Categories"
	CategoryGenres             = "Genres"
	CategoryConservationStatus = "Conservation Status"
	CategorySCAKingdom         = "SCA Kingdom"
	CategorySCABards           = "SCA Bards"
	CategoryAmtgard            = "Amtgard"
	CategoryInMemoriam         = "In Memoriam"
)

var (
	// scaKingdoms are the kingdoms the reference names, for the ones the wiki tags without filing them under SCA
	// Kingdom (Ansteorra, Caid). A kingdom filed there needs no entry, which is why the one whose name the spell
	// checker rejects, always filed there, is left out.
	scaKingdoms = map[string]bool{
		"Aethelmearc": true, "An Tir": true, "Ansteorra": true, "Artemisia": true, "Atenveldt": true,
		"Avacal": true, "Caid": true, "Calontir": true, "Drachenwald": true, "Ealdormere": true,
		"East Kingdom": true, "Gleann Abhann": true, "Lochac": true, "Meridies": true, "Midrealm": true,
		"Northshield": true, "Outlands": true, "Trimaris": true, "West Kingdom": true, "Tir Righ": true,
		"Tir Mara": true,
	}
	// performerRoles are the performer-type categories, with the role each gives a bard.
	performerRoles = map[string]domain.PersonRole{
		"Musicians": domain.PersonRoleMusician, "Singers": domain.PersonRoleSinger,
		"Instrumentalists": domain.PersonRoleInstrumentalist, "Songwriters": domain.PersonRoleSongwriter,
		"Storytellers": domain.PersonRoleStoryteller, "Jesters": domain.PersonRoleJester,
		"Poets": domain.PersonRolePoet, "Dancers": domain.PersonRoleDancer,
	}
	bardAffiliations     = map[string]bool{CategorySCABards: true, CategoryAmtgard: true}
	structuralCategories = map[string]bool{
		CategoryTopLevel: true, CategoryPieces: true, CategoryBards: true, "Moods": true, "Topics": true,
		"Cultures": true, "Languages": true, "Legendaria": true, "Source Category": true, CategoryGenres: true,
		"Forms": true, CategoryConservationStatus: true, CategorySCAKingdom: true,
	}
	categoryPlaceholder = regexp.MustCompile(`^(Kingdom Name|BardNameHere|TagGoesHere|TagNameHere)$`)
	categoryRedirect    = regexp.MustCompile(`(?i)#REDIRECT[` + jsSpaces + `]*\[\[:?Category:([^\]|#]+)`)
)

// CategoryNode is one category with its classification. Facets are the top-level categories among the category
// and its ancestors; Ancestors are all categories above it, nearest first. RedirectTo is the category a redirect
// category's page points to, or empty.
type CategoryNode struct {
	Category
	Class      CategoryClass
	Facets     []string
	Ancestors  []string
	RedirectTo string
}

// Taxonomy is every category of the wiki, classified.
type Taxonomy struct {
	Nodes map[string]CategoryNode
	// TopLevel are the subcategories of Top Level Categories, in wiki order.
	TopLevel []string
}

// Class returns the class of the category name, or ClassUnclassified for a category the wiki does not have.
func (t Taxonomy) Class(name string) CategoryClass {
	if n, ok := t.Nodes[name]; ok {
		return n.Class
	}

	return ClassUnclassified
}

// Resolve follows redirect categories from name to the category they lead to. A redirect to a missing category,
// or a cycle, stops at the last category reached.
func (t Taxonomy) Resolve(name string) string {
	seen := map[string]bool{}
	for {
		n, ok := t.Nodes[name]
		if !ok || n.RedirectTo == "" || seen[name] {
			return name
		}
		seen[name] = true
		name = n.RedirectTo
	}
}

// ClassifyCategories classifies every category of d as the reference's classifyCategory does: by redirect, by
// name, by the structural categories among its ancestors, by whether it is named after a credited person (a bard
// page, or a person linked from an infobox person field) and, for a category with no other class, by whether its
// members are bards only or pieces only.
func ClassifyCategories(d Dump) Taxonomy {
	pages := make(map[string]Page, len(d.Pages))
	for _, p := range d.Pages {
		pages[p.Title] = p
	}
	bardNames := creditedNames(d)
	top := d.Categories[CategoryTopLevel].Subcategories
	t := Taxonomy{Nodes: make(map[string]CategoryNode, len(d.Categories)), TopLevel: top}
	for name, c := range d.Categories {
		ancestors := categoryAncestors(d.Categories, name, map[string]bool{})
		node := CategoryNode{Category: c, Ancestors: ancestors, Facets: facetsOf(top, name, ancestors)}
		var isRedirect bool
		node.RedirectTo, isRedirect = categoryPageRedirect(pages, name)
		node.Class = classify(node, isRedirect, bardNames, pages)
		t.Nodes[name] = node
	}

	return t
}

// classify returns the class of a category, trying the reference's rules in its order.
func classify(n CategoryNode, isRedirect bool, bardNames map[string]bool, pages map[string]Page) CategoryClass {
	if class, ok := structuralClass(n, isRedirect); ok {
		return class
	}
	if len(n.Facets) == 0 && bardNames[n.Name] {
		return ClassBardName
	}
	if class, ok := namedClass(n); ok {
		return class
	}

	return memberClass(n, pages)
}

// structuralClass classifies redirects, the root, the structural categories and those below Genres or
// Conservation Status.
func structuralClass(n CategoryNode, isRedirect bool) (CategoryClass, bool) {
	switch {
	case isRedirect:
		return ClassRedirect, true
	case n.Name == CategoryTopLevel:
		return ClassRoot, true
	case structuralCategories[n.Name]:
		return ClassFacetRoot, true
	case slices.Contains(n.Ancestors, CategoryGenres):
		return ClassKind, true
	case slices.Contains(n.Ancestors, CategoryConservationStatus):
		return ClassConservationStatus, true
	}

	return "", false
}

// namedClass classifies kingdoms, the bard categories, facet terms and the categories known by name.
func namedClass(n CategoryNode) (CategoryClass, bool) {
	switch {
	case scaKingdoms[n.Name] || slices.Contains(n.Ancestors, CategorySCAKingdom):
		return ClassSCAKingdom, true
	case performerRoles[n.Name] != "":
		return ClassBardPerformerType, true
	case bardAffiliations[n.Name]:
		return ClassBardAffiliation, true
	case len(n.Facets) > 0:
		return ClassFacetTerm, true
	case categoryPlaceholder.MatchString(n.Name):
		return ClassTemplatePlaceholder, true
	case n.Name == CategoryInMemoriam:
		return ClassBardStatus, true
	case n.Name == CategoryVenue:
		return ClassEntityType, true
	}

	return "", false
}

// memberClass classifies any other category by its members: bards only, pieces only, or neither.
func memberClass(n CategoryNode, pages map[string]Page) CategoryClass {
	var bards, pieces int
	for _, m := range n.Pages {
		member := pages[m.Title]
		switch {
		case member.HasCategory(CategoryBards):
			bards++
		case member.HasCategory(CategoryPieces):
			pieces++
		}
	}
	switch {
	case bards > 0 && pieces == 0:
		return ClassBardAffiliationGroup
	case pieces > 0 && bards == 0:
		return ClassFacetTermUnparented
	}

	return ClassUnclassified
}

// creditedNames are the bard page titles (members of Bards) and the persons infobox person fields link to: page
// link targets and external link labels, on every page of the main namespace that is not a redirect.
func creditedNames(d Dump) map[string]bool {
	names := map[string]bool{}
	for _, m := range d.Categories[CategoryBards].Pages {
		names[m.Title] = true
	}
	for _, p := range d.Pages {
		if p.NS != NSMain || p.Redirect {
			continue
		}
		for _, box := range ParsePage(p.Wikitext).Infoboxes {
			for _, pp := range personParams {
				param, ok := box.Param(pp.name)
				if !ok {
					continue
				}
				for _, it := range param.Value.Items {
					switch {
					case it.Type == ValuePageLink:
						names[it.Target] = true
					case it.Type == ValueExternalLink && it.Label != "":
						names[NormTitle(it.Label)] = true
					}
				}
			}
		}
	}

	return names
}

// categoryAncestors returns the categories above name, depth first and without repeats, as the reference's
// ancestors does: seen is shared across the whole walk.
func categoryAncestors(categories map[string]Category, name string, seen map[string]bool) []string {
	if seen[name] {
		return nil
	}
	seen[name] = true
	c, ok := categories[name]
	if !ok {
		return nil
	}
	var out []string
	for _, p := range c.Parents {
		out = appendUnique(out, p)
		for _, a := range categoryAncestors(categories, p, seen) {
			out = appendUnique(out, a)
		}
	}

	return out
}

func facetsOf(top []string, name string, ancestors []string) []string {
	var out []string
	for _, a := range append([]string{name}, ancestors...) {
		if slices.Contains(top, a) {
			out = appendUnique(out, a)
		}
	}

	return out
}

// categoryPageRedirect reports whether the category's page is a redirect and, if its target is a category, that
// category.
func categoryPageRedirect(pages map[string]Page, name string) (string, bool) {
	p, ok := pages[categoryPrefix+name]
	if !ok || !p.Redirect {
		return "", false
	}
	if m := categoryRedirect.FindStringSubmatch(p.Wikitext); m != nil {
		return NormTitle(m[1]), true
	}

	return "", true
}

func appendUnique(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}

	return append(list, s)
}
