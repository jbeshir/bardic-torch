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
	CategoryComedy       = "Comedy"
)

// genreKinds are the piece kinds the genre categories give.
var genreKinds = map[string]domain.PieceKind{
	CategorySongs: domain.PieceKindSong, CategoryPoems: domain.PieceKindPoem, CategoryStories: domain.PieceKindStory,
	CategoryDances: domain.PieceKindDanceMusic, CategoryInstrumental: domain.PieceKindInstrumental,
	CategoryComedy: domain.PieceKindComedy,
}

// kindCategories are the genre categories that a primary kind implies, which are not secondary kinds.
var kindCategories = map[domain.PieceKind][]string{
	domain.PieceKindSong:         {CategorySongs},
	domain.PieceKindPoem:         {CategoryPoems},
	domain.PieceKindStory:        {CategoryStories},
	domain.PieceKindDanceMusic:   {CategoryDances, CategoryInstrumental},
	domain.PieceKindInstrumental: {CategoryInstrumental},
	domain.PieceKindComedy:       {CategoryComedy},
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

// partQualifiers are the qualifiers that scope a term or credit to a part of a piece. "(chorus tune)" is the
// chorus's tune, so it scopes to the chorus: a tune credit or an origin so qualified is the chorus's.
var partQualifiers = map[string]domain.PiecePart{
	"words": domain.PiecePartWords, "lyrics": domain.PiecePartWords, "tune": domain.PiecePartTune,
	"chorus tune": domain.PiecePartChorus, "music": domain.PiecePartTune, "verses": domain.PiecePartVerses,
	"verse": domain.PiecePartVerses, "chorus": domain.PiecePartChorus,
}

// PieceTerm tags a piece with the term planned from Category. IsPrimary marks the first term of its facet;
// Part is the part of the piece the term applies to, or nil.
type PieceTerm struct {
	Category  string
	IsPrimary bool
	Part      *domain.PiecePart
}

// PieceRecord is a piece page parsed for import. Wikitext is the page's wikitext without its inline lyrics and
// with its markup repaired, and Parsed is that text parsed; every later step reads these, never the original.
type PieceRecord struct {
	PageID         int64
	Title          string
	URL            string
	Wikitext       string
	Parsed         ParsedPage
	Kind           domain.PieceKind
	SecondaryKinds []domain.PieceKind
	Terms          []PieceTerm
	// ConservationStatuses are the statuses of the page's conservation categories, least severe first.
	ConservationStatuses []domain.ConservationStatus
	// BardNameCategories are the page's categories named after a credited person: credit signals.
	BardNameCategories []string
	Titles             PieceTitles
	// Credits are the credits of the infoboxes' person parameters, before the persons they name are matched.
	Credits []CreditRecord
	Content PieceContent
	Lyrics  DroppedLyrics
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
	lyrics := InlineLyricsBlocks(p.Wikitext)
	repaired := RepairMarkup(WithoutInlineLyrics(p.Wikitext, lyrics.Blocks))
	parsed := ParsePage(repaired)
	kind := primaryKind(p, parsed.Infoboxes)
	sorted := sortCategories(p, parsed.Infoboxes, s)
	r := PieceRecord{
		PageID: p.PageID, Title: p.Title, URL: p.URL, Wikitext: repaired, Parsed: parsed,
		Kind: kind, SecondaryKinds: secondaryKinds(p, kind),
		Terms: sorted.terms, ConservationStatuses: sorted.statuses, BardNameCategories: sorted.bardNames,
		Titles:  pieceTitles(p, parsed, s),
		Credits: pieceCredits(parsed.Infoboxes, sorted.terms, s),
		Lyrics:  DroppedLyrics{HadInlineLyrics: lyrics.HadInlineLyrics},
	}
	r.Content = pieceContent(r, s, versionNotes(p.Wikitext, lyrics))

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

// secondaryKinds are the kinds of the page's genre categories other than those its primary kind implies.
func secondaryKinds(p Page, kind domain.PieceKind) []domain.PieceKind {
	var secondary []domain.PieceKind
	for _, c := range p.Categories {
		k, ok := genreKinds[c]
		if ok && k != kind && !slices.Contains(kindCategories[kind], c) && !slices.Contains(secondary, k) {
			secondary = append(secondary, k)
		}
	}

	return secondary
}

// infoboxCategory is a category an infobox links to, at its position among the infobox's category links, with
// the part a qualifier scopes it to.
type infoboxCategory struct {
	position int
	part     *domain.PiecePart
}

// infoboxCategories returns the categories the infoboxes link to (read through redirect categories), first
// position first.
func infoboxCategories(boxes []Infobox, t Taxonomy) map[string]infoboxCategory {
	out := map[string]infoboxCategory{}
	for _, b := range boxes {
		for _, param := range b.Params {
			items := param.Value.Items
			for k, it := range items {
				if !isCategoryItem(it) {
					continue
				}
				target := t.Resolve(it.Target)
				if _, seen := out[target]; !seen {
					out[target] = infoboxCategory{position: len(out), part: itemPart(items, k)}
				}
			}
		}
	}

	return out
}

func isCategoryItem(it ValueItem) bool {
	return it.Type == ValueCategoryLink || it.Type == ValueCategoryTag
}

// itemPart is the part a text qualifier in parentheses scopes items[k] to: a qualifier of the item, or of the
// item the item itself qualifies.
func itemPart(items []ValueItem, k int) *domain.PiecePart {
	label := items[k].Label
	if items[k].Qualifies != "" {
		label = items[k].Qualifies
	}
	for _, q := range items {
		if q.Type != ValueText || q.Qualifies != label {
			continue
		}
		if part, ok := partQualifiers[strings.ToLower(q.Label)]; ok {
			return &part
		}
	}

	return nil
}

// sortedPieceCategories are a page's categories sorted by what they give a piece.
type sortedPieceCategories struct {
	terms     []PieceTerm
	statuses  []domain.ConservationStatus
	bardNames []string
}

// rankedTerm is a term of a piece with its facet and its rank: its infobox position, or after every infobox link
// in category order.
type rankedTerm struct {
	term  PieceTerm
	facet domain.Facet
	rank  int
}

// sortCategories sorts the page's categories into terms (affiliations, which classify persons, aside),
// conservation statuses (least severe first) and bard-name signals; the rest give nothing. Terms are ordered by the
// infobox and then by the categories; the first of each facet is primary.
func sortCategories(p Page, boxes []Infobox, s Site) sortedPieceCategories {
	boxCategories := infoboxCategories(boxes, s.Taxonomy)
	var out sortedPieceCategories
	var terms []rankedTerm
	for k, c := range p.Categories {
		category := s.Taxonomy.Resolve(c)
		if spec, ok := s.Terms.Term(category); ok && spec.Facet != domain.FacetAffiliation {
			if !slices.ContainsFunc(terms, func(t rankedTerm) bool { return t.term.Category == category }) {
				terms = append(terms, rankTerm(category, spec.Facet, len(boxCategories)+k, boxCategories))
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
	primary := map[domain.Facet]bool{}
	for _, t := range terms {
		t.term.IsPrimary = !primary[t.facet]
		primary[t.facet] = true
		out.terms = append(out.terms, t.term)
	}

	return out
}

func rankTerm(category string, facet domain.Facet, rank int, boxCategories map[string]infoboxCategory) rankedTerm {
	t := rankedTerm{term: PieceTerm{Category: category}, facet: facet, rank: rank}
	if at, ok := boxCategories[category]; ok {
		t.rank, t.term.Part = at.position, at.part
	}

	return t
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
