package mediawiki

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/wikidumptest"
)

// The name-run oracle checks the privacy classifier against the crawl independently of its candidate rules: every
// run of two or more capitalised words of a page's prose and link titles must be accounted for, so that a person's
// name written in a form no rule knows fails the gate instead of reaching the scrubbed copy or a public field.
// It finds runs with its own simple rules, not the classifier's.

var (
	// oracleURL is a URL, which the oracle does not read (URLs are never rewritten; the importer lists them).
	oracleURL = regexp.MustCompile(`(?i)\b(?:https?://|www\.)[^\s\]|<>"]+`)
	// oracleLinkTarget is the target of a wikilink, a page title or category; its label is read.
	oracleLinkTarget = regexp.MustCompile(`\[\[[^\]|\n]*`)
	// oraclePoem is a <poem> element's body, closed or not: lyrics, which no field holds.
	oraclePoem = regexp.MustCompile(`(?is)<poem\b[^>]*>.*?(?:</poem>|\z)`)
	// oracleLyricsHeading starts a Lyrics or Text section, whose lines without a link are lyrics.
	oracleLyricsHeading = regexp.MustCompile(`(?i)^=+\s*(?:lyrics|text)\s*=+\s*$`)
	// oracleWikiArticle is an external link to a wiki article and its label: a label that is the article's title
	// names a published identity (an encyclopedia or SCA wiki page), not a private person.
	oracleWikiArticle = regexp.MustCompile(`\[https?://[^\s\]]*(?:/wiki/|/index\.php/|title=)([^\s\]?#&]+)[ \t]+([^\]\n]+)\]`)
	// oracleHeading is any heading line.
	oracleHeading = regexp.MustCompile(`^=+[^=].*=+\s*$`)
	// oracleRun is two or more capitalised words, each pair joined by a space or by name particles.
	oracleRun = regexp.MustCompile(`\p{Lu}[\p{L}\p{M}'’.-]*(?:(?:[ \t]+(?:of|the|de|du|da|di|del|la|le|von|van|` +
		`ap|ab|ferch|verch|inghean|ingen|mac|nic|y))*[ \t]+\p{Lu}[\p{L}\p{M}'’.-]*)+`)
	// oracleWord is one word of a run.
	oracleWord = regexp.MustCompile(`\p{Lu}[\p{L}\p{M}'’.-]*`)
)

// oracleNonNameWords are words that are not part of a person's name in this wiki (folded): the wiki's own
// vocabulary, platforms, places and event words. A run is split at them. The list holds no personal name.
var oracleNonNameWords = setOf(
	// Headings, wiki and licence words.
	"key", "links", "link", "other", "pages", "page", "external", "resources", "resource", "categories",
	"category", "see", "also", "use", "for", "title", "name", "names", "piece", "pieces", "text", "lyrics",
	"history", "notes", "performances", "performance", "recordings", "recording", "licensing", "etiquette",
	"creative", "commons", "cc", "by-sa", "by-nc-sa", "url", "wiki", "mediawiki", "index", "directory", "list",
	"archive", "official", "preference", "preferences", "checklist", "instructions", "guide", "form", "forms",
	"rights", "firstname", "lastname",
	// Platforms and publications.
	"youtube", "google", "facebook", "spotify", "bandcamp", "soundcloud", "website", "site", "blog", "web",
	"songbook", "songbooks", "book", "books", "sheet", "sheets", "music", "cd", "album", "compendium",
	"miscellany", "collection", "project", "songs", "song", "words", "known",
	// The SCA and its kingdoms, events and ranks.
	"sca", "bardic", "bard", "bards", "college", "kingdom", "kingdoms", "barony", "crown", "war", "wars",
	"tourney", "pennsic", "gulf", "twelfth", "night", "coronation", "investiture", "baron", "baroness", "king",
	"queen", "prince", "princess", "master", "mistress", "duke", "duchess", "count", "countess", "sir", "lord",
	"lady", "dame", "laurel", "pelican", "chivalry", "knight", "knighthood", "herald", "heralds",
	"calontir", "midrealm", "northshield", "caid", "ansteorra", "ealdormere", "avacal", "lochac",
	"outlands", "aethelmearc", "meridies", "trimaris", "atenveldt", "artemisia", "drachenwald", "gleann",
	"abhann", "tir", "mara", "ri", "west", "east", "middle", "an", "seagirt", "atlantian", "ansteorran",
	"calontiri",
	// Sentence and title words.
	"the", "a", "of", "in", "on", "at", "to", "from", "and", "or", "i", "i'm", "we", "you", "us", "it", "be",
	"when", "not", "just", "can", "your", "my", "our", "this", "that", "thank", "later", "up", "new", "one",
	"another", "year", "day", "st", "sainte", "saint", "mr", "mrs", "ms", "dr",
)

// oracleNonNamePhrases are the runs of the crawl, reviewed by hand, that name no person (songs, books, albums,
// bands, websites, places, events, and legendary or fictional figures) and are not the title of an entity the
// parse finds. A run whose words are words of one of them is accounted for. The list holds no personal name (only
// two SCA given names inside list and contest titles): a
// run naming a person is classified by a candidate rule instead.
var oracleNonNamePhrases = []string{
	// Songs, poems, tunes and their quoted lines.
	"Mack the Knife", "Press Gang", "Great Silky", "Merry Rose", "Braes of Balquhither", "Amazing Grace",
	"Wear Chain Mail Knickers", "Volga Boatmen", "Happy Birthday", "Mongolian Birthday Song",
	"Vulgar Birthday Song", "Barbarian Birthday Dirge", "Barbarian Birthday Song", "Viking Birthday Dirge",
	"Viking Birthday Song", "Tears Away", "Rising Winds", "Warrior Never Dies", "Was Young", "Three Ravens",
	"Old Rosin the Beau", "Róisín the Bow", "Cruiscin Lan", "Sailin' Home", "Man Who Would Not Die",
	"Not-Ballad of Commius", "No Shit There I Was", "Daring Exploits of Knight McKnight", "Fighting Men",
	"Hidden Path", "Sacred Stone", "Perpetual Gift", "Green Eggs", "Joie de Vivre", "Porque Llorax", "El Cantar",
	"El Poems", "Chanson de Roland", "Human History", "Spoken Word", "Personal Stories", "Period Spellings",
	"Annual Birthday Bardic", "Her Stellar Highness Elashava's Bardic Challenge", "Liam's Favorite SCA Singers",
	"Mythical Creatures", "Creaturae Magicus", "Lone Penman", "Discussion of Born", "Why Create", "Hard Copy",
	"Second Edition", "Show Preview", "Ask First", "Sell Copies For Money", "Deceased October", "AS XXXVII",
	"Longtime SCAdian", "No Do", "April Fools' Day",
	// Albums, books, bands, groups and websites.
	"Iron Hart", "Nice Workes", "Harts Haven Music", "Raven's Cove", "Musica Subterranea", "Surreptitious Whiskey", "Dance Band",
	"Longest Johns", "Filk Yeah Filk", "Mainly Norfolk", "MidBards Listing", "Scribhinni Lasair",
	"MIT Mailing List", "Melodeon Forum", "Celtic Literature Collective", "Anthology of English Literature",
	"Performing Arts Guild", "Sheffield Hallam University", "Renaissance Dance", "Black Allemonde",
	// Places, periods, history and legend.
	"North Carolina", "South Carolina", "North Dakota", "South Dakota", "Rhode Island", "Nova Scotia",
	"Prince Edward Island", "Northern California", "Lower Peninsula of Michigan", "Elizabethan England",
	"Norman Britain", "Roman Britain", "Bronze Ages", "Multiple Culture", "Ossianic Cycle", "Berne Convention",
	"Battle of Roncevaux Pass", "Siege of Harlech Castle", "Holy Grail", "Knights of the Round Table",
	"Boyhood Deeds of Fionn", "Macgnímartha Finn", "Fionn Folklore", "Version of Fionn's", "Ireland's Myths", "Finn McCool",
}

func setOf(words ...string) map[string]bool {
	out := make(map[string]bool, len(words))
	for _, w := range words {
		out[w] = true
	}

	return out
}

// oracleText is the wikitext of a page with what the oracle does not read blanked, keeping offsets: URLs, link
// targets, <poem> bodies, the lines without a link of Lyrics and Text sections, and the label of a link to a wiki
// article that is the article's title.
func oracleText(wikitext string) string {
	b := []byte(wikitext)
	blank := func(start, end int) {
		for k := start; k < end; k++ {
			if b[k] != '\n' {
				b[k] = ' '
			}
		}
	}
	for _, re := range []*regexp.Regexp{oracleURL, oracleLinkTarget, oraclePoem} {
		for _, m := range re.FindAllStringIndex(wikitext, -1) {
			blank(m[0], m[1])
		}
	}
	for _, m := range oracleWikiArticle.FindAllStringSubmatchIndex(wikitext, -1) {
		article, err := url.PathUnescape(strings.ReplaceAll(wikitext[m[2]:m[3]], "_", " "))
		title, label := oracleWords(article), oracleWords(wikitext[m[4]:m[5]])
		if err == nil && len(title) == len(label) && equalWords(title, label) {
			blank(m[4], m[5])
		}
	}
	blankLyricLines(wikitext, blank)

	return string(b)
}

// blankLyricLines blanks the lines without a link of the Lyrics and Text sections of wikitext.
func blankLyricLines(wikitext string, blank func(start, end int)) {
	lyrics := false
	at := 0
	for _, line := range strings.SplitAfter(wikitext, "\n") {
		trimmed := strings.TrimRight(line, "\n")
		switch {
		case oracleLyricsHeading.MatchString(trimmed):
			lyrics = true
		case oracleHeading.MatchString(trimmed):
			lyrics = false
		case lyrics && !strings.Contains(line, "["):
			blank(at, at+len(line))
		}
		at += len(line)
	}
}

// oracleRunSpans are the spans of the runs of text to check: each capitalised run split at the non-name words
// (oracleNonNameWords and nonNames), with two or more words left.
func oracleRunSpans(text string, nonNames map[string]bool) [][]int {
	var out [][]int
	for _, run := range oracleRun.FindAllStringIndex(text, -1) {
		var words [][]int
		flush := func() {
			if len(words) >= 2 {
				out = append(out, []int{words[0][0], words[len(words)-1][1]})
			}
			words = nil
		}
		for _, w := range oracleWord.FindAllStringIndex(text[run[0]:run[1]], -1) {
			word := []int{run[0] + w[0], run[0] + w[1]}
			if w := oracleFold(text[word[0]:word[1]]); oracleNonNameWords[w] || nonNames[w] {
				flush()

				continue
			}
			words = append(words, word)
			if w := text[word[0]:word[1]]; strings.HasSuffix(w, ".") && len(w) > 2 {
				flush() // the end of a sentence, not an initial
			}
		}
		flush()
	}

	return out
}

// oracleFold is a word folded as the non-name list holds it: lower case, without a possessive "'s" or "'".
func oracleFold(word string) string {
	w := strings.ToLower(strings.TrimRight(word, ".-"))
	for _, suffix := range []string{"'s", "’s", "'", "’"} {
		w = strings.TrimSuffix(w, suffix)
	}

	return w
}

// TestResearchOracle_EveryNameRunClassified: every run of two or more capitalised words of the crawl's prose and
// link titles (oracleRunSpans) is a page title's or category's words, or every word of it lies in an occurrence
// of a name the classifier decided on (private or public, PrivateNames) or of a published name. A run that is
// neither is a name form no candidate rule sees. It reports page IDs and offsets only.
func TestResearchOracle_EveryNameRunClassified(t *testing.T) {
	dumpFS, _ := wikidumptest.Research(t)
	d, err := LoadDump(dumpFS)
	wikidumptest.NoError(t, "loading the crawl", err)
	c := ParseCorpus(d)

	decided := append([]string{}, PublicNames...)
	for _, dec := range c.Privacy {
		decided = append(decided, dec.Text)
	}
	matcher := domain.NewNameMatcher(nil, decided)
	titles := oracleTitles(d, c)
	// A word that is a category's whole name (a kingdom, a genre) is not part of a person's name either.
	nonNames := map[string]bool{}
	for name := range d.Categories {
		if words := oracleWords(strings.TrimPrefix(name, categoryPrefix)); len(words) == 1 {
			nonNames[words[0]] = true
		}
	}
	for _, phrase := range oracleNonNamePhrases {
		titles = append(titles, oracleWords(phrase))
	}
	for _, p := range d.Pages {
		if p.NS != NSMain && p.NS != NSCategory {
			continue
		}
		text := oracleText(p.Wikitext)
		covered := make([]bool, len(text))
		for _, m := range matcher.Find(text) {
			for k := m.Start; k < m.End; k++ {
				covered[k] = true
			}
		}
		for _, span := range oracleRunSpans(text, nonNames) {
			wikidumptest.Equal(t, wikidumptest.Location{PageID: p.PageID, Field: "unclassified name run",
				Offset: span[0]}, true, oracleRunClassified(text, span, covered, titles))
		}
	}
}

// oracleRunClassified reports whether the run at span is accounted for: its words outside decided names (covered)
// form no group of two or more consecutive words that is not words of a title.
func oracleRunClassified(text string, span []int, covered []bool, titles [][]string) bool {
	ok := true
	start, end, n := 0, 0, 0
	flush := func() {
		if n >= 2 && !wordsInTitles(oracleWords(text[start:end]), titles) {
			ok = false
		}
		n = 0
	}
	for _, w := range oracleWord.FindAllStringIndex(text[span[0]:span[1]], -1) {
		if covered[span[0]+w[0]] {
			flush()

			continue
		}
		if n == 0 {
			start = span[0] + w[0]
		}
		end = span[0] + w[1]
		n++
	}
	flush()

	return ok
}

// oracleTitles are the words of every name the wiki gives an entity that is not a person: page titles and
// categories, the pieces' alternate titles, the stub pieces, the events, the collections and the stub persons the
// parse finds.
func oracleTitles(d Dump, c Corpus) [][]string {
	var names []string
	for _, p := range d.Pages {
		names = append(names, strings.TrimPrefix(p.Title, categoryPrefix))
	}
	for name := range d.Categories {
		names = append(names, strings.TrimPrefix(name, categoryPrefix))
	}
	for _, p := range c.Pieces {
		for _, a := range p.Titles.Alternates {
			names = append(names, a.Text)
		}
	}
	for _, sp := range c.People.StubPieces {
		names = append(names, sp.Title)
	}
	for _, e := range c.Links.Events {
		names = append(names, e.Name)
	}
	for _, r := range c.Links.Collections.Records {
		names = append(names, r.Name())
	}
	// A name the wiki credits by itself (an infobox credit or a red link) is a public stub person, imported as
	// written and listed in the review (PLAN §7).
	for _, s := range c.People.Persons.Stubs {
		names = append(names, s.Name)
	}
	out := make([][]string, 0, len(names))
	for _, n := range names {
		out = append(out, oracleWords(n))
	}

	return out
}

// oracleWordPattern is a word as the oracle compares words: letters, marks and digits, with inner apostrophes.
var oracleWordPattern = regexp.MustCompile(`[\p{L}\p{M}\p{N}]+(?:['’][\p{L}\p{M}\p{N}]+)*['’]?`)

// oracleWords are the folded words of text (oracleFold).
func oracleWords(text string) []string {
	var out []string
	for _, w := range oracleWordPattern.FindAllString(text, -1) {
		out = append(out, oracleFold(w))
	}

	return out
}

func wordsInTitles(words []string, titles [][]string) bool {
	for _, title := range titles {
		for start := 0; start+len(words) <= len(title); start++ {
			if equalWords(title[start:start+len(words)], words) {
				return true
			}
		}
	}

	return false
}
