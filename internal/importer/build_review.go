package importer

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// possessiveLabel is a collection label that starts with what may be a person's name in the possessive
// ("First Last's Songbook").
var possessiveLabel = regexp.MustCompile(`^\p{Lu}[\p{L}.'’-]*(?: \p{Lu}[\p{L}.'’-]*){1,3}['’]s\b`)

// reviewCorpus adds the review items that come from the corpus rather than from building one entity.
func (b *builder) reviewCorpus() {
	for _, a := range b.aliasItems {
		b.review.Aliases = append(b.review.Aliases, a.unsure...)
	}
	for _, a := range b.aliasItems {
		b.review.Aliases = append(b.review.Aliases, a.sure...)
	}
	b.reviewAmbiguities()
	b.reviewMatches()
	b.reviewCredits()
	b.reviewRedLinks()
	for _, p := range b.c.Pieces {
		b.reviewPiece(p)
	}
	for _, bard := range b.c.People.Bards {
		b.reviewBard(bard)
	}
	b.reviewLinks()
	b.reviewPerformances()
	b.reviewTaxonomy()
	b.reviewCollections()
	b.reviewContributors()
}

func (b *builder) reviewAmbiguities() {
	for _, p := range b.c.Pieces {
		for _, r := range p.Titles.Rejected {
			if r.Reason == mediawiki.TitleIsOtherPage {
				b.review.Ambiguities = append(b.review.Ambiguities, item("other_page_title", p.Title,
					"The %s gives the title %q, which is another page's. Guess: the page title %q; the "+
						"alternative: %q.", r.Source, r.Text, p.Title, r.Text))
			}
		}
	}
	for _, a := range b.c.People.Ambiguities() {
		b.review.Ambiguities = append(b.review.Ambiguities, item(string(a.Kind), a.Page,
			"%q: guess %q; alternatives: %s.", a.Name, a.Guess, quoted(a.Alternatives)))
	}
}

func quoted(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, fmt.Sprintf("%q", v))
	}

	return strings.Join(out, ", ")
}

func (b *builder) reviewMatches() {
	for _, m := range b.c.People.Persons.Matches {
		candidates := make([]string, 0, len(m.Match.Candidates))
		for _, c := range m.Match.Candidates {
			candidates = append(candidates, string(c))
		}
		person := string(m.Match.Person)
		if person == "" {
			person = "none (a stub of its own)"
		}
		b.review.PersonMatches = append(b.review.PersonMatches, item(string(m.Match.Kind), m.Mention.Page,
			"%q (%s) matched %s via %s (rule %s); candidates: %s.", m.Mention.Name.Name, m.Mention.Name.Form,
			person, valueOr(string(m.Match.Via), "none"), valueOr(string(m.Match.Rule), "exact"), quoted(candidates)))
	}
}

func valueOr(s, otherwise string) string {
	if s == "" {
		return otherwise
	}

	return s
}

func (b *builder) reviewCredits() {
	for _, d := range b.c.People.Disagreements {
		b.review.Credits = append(b.review.Credits, item(string(d.Kind), d.Piece, "%s: %q (person %s).", d.Kind,
			d.Name, valueOr(string(d.Person), "none")))
	}
	for _, p := range b.c.Pieces {
		for _, c := range b.c.People.Credits[p.PageID] {
			if c.PartInferred && c.Part != nil {
				b.review.Credits = append(b.review.Credits, item("part_inferred", p.Title,
					"The %s credit of %q is scoped to the %s, inferred from the piece's origins.", c.Role,
					valueOr(c.Name.Name, string(c.Attribution)), *c.Part))
			}
			for _, q := range c.UnusedQualifiers {
				b.review.Credits = append(b.review.Credits, item("unused_qualifier", p.Title,
					"A qualifier of the %s credit was not imported, as it may name someone: %q.", c.Role, q))
			}
		}
	}
}

func (b *builder) reviewRedLinks() {
	for _, sp := range b.c.People.StubPieces {
		persons := make([]string, 0, len(sp.Credits))
		for _, c := range sp.Credits {
			persons = append(persons, string(c.Person))
		}
		b.review.RedLinks = append(b.review.RedLinks, item("stub_piece", firstOf(persons),
			"Stub piece %q, credited (lyrics, possible) to %s.", sp.Title, strings.Join(persons, ", ")))
	}
	for _, s := range b.stubs {
		page := ""
		if len(s.Mentions) > 0 {
			page = s.Mentions[0].Page
		}
		b.review.RedLinks = append(b.review.RedLinks, item("stub_person", page, "Stub person %q (%s), named on "+
			"%d page(s).", s.Name, s.Kind, len(s.Mentions)))
	}
	for _, r := range b.c.People.RedLinks {
		switch r.Class {
		case mediawiki.RedLinkUnclassified, mediawiki.RedLinkNotImported, mediawiki.RedLinkMalformedURL:
			b.review.RedLinks = append(b.review.RedLinks, item(string(r.Class), firstOf(r.Pages),
				"Red link %q (namespace %d, %s), from %s: not imported.", r.Target, r.NS, r.Class,
				strings.Join(r.Pages, ", ")))
		case mediawiki.RedLinkWork, mediawiki.RedLinkCredit, mediawiki.RedLinkPerson, mediawiki.RedLinkPerformer,
			mediawiki.RedLinkCategory:
		}
	}
}

func firstOf(values []string) string {
	if len(values) == 0 {
		return ""
	}

	return values[0]
}

func (b *builder) reviewPiece(p mediawiki.PieceRecord) {
	for _, a := range p.Titles.Alternates {
		b.review.Titles = append(b.review.Titles, item("alternate", p.Title, "Alias %q (%s, from the %s).", a.Text,
			a.Kind, a.Source))
	}
	for _, r := range p.Titles.Rejected {
		b.review.Titles = append(b.review.Titles, item("rejected", p.Title, "Not imported: %q (from the %s): %s.",
			r.Text, r.Source, r.Reason))
	}
	for _, u := range p.Titles.Unparsed {
		b.review.Titles = append(b.review.Titles, item("unparsed", p.Title, "Prose naming other titles, kept in "+
			"the notes: %q.", u.Text))
	}
	for _, d := range p.Drift {
		b.review.Drift = append(b.review.Drift, item(string(d.Kind), p.Title, "%s: %q (read as %q).", d.Kind,
			d.Category, d.ResolvesTo))
	}
	if len(p.ConservationStatuses) > 1 {
		b.review.Drift = append(b.review.Drift, item("conservation_statuses", p.Title, "Several conservation "+
			"statuses (%s): the most severe, %s, is imported.", strings.Join(stringsOf(p.ConservationStatuses), ", "),
			p.Conservation()))
	}
	if len(p.IgnoredCategories) > 0 {
		b.review.Drift = append(b.review.Drift, item("ignored_categories", p.Title, "Categories that give the "+
			"piece nothing: %s.", strings.Join(p.IgnoredCategories, ", ")))
	}
	for _, s := range p.Lyrics.Sections {
		b.review.Lyrics = append(b.review.Lyrics, item("dropped", p.Title, "Section %q: %d line(s) of lyrics "+
			"dropped; licensing note kept: %s.", valueOr(s.Heading, "lead"), s.Lines,
			yesNo(p.Content.LicensingNotesMD != "")))
	}
	if p.Content.SaysPublicDomain {
		b.review.Licensing = append(b.review.Licensing, item("says_public_domain", p.Title, "The licensing text "+
			"says \"public domain\"; no licence is set without a source."))
	}
	if len(p.Content.PeriodCategories) > 0 {
		b.review.Licensing = append(b.review.Licensing, item("period", p.Title, "Period %s–%s from the century "+
			"origins %s.", int16Text(p.Content.PeriodStart), int16Text(p.Content.PeriodEnd),
			strings.Join(p.Content.PeriodCategories, ", ")))
	}
	b.reviewMarkup(p.Title, p.MarkupIssues)
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}

	return "no"
}

func int16Text(v *int16) string {
	if v == nil {
		return "?"
	}

	return fmt.Sprint(*v)
}

func (b *builder) reviewMarkup(page string, issues []mediawiki.MarkupIssue) {
	for _, m := range issues {
		b.review.Markup = append(b.review.Markup, item(string(m.Kind), page, "%s at byte %d.", m.Kind, m.Offset))
	}
}

func (b *builder) reviewBard(bard mediawiki.BardRecord) {
	b.reviewMarkup(bard.Title, bard.MarkupIssues)
	for _, h := range bard.StrayHeadings {
		b.review.Markup = append(b.review.Markup, item("stray_heading", bard.Title, "A section headed with the "+
			"page title (%q) is imported into the notes.", h))
	}
	if len(bard.DroppedCategories) > 0 {
		b.review.Taxonomy = append(b.review.Taxonomy, item("dropped_categories", bard.Title, "Categories not "+
			"imported: %s.", strings.Join(bard.DroppedCategories, ", ")))
	}
}

func (b *builder) reviewLinks() {
	for _, pl := range b.c.Links.Pieces {
		b.reviewPageLinks(pl.Title, pl.PageLinks)
	}
	for _, pl := range b.c.Links.Persons {
		b.reviewPageLinks(string(pl.Person), pl.PageLinks)
	}
	for _, c := range b.c.Links.Conflicts {
		owner := ownerName(c.Owner, b.c.Links)
		b.review.Links = append(b.review.Links, item("conflict", owner, "%s: its %s differ from those %s gives; "+
			"the import uses %s's.", c.NormalizedURL, strings.Join(c.Fields, ", "),
			ownerName(c.CanonicalOwner, b.c.Links), ownerName(c.CanonicalOwner, b.c.Links)))
	}
}

func ownerName(o mediawiki.LinkOwner, l mediawiki.Links) string {
	if o.Person != "" {
		return string(o.Person)
	}
	for _, pl := range l.Pieces {
		if pl.PageID == o.PieceID {
			return pl.Title
		}
	}

	return fmt.Sprint(o.PieceID)
}

func (b *builder) reviewPageLinks(page string, pl mediawiki.PageLinks) {
	for _, d := range pl.Dropped {
		b.review.Links = append(b.review.Links, item("dropped", page, "%q (section %s) does not normalise: "+
			"not imported.", d.URL, d.Section))
	}
	changed := 0
	for _, o := range pl.Occurrences {
		if u, err := domain.NormalizeURL(o.URL); err == nil && u != o.URL {
			changed++
		}
	}
	if changed > 0 {
		b.review.Links = append(b.review.Links, item("normalised", page, "%d URL(s) as written differ from "+
			"their normalised form (tracking parameters, case, www).", changed))
	}
	if repeated := pl.RepeatedURLs(); len(repeated) > 0 {
		b.review.Links = append(b.review.Links, item("repeated", page, "URLs written more than once, imported "+
			"once each: %s.", strings.Join(repeated, ", ")))
	}
	for _, l := range pl.Links {
		if len(l.Roles) > 1 {
			b.review.Links = append(b.review.Links, item("roles", page, "%s has several roles: %s.", l.URL,
				strings.Join(stringsOf(l.Roles), ", ")))
		}
		if len(l.CollectionTied) > 0 {
			b.review.Links = append(b.review.Links, item("collection_tie", page, "%s could belong to several "+
				"collections (%s): it belongs to none.", l.URL, strings.Join(l.CollectionTied, ", ")))
		}
		if l.CollectionURL != "" && l.Locator != "" {
			b.review.Links = append(b.review.Links, item("collection", page, "%s is in the collection %s at %q.",
				l.URL, l.CollectionURL, l.Locator))
		}
	}
}

func (b *builder) reviewPerformances() {
	for _, p := range b.c.Links.Unlinked {
		b.review.Performances = append(b.review.Performances, item("unlinked", p.Page, "A performance item has "+
			"no link to record it: not imported."))
	}
	for _, p := range b.c.Links.RepeatedPerformances {
		b.review.Performances = append(b.review.Performances, item("repeated", p.Page, "A performance item the "+
			"piece page %q already records on the same link: imported once, from the piece page.", p.Piece))
	}
	for _, e := range b.out.Events {
		b.review.Performances = append(b.review.Performances, item("event", "", "Event %q (%s) created.",
			e.Doc.Name, e.Doc.Kind))
	}
	for _, s := range b.c.Links.SimilarEvents {
		b.review.Performances = append(b.review.Performances, item("similar_events", "", "Events %q and %q have "+
			"similar names; they are not merged.", b.w.mask(s.A), b.w.mask(s.B)))
	}
}

func (b *builder) reviewTaxonomy() {
	for _, d := range b.c.Site.Terms.Decisions {
		b.review.Taxonomy = append(b.review.Taxonomy, item(string(d.Kind), "", "Category %q: %s%s.", d.Category,
			d.Kind, relatedText(d.Related)))
	}
}

func relatedText(related string) string {
	if related == "" {
		return ""
	}

	return fmt.Sprintf(" (%q)", related)
}

func (b *builder) reviewCollections() {
	page := mediawiki.CollectionsPage
	for _, col := range b.out.Collections {
		b.review.Collections = append(b.review.Collections, item("kind", page, "%q: kind %s (guessed).",
			col.Doc.Name, col.Doc.Kind))
	}
	for _, r := range b.c.Links.Collections.Records {
		if r.ParentURL != "" {
			b.review.Collections = append(b.review.Collections, item("nested", page, "%s is nested under %s.",
				r.NormalizedURL, r.ParentURL))
		}
		if !r.IsBardicResource {
			b.review.Collections = append(b.review.Collections, item("not_bardic", page, "%s is noted as not a "+
				"bardic community resource.", r.NormalizedURL))
		}
		if len(r.RegionCandidates) > 0 {
			b.review.Collections = append(b.review.Collections, item("regions", page, "%s names several kingdoms "+
				"(%s): no region is set.", r.NormalizedURL, strings.Join(r.RegionCandidates, ", ")))
		}
		if possessiveLabel.MatchString(r.Label) {
			b.review.Collections = append(b.review.Collections, item("possessive_label", page, "The label of %s "+
				"may name a person (%q): check it may be public.", r.NormalizedURL, b.w.mask(r.Label)))
		}
	}
	for _, r := range b.c.Links.Collections.Duplicates {
		b.review.Collections = append(b.review.Collections, item("duplicate", page, "A second entry for %s is "+
			"merged into the first.", r.NormalizedURL))
	}
	for _, r := range b.c.Links.Collections.Dropped {
		b.review.Collections = append(b.review.Collections, item("dropped", page, "The entry %q has a URL that "+
			"does not normalise: not imported.", r.URL))
	}
}

func (b *builder) reviewContributors() {
	edits := map[string]int{}
	for _, a := range b.c.Dump.Activity {
		for u, n := range a.Users {
			edits[u] += n
		}
	}
	users := make([]string, 0, len(edits))
	for u := range edits {
		users = append(users, u)
	}
	sort.Strings(users)
	hidden := map[string]bool{}
	for _, n := range b.w.names {
		if n.Legal {
			hidden[mediawiki.FoldName(n.Text)] = true
		}
	}
	for _, u := range users {
		flag := ""
		if hidden[mediawiki.FoldName(u)] || b.w.holdsName(u) {
			flag = " It matches a hidden alias or possible legal name: consider hiding it."
		}
		b.review.Contributors = append(b.review.Contributors, item("contributor", "", "%q: %d edit(s).%s", u,
			edits[u], flag))
	}
}

func (b *builder) counts() Counts {
	c := Counts{PiecesByKind: map[string]int{}, Terms: len(b.out.Terms), Collections: len(b.out.Collections),
		Events: len(b.out.Events), Links: len(b.out.Links), Persons: len(b.out.Persons)}
	for _, p := range b.out.Pieces {
		if p.Doc.IsStub {
			c.StubPieces++
		} else {
			c.PiecesByKind[string(p.Doc.Kind)]++
		}
	}
	for _, p := range b.out.Persons {
		if p.Doc.IsStub {
			c.StubPersons++
		} else {
			c.Bards++
		}
	}
	for _, l := range b.out.Links {
		if l.IsHidden {
			c.HiddenLinks++
		}
	}
	c.Revisions = int(b.rev)

	return c
}

// mask is s with its private names and emails withheld, for review text quoting public data.
func (w withholder) mask(s string) string {
	out, _ := w.text(s)

	return out
}
