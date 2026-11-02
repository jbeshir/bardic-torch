// Package importer builds the catalogue import from a parsed wiki dump (Build) and the review the moderators read
// before and after applying it (RenderReview). It is pure: it reads values and returns new ones, and writes
// nothing.
package importer

import (
	"fmt"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// Item is one entry of the import review: its stable ID (a section prefix and a number, "ALIAS-007"), what kind
// of entry it is within its section, the wiki page it is about (empty for the wiki as a whole) and what the
// importer found and did. Text never holds lyrics; it may name hidden aliases, as the review is private.
type Item struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Page string `json:"page"`
	Text string `json:"text"`
}

// Counts are what the import holds, for the review's introduction, with two counts of what it was read from: the
// entries of the Collections page and the pieces that had inline lyrics.
type Counts struct {
	PiecesByKind       map[string]int
	StubPieces         int
	InlineLyricsPieces int
	CollectionEntries  int
	Persons            int
	Bards              int
	StubPersons        int
	Terms              int
	Collections        int
	Events             int
	Links              int
	HiddenLinks        int
	Revisions          int
}

// Review is the import review: the counts and one list of items per section, in RenderReview's order.
type Review struct {
	Counts        Counts
	Ambiguities   []Item
	Aliases       []Item
	HiddenNames   []Item
	Contacts      []Item
	PersonMatches []Item
	Credits       []Item
	RedLinks      []Item
	Titles        []Item
	Drift         []Item
	Lyrics        []Item
	Links         []Item
	Performances  []Item
	Persons       []Item
	Taxonomy      []Item
	Collections   []Item
	Licensing     []Item
	Markup        []Item
	Contributors  []Item
}

// reviewSection is one section of the review: its heading, the prefix of its item IDs, what it lists and its
// items.
type reviewSection struct {
	title  string
	prefix string
	about  string
	items  *[]Item
}

// sections are the review's sections, in order.
func (r *Review) sections() []reviewSection {
	return []reviewSection{
		{"Ambiguous records", "AMBIG", "Records the wiki leaves ambiguous (PLAN §2.9): the importer's guess, " +
			"and the alternative.", &r.Ambiguities},
		{"Alias classifications", "ALIAS", "Every alias of every person, with its kind, whether it is public " +
			"and the rule that decided it. Unsure classifications come first; they are hidden until reviewed.",
			&r.Aliases},
		{"Hidden names withheld from public text", "NAME", "Every place a hidden alias or another possible " +
			"legal name was found in public text and replaced with \"" + domain.WithheldName + "\"; every link " +
			"imported hidden because its URL holds a legal name, and every other URL that may name a person; " +
			"and every collection, event, stub person, stub piece, alias or uploader left out because its name " +
			"holds a legal name. The context shows the text after withholding.", &r.HiddenNames},
		{"Contact details withheld", "EMAIL", "Email addresses replaced with \"" + domain.WithheldEmail + "\". " +
			"Restore any the person wants published.", &r.Contacts},
		{"Fuzzy-matched persons", "MATCH", "Names matched to a person otherwise than exactly, with the " +
			"candidates.", &r.PersonMatches},
		{"Credit signals", "CREDIT", "Where the infobox credits, the bard-name categories and the bards' " +
			"works lists disagree, and credits the importer completed or could not read.", &r.Credits},
		{"Red links and stubs", "STUB", "Pieces and persons the wiki only names, imported as stubs, and the " +
			"red links that were not imported.", &r.RedLinks},
		{"Alternate titles", "TITLE", "Titles imported as aliases, titles rejected (with the reason) and " +
			"prose naming other titles that was not parsed.", &r.Titles},
		{"Facet drift", "DRIFT", "Where a piece's infobox and its categories disagree. Facets come from the " +
			"categories.", &r.Drift},
		{"Inline lyrics dropped", "LYRICS", "Lyrics written on the wiki are never imported: the page, section " +
			"and line count of each.", &r.Lyrics},
		{"Links", "LINK", "Links dropped, merged, matched to collections, normalised or given another " +
			"document's fields.", &r.Links},
		{"Performances", "PERF", "Performance items not fully read, events created and similar event names.",
			&r.Performances},
		{"Persons", "PERSON", "Statuses, roles, affiliations and relations inferred, and the kind guessed for " +
			"each stub person.", &r.Persons},
		{"Taxonomy decisions", "TERM", "How the categories became terms where the wiki does not say.",
			&r.Taxonomy},
		{"Collections", "COLL", "Kinds guessed, nesting, merged entries, resources that are not bardic and " +
			"labels that may name a person.", &r.Collections},
		{"Licensing statements", "LICENCE", "Statements the importer did not turn into a licence: every " +
			"licence needs a source.", &r.Licensing},
		{"Markup and text problems", "MARKUP", "Broken markup repaired or found, and names with characters " +
			"an editor could not enter.", &r.Markup},
		{"Credited wiki contributors", "USER", "The wiki user names credited in revision history, flagged when " +
			"one matches a private name or is shaped like a personal name. Check that none needs hiding.",
			&r.Contributors},
	}
}

// numberItems gives every item its ID: its section's prefix and its position in the section.
func (r *Review) numberItems() {
	for _, s := range r.sections() {
		for k := range *s.items {
			(*s.items)[k].ID = fmt.Sprintf("%s-%03d", s.prefix, k+1)
		}
	}
}

// Items returns every item of the review, in section order.
func (r *Review) Items() []Item {
	var out []Item
	for _, s := range r.sections() {
		out = append(out, *s.items...)
	}

	return out
}

// ItemsAbout returns the items about the page titled page, in section order.
func (r *Review) ItemsAbout(page string) []Item {
	var out []Item
	for _, it := range r.Items() {
		if it.Page == page {
			out = append(out, it)
		}
	}

	return out
}

// item is a review item of kind about page, its text formatted.
func item(kind, page, format string, args ...any) Item {
	return Item{Kind: kind, Page: page, Text: oneLine(fmt.Sprintf(format, args...))}
}

// oneLine collapses the white space of s, so that an item is one line of the report.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
