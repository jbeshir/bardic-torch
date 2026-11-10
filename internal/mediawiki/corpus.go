package mediawiki

// Corpus is a dump parsed for import: its pages indexed (Site), the pieces, the persons and credits, and the links.
type Corpus struct {
	Dump   Dump
	Site   Site
	Pieces []PieceRecord
	People People
	Links  Links
}

// ParseCorpus parses a dump for import.
func ParseCorpus(d Dump) Corpus {
	s := NewSite(d)
	pieces := s.Pieces()
	people := ParsePeople(s, pieces)

	return Corpus{Dump: d, Site: s, Pieces: pieces, People: people, Links: ParseLinks(s, pieces, people)}
}
