package mediawiki

// Corpus is a dump parsed for import: its pages indexed (Site), the pieces, the persons and credits, the links,
// and the private names no public text may hold (PrivateNames, decided on the pages without their inline lyrics),
// with every decision taken on a possible person name.
type Corpus struct {
	Dump         Dump
	Site         Site
	Pieces       []PieceRecord
	People       People
	Links        Links
	PrivateNames []PrivateName
	Privacy      []PrivacyDecision
}

// ParseCorpus parses a dump for import.
func ParseCorpus(d Dump) Corpus {
	s := NewSite(d)
	pieces := s.Pieces()
	people := ParsePeople(s, pieces)
	pages := ContentPages(d.Pages)
	for k, p := range pages {
		pages[k].Wikitext = WithoutInlineLyrics(p.Wikitext, InlineLyricsBlocks(p.Wikitext).Blocks)
	}
	names, decisions := PrivateNames(pages, d.Redirects)

	return Corpus{Dump: d, Site: s, Pieces: pieces, People: people, Links: ParseLinks(s, pieces, people),
		PrivateNames: names, Privacy: decisions}
}
