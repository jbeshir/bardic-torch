package mediawiki

import (
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// RedLinkKey is the key of a red link, as the reference's redlinks.json writes it: "<namespace>:<target>".
func RedLinkKey(ns int, target string) string {
	return strconv.Itoa(ns) + ":" + target
}

// RedLinks returns every red link of the main namespace's pages (redirects aside), as the reference's
// redlinks.json does: by RedLinkKey, the titles of the pages that link it, sorted. A red link is a page link, a
// category link or a special page link (category tags and interwiki links aside) to a page the dump does not
// have. Links to the other namespaces (users, templates, files) are not considered: the importer reads none of
// their pages, and the scrubbed copy of the dump does not keep them.
func RedLinks(s Site) map[string][]string {
	out := map[string][]string{}
	for _, p := range s.ordered {
		if p.Redirect {
			continue
		}
		for _, l := range FindWikilinks(MaskNowiki(StripComments(p.Wikitext))) {
			if l.Kind == WikilinkInterwiki || l.Kind == WikilinkCategoryTag || s.exists(l) {
				continue
			}
			if key := RedLinkKey(l.NS, l.Target); !slices.Contains(out[key], p.Title) {
				out[key] = append(out[key], p.Title)
			}
		}
	}
	for _, pages := range out {
		sort.Strings(pages)
	}

	return out
}

// exists reports whether a link's target exists, or whether the link is to a namespace RedLinks does not consider.
func (s Site) exists(l Wikilink) bool {
	switch l.NS {
	case NSMain:
		return s.HasPage(l.Target)
	case NSCategory:
		return s.hasCategoryPage(l.Target)
	case NSSpecial:
		return false
	}

	return true
}

// RedLinkClass is what a red link is for the import.
type RedLinkClass string

// The red link classes, in the order a red link with several uses is classified by.
const (
	// RedLinkWork is a work a bard page lists: a stub piece credited to the bard (StubPiece).
	RedLinkWork RedLinkClass = "work"
	// RedLinkCredit is a name an infobox credits: a stub person, the person the name matches, or a pseudo-bard
	// (Traditional & Folk) that is an attribution.
	RedLinkCredit RedLinkClass = "credit"
	// RedLinkPerson is a person named in prose ("adapted by [[X]]", "songs by [[X]]"): a stub person.
	RedLinkPerson RedLinkClass = "person"
	// RedLinkPerformer is a performer of a performance item, matched with the performances.
	RedLinkPerformer RedLinkClass = "performer"
	// RedLinkCategory is a category with no page; its term, if any, comes from the taxonomy.
	RedLinkCategory RedLinkClass = "category"
	// RedLinkMalformedURL is an external URL written as a page link, repaired to an external link (RepairMarkup).
	RedLinkMalformedURL RedLinkClass = "malformed_url"
	// RedLinkNotImported is a special page, or a link only from pages that are not imported (a meta page's example).
	RedLinkNotImported RedLinkClass = "not_imported"
	// RedLinkUnclassified is any other red link: not imported.
	RedLinkUnclassified RedLinkClass = "unclassified"
)

var redLinkPriority = []RedLinkClass{RedLinkWork, RedLinkCredit, RedLinkPerson, RedLinkPerformer}

// RedLink is one red link: its namespace and target, the pages that link it and its class.
type RedLink struct {
	NS     int
	Target string
	Pages  []string
	Class  RedLinkClass
}

var (
	// malformedURL is a page link target that is an external URL.
	malformedURL = regexp.MustCompile(`(?i)^(?:https?:)?//`)
	// personBefore is the wording before a link to a person in prose: "by" or "of", and perhaps a title ("Don").
	personBefore = regexp.MustCompile(`(?i)\b(?:by|of)(?:[ \t]+\p{Lu}\p{Ll}*\.?)?[ \t]*$`)
)

// ClassifyRedLinks classifies the red links (RedLinks) by how the imported pages use them, the first class of
// redLinkPriority that applies winning; sorted by key.
func ClassifyRedLinks(s Site, pieces []PieceRecord, bards []BardRecord) []RedLink {
	uses := redLinkUses(pieces, bards)
	var out []RedLink
	for key, pages := range RedLinks(s) {
		colon := strings.IndexByte(key, ':')
		ns, _ := strconv.Atoi(key[:colon])
		r := RedLink{NS: ns, Target: key[colon+1:], Pages: pages, Class: RedLinkUnclassified}
		imported := slices.ContainsFunc(pages, func(title string) bool {
			t := TypePage(s.pages[title])

			return t == PageTypePiece || t == PageTypeBard
		})
		switch {
		case ns == NSCategory:
			r.Class = RedLinkCategory
		case malformedURL.MatchString(r.Target):
			r.Class = RedLinkMalformedURL
		case ns == NSSpecial || !imported:
			r.Class = RedLinkNotImported
		default:
			for _, c := range redLinkPriority {
				if uses[c][r.Target] {
					r.Class = c

					break
				}
			}
		}
		out = append(out, r)
	}
	sort.Slice(out, func(a, b int) bool {
		return RedLinkKey(out[a].NS, out[a].Target) < RedLinkKey(out[b].NS, out[b].Target)
	})

	return out
}

// redLinkUses are the page link targets of the imported pages by how they use them: works listed with no page,
// credited names, persons named in prose and performers.
func redLinkUses(pieces []PieceRecord, bards []BardRecord) map[RedLinkClass]map[string]bool {
	uses := map[RedLinkClass]map[string]bool{}
	for _, c := range redLinkPriority {
		uses[c] = map[string]bool{}
	}
	for _, p := range pieces {
		for _, b := range p.Parsed.Infoboxes {
			for _, param := range personParams {
				v, _ := b.Param(param.name)
				for _, it := range v.Value.Items {
					if it.Type == ValuePageLink {
						uses[RedLinkCredit][it.Target] = true
					}
				}
			}
		}
		pageLinkUses(uses, p.Parsed, p.Wikitext)
	}
	for _, b := range bards {
		for _, w := range b.Works {
			if !w.Exists {
				uses[RedLinkWork][w.Title] = true
			}
		}
		pageLinkUses(uses, b.Parsed, b.Wikitext)
	}

	return uses
}

// pageLinkUses adds the performers of a page's performance items and the persons its prose names (outside the
// infoboxes and the works lists).
func pageLinkUses(uses map[RedLinkClass]map[string]bool, parsed ParsedPage, wikitext string) {
	for _, sec := range parsed.Sections {
		switch {
		case sec.Key.IsPerformanceSection():
			for _, item := range sec.Content.Items {
				for _, l := range ParsePerformanceText(item.Raw, MinPerformanceYear).Performers {
					uses[RedLinkPerformer][l.Target] = true
				}
			}
		case sec.Key != SectionWorks && sec.Key != SectionWorksMusic:
			for _, target := range prosePersonLinks(sec.Body(wikitext)) {
				uses[RedLinkPerson][target] = true
			}
		}
	}
}

// prosePersonLinks are the targets of a section body's page links that name persons ("by [[X]]"), outside its
// infobox.
func prosePersonLinks(body string) []string {
	masked := MaskNowiki(StripComments(body))
	if loc := infoboxCall.FindStringIndex(masked); loc != nil {
		masked = masked[:loc[0]] + strings.Repeat(" ", loc[1]-loc[0]) + masked[loc[1]:]
	}
	var out []string
	last := 0
	for _, l := range FindWikilinks(masked) {
		if l.Kind == WikilinkPage && personBefore.MatchString(masked[last:l.Start]) {
			out = append(out, l.Target)
		}
		last = l.End
	}

	return out
}

// personRedLinkMentions are the red links that name persons in prose, as mentions, one per page linking them.
func personRedLinkMentions(redLinks []RedLink) []PersonMention {
	var out []PersonMention
	for _, r := range redLinks {
		if r.Class != RedLinkPerson {
			continue
		}
		for range r.Pages {
			out = append(out, PersonMention{Name: PersonName{Name: r.Target, Form: NameRedLink}})
		}
	}

	return out
}
