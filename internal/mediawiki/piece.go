package mediawiki

import (
	"slices"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// infoboxKinds are the piece kinds the infobox templates give, by lower-case template name.
var infoboxKinds = map[string]domain.PieceKind{
	"infobox song": domain.PieceKindSong, "infobox poem": domain.PieceKindPoem,
	"infobox story": domain.PieceKindStory, "infobox dance&music": domain.PieceKindDanceMusic,
}

// The genre categories.
const (
	CategorySongs        = "Songs"
	CategoryPoems        = "Poems"
	CategoryStories      = "Stories"
	CategoryDances       = "Dances"
	CategoryInstrumental = "Instrumental"
)

// genreKinds are the piece kinds the genre categories give.
var genreKinds = map[string]domain.PieceKind{
	CategorySongs: domain.PieceKindSong, CategoryPoems: domain.PieceKindPoem, CategoryStories: domain.PieceKindStory,
	CategoryDances: domain.PieceKindDanceMusic, CategoryInstrumental: domain.PieceKindInstrumental,
}

// conservationCategory is a conservation category and the status it gives.
type conservationCategory struct {
	category string
	status   domain.ConservationStatus
}

// conservationStatuses are the statuses the conservation categories give, least severe first.
var conservationStatuses = []conservationCategory{
	{"Low Risk", domain.ConservationStatusLowRisk},
	{"Endangered", domain.ConservationStatusEndangered},
	{"Critically Endangered", domain.ConservationStatusCriticallyEndangered},
	{"Restricted", domain.ConservationStatusRestricted},
}

// PieceRecord is a piece page parsed for import. Wikitext is the page's wikitext without its inline lyrics and
// with its markup repaired, and Parsed is that text parsed; every later step reads these, never the original.
// Terms are the categories of the piece's terms, in order. AlsoInstrumental is a page in Instrumental whose kind
// is neither instrumental nor dance_music.
type PieceRecord struct {
	PageID           int64
	Title            string
	Wikitext         string
	Parsed           ParsedPage
	Kind             domain.PieceKind
	AlsoInstrumental bool
	Terms            []string
	// ConservationStatuses are the statuses of the page's conservation categories, least severe first.
	ConservationStatuses []domain.ConservationStatus
	// BardNameCategories are the page's categories named after a credited person: credit signals.
	BardNameCategories []string
	Titles             PieceTitles
	// Credits are the credits of the infoboxes' person parameters, before the persons they name are matched.
	Credits []CreditRecord
	Content PieceContent
	// HadInlineLyrics is legacy_had_inline_lyrics: DropInlineLyrics dropped some of the page.
	HadInlineLyrics bool
}

// Conservation is the piece's conservation status: the most severe of its categories', or unassessed.
func (p PieceRecord) Conservation() domain.ConservationStatus {
	if n := len(p.ConservationStatuses); n > 0 {
		return p.ConservationStatuses[n-1]
	}

	return domain.ConservationStatusUnassessed
}

// ParsePiece parses a piece page. Its inline lyrics are found on the original wikitext and dropped before
// anything else reads it; facets come from the page's categories, the infobox only ordering and scoping them.
func ParsePiece(p Page, s Site) PieceRecord {
	dropped, hadLyrics := DropInlineLyrics(p.Wikitext)
	repaired := RepairMarkup(dropped)
	parsed := ParsePage(repaired)
	kind := primaryKind(p, parsed.Infoboxes)
	sorted := sortCategories(p, parsed.Infoboxes, s)
	r := PieceRecord{
		PageID: p.PageID, Title: p.Title, Wikitext: repaired, Parsed: parsed,
		Kind: kind, AlsoInstrumental: p.HasCategory(CategoryInstrumental) && kind != domain.PieceKindInstrumental &&
			kind != domain.PieceKindDanceMusic,
		Terms: sorted.terms, ConservationStatuses: sorted.statuses, BardNameCategories: sorted.bardNames,
		Titles:          pieceTitles(p, parsed, s),
		Credits:         pieceCredits(parsed.Infoboxes, s),
		HadInlineLyrics: hadLyrics,
	}
	r.Content = pieceContent(r, s, versionNotes(dropped))

	return r
}

// primaryKind is dance_music for a dance and music infobox or a page in both Dances and Instrumental, otherwise
// the first infobox's kind, otherwise the genre categories' (song, poem or story first), otherwise song.
func primaryKind(p Page, boxes []Infobox) domain.PieceKind {
	var fromBoxes []domain.PieceKind
	for _, b := range boxes {
		if k, ok := infoboxKinds[strings.ToLower(b.Template)]; ok {
			fromBoxes = append(fromBoxes, k)
		}
	}
	if slices.Contains(fromBoxes, domain.PieceKindDanceMusic) ||
		(p.HasCategory(CategoryDances) && p.HasCategory(CategoryInstrumental)) {
		return domain.PieceKindDanceMusic
	}
	if len(fromBoxes) > 0 {
		return fromBoxes[0]
	}
	for _, k := range []domain.PieceKind{domain.PieceKindSong, domain.PieceKindPoem, domain.PieceKindStory} {
		if slices.ContainsFunc(p.Categories, func(c string) bool { return genreKinds[c] == k }) {
			return k
		}
	}
	for _, c := range p.Categories {
		if k, ok := genreKinds[c]; ok {
			return k
		}
	}

	return domain.PieceKindSong
}

// infoboxCategories returns the positions of the categories the infoboxes link to (read through redirect
// categories) among their category links, first position first.
func infoboxCategories(boxes []Infobox, t Taxonomy) map[string]int {
	out := map[string]int{}
	for _, b := range boxes {
		for _, param := range b.Params {
			for _, it := range param.Value.Items {
				if !isCategoryItem(it) {
					continue
				}
				target := t.Resolve(it.Target)
				if _, seen := out[target]; !seen {
					out[target] = len(out)
				}
			}
		}
	}

	return out
}

func isCategoryItem(it ValueItem) bool {
	return it.Type == ValueCategoryLink || it.Type == ValueCategoryTag
}

// sortedPieceCategories are a page's categories sorted by what they give a piece.
type sortedPieceCategories struct {
	terms     []string
	statuses  []domain.ConservationStatus
	bardNames []string
}

// rankedTerm is the category of a term of a piece with its rank: its infobox position, or after every infobox link
// in category order.
type rankedTerm struct {
	category string
	rank     int
}

// sortCategories sorts the page's categories into terms (affiliations, which classify persons, aside),
// conservation statuses (least severe first) and bard-name signals; the rest give nothing. Terms are ordered by the
// infobox and then by the categories.
func sortCategories(p Page, boxes []Infobox, s Site) sortedPieceCategories {
	boxCategories := infoboxCategories(boxes, s.Taxonomy)
	var out sortedPieceCategories
	var terms []rankedTerm
	for k, c := range p.Categories {
		category := s.Taxonomy.Resolve(c)
		if spec, ok := s.Terms.Term(category); ok && spec.Facet != domain.FacetAffiliation {
			if !slices.ContainsFunc(terms, func(t rankedTerm) bool { return t.category == category }) {
				terms = append(terms, rankTerm(category, len(boxCategories)+k, boxCategories))
			}

			continue
		}
		if class := s.Taxonomy.Class(category); class == ClassConservationStatus {
			out.statuses = appendStatus(out.statuses, category)
		} else if class == ClassBardName {
			out.bardNames = append(out.bardNames, c)
		}
	}
	slices.SortStableFunc(terms, func(a, b rankedTerm) int { return a.rank - b.rank })
	slices.SortStableFunc(out.statuses, func(a, b domain.ConservationStatus) int { return severity(a) - severity(b) })
	for _, t := range terms {
		out.terms = append(out.terms, t.category)
	}

	return out
}

func rankTerm(category string, rank int, boxCategories map[string]int) rankedTerm {
	if position, ok := boxCategories[category]; ok {
		rank = position
	}

	return rankedTerm{category: category, rank: rank}
}

func appendStatus(statuses []domain.ConservationStatus, category string) []domain.ConservationStatus {
	for _, cs := range conservationStatuses {
		if cs.category == category && !slices.Contains(statuses, cs.status) {
			statuses = append(statuses, cs.status)
		}
	}

	return statuses
}

func severity(s domain.ConservationStatus) int {
	return slices.IndexFunc(conservationStatuses, func(cs conservationCategory) bool { return cs.status == s })
}
