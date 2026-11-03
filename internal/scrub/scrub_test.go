package scrub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/mediawiki"
	"github.com/jbeshir/bardic-torch/internal/testutil/wikidumptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The synthetic people of these tests. "Wren Placeholder" is a bard (a page title, public); "Quill Fakename" is the
// bard's legal name, given in the H1, which the classifier hides.
const (
	bard      = "Wren Placeholder"
	legal     = "Quill Fakename"
	scaAlias  = "Wren of Examplemoor"
	firstName = "Legal Name 001"
	piece     = "Song of Testing"
	fernUser  = "Fern Handle"
)

var lastRev = time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)

func page(id int64, ns int, title, wikitext string, categories ...string) mediawiki.Page {
	return mediawiki.Page{
		NS: ns, Title: title, PageID: id, Categories: categories, HiddenCategories: []string{}, Wikitext: wikitext,
		URL: "https://wiki.example/wiki/" + strings.ReplaceAll(title, " ", "_"), RevID: id * 10,
		LastRevTimestamp: lastRev,
	}
}

func bardPage(wikitext string) mediawiki.Page {
	return page(1, mediawiki.NSMain, bard, "= "+bard+" ("+legal+", "+scaAlias+") =\n"+wikitext,
		mediawiki.CategoryBards)
}

func scrubbed(t *testing.T, d mediawiki.Dump) Result {
	t.Helper()
	res, err := Scrub(d)
	require.NoError(t, err)

	return res
}

func pageByID(t *testing.T, d mediawiki.Dump, id int64) mediawiki.Page {
	t.Helper()
	for _, p := range d.Pages {
		if p.PageID == id {
			return p
		}
	}
	t.Fatalf("no page %d", id)

	return mediawiki.Page{}
}

func TestScrub_ReplacesLegalAliasEverywhere(t *testing.T) {
	category := "Songs by " + legal
	d := mediawiki.Dump{
		Pages: []mediawiki.Page{
			bardPage("Also written FAKENAME alone, quill alone, quill_fakename or QuillFakename.\n" +
				"* [https://example.org/quill-fakename/home Site] and https://example.org/wiki/Quill_Fakename\n" +
				"* [https://quillfakename.example/ Shop]\n"),
			page(2, mediawiki.NSMain, piece, "Written by [["+legal+"]].\n[[Category:"+category+"]]\n", category),
			page(3, mediawiki.NSCategory, "Category:"+category, "Works.\n"),
		},
		Categories: map[string]mediawiki.Category{category: {
			Name: category, PageExists: true, Parents: []string{"Songs by person"}, Subcategories: []string{},
			Pages: []mediawiki.CategoryMember{{Title: piece}}, DescriptionWikitext: "By " + legal + ".",
		}},
		Redirects: []mediawiki.Redirect{{From: legal, NS: mediawiki.NSMain, To: bard}},
		Activity:  []mediawiki.PageActivity{{Title: "Category:" + category, NS: mediawiki.NSCategory, Revisions: 1}},
	}

	res := scrubbed(t, d)

	out := res.Dump
	bardText := pageByID(t, out, 1).Wikitext
	assert.Contains(t, bardText, "= "+bard+" ("+firstName+", "+scaAlias+") =")
	assert.Contains(t, bardText, "[https://example.org/legal-name-001/home Site]")
	assert.Contains(t, bardText, "https://example.org/wiki/Legal_Name_001")
	assert.Contains(t, bardText, "[https://legalname001.example/ Shop]")
	assert.Contains(t, bardText, "Also written FAKENAME alone, quill alone, Legal Name 001 or LegalName001.",
		"single words stay")
	wantCategory := "Songs by " + firstName
	pieceOut := pageByID(t, out, 2)
	assert.Equal(t, "Written by [["+firstName+"]].\n[[Category:"+wantCategory+"]]\n", pieceOut.Wikitext)
	assert.Equal(t, []string{wantCategory}, pieceOut.Categories)
	categoryPage := pageByID(t, out, 3)
	assert.Equal(t, "Category:"+wantCategory, categoryPage.Title)
	assert.Equal(t, "https://wiki.example/wiki/Category:Songs_by_Legal_Name_001", categoryPage.URL)
	require.Contains(t, out.Categories, wantCategory)
	assert.Equal(t, wantCategory, out.Categories[wantCategory].Name)
	assert.Equal(t, "By "+firstName+".", out.Categories[wantCategory].DescriptionWikitext)
	assert.Equal(t, []mediawiki.Redirect{{From: firstName, NS: mediawiki.NSMain, To: bard}}, out.Redirects)
	assert.Equal(t, "Category:"+wantCategory, out.Activity[0].Title)
	for _, form := range []string{legal, "quill_fakename", "quillfakename", "quill-fakename"} {
		requireNoText(t, res, form)
	}
}

// requireNoText fails if any text of the scrubbed dump holds s, in any case.
func requireNoText(t *testing.T, res Result, s string) {
	t.Helper()
	files, err := mediawiki.EncodeDump(res.Dump)
	require.NoError(t, err)
	for name, content := range files {
		require.NotContains(t, strings.ToLower(string(content)), strings.ToLower(s), name)
	}
}

func TestScrub_CategoryNamesThatBecomeEqualAreAnError(t *testing.T) {
	d := mediawiki.Dump{
		Pages: []mediawiki.Page{bardPage("")},
		Categories: map[string]mediawiki.Category{
			"Songs by " + legal: {Name: "Songs by " + legal}, "Songs by Quill_Fakename": {Name: "Songs by Quill_Fakename"},
		},
	}

	_, err := Scrub(d)

	require.ErrorIs(t, err, ErrPlaceholderCollision)
}

func TestScrub_LastWordBeforeFirstAndFirstAndLast(t *testing.T) {
	d := mediawiki.Dump{Pages: []mediawiki.Page{
		page(1, mediawiki.NSMain, bard, "= "+bard+" (Quill Middle Fakename) =\nIndexed as Fakename, Quill. "+
			"Signed Quill Fakename.\n", mediawiki.CategoryBards),
	}}

	text := pageByID(t, scrubbed(t, d).Dump, 1).Wikitext

	assert.Equal(t, "= "+bard+" ("+firstName+") =\nIndexed as "+firstName+". Signed "+firstName+".\n", text)
}

func TestScrub_HonorificSurname(t *testing.T) {
	d := mediawiki.Dump{Pages: []mediawiki.Page{
		bardPage("Thanks to Mr. Fakename, Dr Fakename and MRS FAKENAME.\n"),
		page(2, mediawiki.NSMain, "Ash Placeholder", "= Ash Placeholder (Ann Fakename-Otherly) =\n"+
			"Ask Ms. Fakename-Otherly or Mx. Otherly.\n", mediawiki.CategoryBards),
	}}

	out := scrubbed(t, d).Dump

	assert.Contains(t, pageByID(t, out, 1).Wikitext, "Thanks to Mr. "+firstName+", Dr "+firstName+" and MRS "+
		firstName+".")
	assert.Contains(t, pageByID(t, out, 2).Wikitext, "Ask Ms. Legal Name 002 or Mx. Legal Name 002.")
}

func TestScrub_UnknownHonorificGetsFreshPlaceholder(t *testing.T) {
	d := mediawiki.Dump{Pages: []mediawiki.Page{
		bardPage("Thanks to Mrs. Smithee and Mrs. Smithee again, and [[Dr. Who]], https://example.org/Mr.%20Smithee.\n"),
	}}

	res := scrubbed(t, d)

	text := pageByID(t, res.Dump, 1).Wikitext
	assert.Contains(t, text, "Thanks to Mrs. Legal Name 002 and Mrs. Legal Name 002 again, and [[Dr. Who]], "+
		"https://example.org/Mr.%20Smithee.", "the alias is 001; links and URLs are not prose")
	assert.Equal(t, 2, res.Stats.Honorifics)
}

func TestScrub_KeepsSCAAliasesAndTitles(t *testing.T) {
	d := mediawiki.Dump{Pages: []mediawiki.Page{
		page(1, mediawiki.NSMain, bard, "= "+bard+" ("+scaAlias+") =\nSung by "+bard+" and by Wren of Examplemoor.\n"+
			"Words by Ambra Michelli.\n", mediawiki.CategoryBards),
		page(2, mediawiki.NSMain, piece, "From the "+piece+" Collection (Song of Testing).\n"),
	}}

	res := scrubbed(t, d)

	for k, p := range res.Dump.Pages {
		assert.Equal(t, d.Pages[k], p)
	}
	for _, c := range res.Candidates {
		assert.False(t, c.Private, c.Text)
	}
}

func TestScrub_CandidatesFailClosed(t *testing.T) {
	d := mediawiki.Dump{Pages: []mediawiki.Page{
		bardPage("Tune by Ash Exampleton. Legal name: Rowan Testcase.\n" +
			"Recorded by Fern Samplewood of the Barony of Examplemoor.\n" +
			"* Recording by the author on YouTube\n" +
			"* Recording by Solohandle on YouTube\n"),
		page(2, mediawiki.NSMain, piece, "= "+piece+" (Poem) =\nArranged by "+bard+".\n"),
	}}

	res := scrubbed(t, d)

	var decisions []string
	for _, c := range res.Candidates {
		decisions = append(decisions, c.Rule+" "+c.Text+": "+string(c.Reason))
	}
	assert.ElementsMatch(t, []string{
		"h1_parenthetical " + legal + ": hidden_alias",
		"h1_parenthetical " + scaAlias + ": public_alias",
		"h1_parenthetical " + legal + ": denied",
		"h1_parenthetical " + scaAlias + ": public_alias",
		"by_from Ash Exampleton: unsure",
		"legal_name_marker Rowan Testcase: hidden_alias",
		"marker Rowan Testcase: legal_marker",
		"by_from Fern Samplewood of the Barony of Examplemoor: public_alias",
		"by_from_lead Fern Samplewood: unsure",
		"uploader Fern Samplewood: denied",
		"uploader the author: no_capital",
		"uploader Solohandle: unsure",
		"h1_parenthetical Poem: single_word",
		"by_from " + bard + ": page_title",
	}, decisions)
	text := pageByID(t, res.Dump, 1).Wikitext
	assert.Contains(t, text, "Tune by Legal Name 003. Legal name: Legal Name 002.")
	assert.Contains(t, text, "Recorded by Legal Name 004 of the Barony of Examplemoor.")
	assert.Contains(t, text, "* Recording by Legal Name 005 on YouTube")
}

func TestScrub_FreeTextBynameLeadIsDenied(t *testing.T) {
	d := mediawiki.Dump{Pages: []mediawiki.Page{
		page(1, mediawiki.NSMain, piece, "Performed by Ash Exampleton of Examplemoor.\n"),
	}}

	res := scrubbed(t, d)

	assert.Equal(t, "Performed by Legal Name 001 of Examplemoor.\n", pageByID(t, res.Dump, 1).Wikitext)
}

const (
	poemOf3  = "<poem>\nfirst real line\nsecond real line\n\nthird real line\n</poem>"
	threeOut = "<poem>\n" + LyricPlaceholderLine + "\n" + LyricPlaceholderLine + "\n" + LyricPlaceholderLine + "\n</poem>"
)

func TestScrub_PoemBodyPlaceholderKeepsThreshold(t *testing.T) {
	long := "<poem style=\"x\">\n" + strings.Repeat("a real line of the song\n", 60) + "</POEM>"
	d := mediawiki.Dump{Pages: []mediawiki.Page{
		page(1, mediawiki.NSMain, piece, "== Notes ==\n"+poemOf3+"\n== Words ==\n"+long+"\n"),
		page(2, mediawiki.NSMain, "Other Song", "== Notes ==\n"+poemOf3+"\n"),
		page(3, mediawiki.NSMain, "Unclosed Song", "== Words ==\n<poem>\none\ntwo\n== Notes ==\nkept\n"),
	}}

	res := scrubbed(t, d)

	four := strings.Repeat(LyricPlaceholderLine+"\n", 4)
	assert.Equal(t, "== Notes ==\n"+threeOut+"\n== Words ==\n<poem style=\"x\">\n"+four+"</POEM>\n",
		pageByID(t, res.Dump, 1).Wikitext)
	assert.Equal(t, "== Words ==\n<poem>\n"+strings.Repeat(LyricPlaceholderLine+"\n", 2)+"== Notes ==\nkept\n",
		pageByID(t, res.Dump, 3).Wikitext)
	for k, p := range d.Pages {
		assert.Equal(t, mediawiki.InlineLyricsBlocks(p.Wikitext).HadInlineLyrics,
			mediawiki.InlineLyricsBlocks(res.Dump.Pages[k].Wikitext).HadInlineLyrics, p.Title)
	}
	assert.True(t, mediawiki.InlineLyricsBlocks(res.Dump.Pages[0].Wikitext).HadInlineLyrics)
	assert.False(t, mediawiki.InlineLyricsBlocks(res.Dump.Pages[1].Wikitext).HadInlineLyrics)
	assert.Equal(t, 4, res.Stats.PoemBlocks)
	requireNoText(t, res, "real line")
}

func TestScrub_ProseLyricsBlock(t *testing.T) {
	text := "== Lyrics ==\nverse one goes here\nverse two goes here\n\nverse three goes here\nverse four\n" +
		"verse five\n* [https://example.org/lyrics Full lyrics]\n== Text ==\nA short note.\n"
	d := mediawiki.Dump{Pages: []mediawiki.Page{page(1, mediawiki.NSMain, piece, text)}}

	res := scrubbed(t, d)

	got := pageByID(t, res.Dump, 1).Wikitext
	assert.Equal(t, "== Lyrics ==\n"+strings.TrimSuffix(strings.Repeat(LyricPlaceholderLine+"\n", 4), "\n")+
		"\n* [https://example.org/lyrics Full lyrics]\n== Text ==\nA short note.\n", got)
	assert.True(t, mediawiki.InlineLyricsBlocks(got).HadInlineLyrics)
	assert.Equal(t, 1, res.Stats.ProseBlocks)
}

func TestScrub_CommentHoldingPoem(t *testing.T) {
	d := mediawiki.Dump{Pages: []mediawiki.Page{
		page(1, mediawiki.NSMain, piece, "Intro <!-- <poem>\nhidden real line\n</poem> --> outro\n"),
	}}

	res := scrubbed(t, d)

	assert.Equal(t, "Intro "+commentPlaceholder+" outro\n", pageByID(t, res.Dump, 1).Wikitext)
	assert.Equal(t, 1, res.Stats.CommentBlocks)
}

func TestScrub_EmailsAndPhones(t *testing.T) {
	d := mediawiki.Dump{Pages: []mediawiki.Page{
		page(1, mediawiki.NSMain, piece, "Mail quill@example.com or [mailto:Quill@Example.com me], "+
			"also ash.e+songs@mail.example.org.\nCall +1 (555) 010-9999 or +1 555.010.9999 but not 2008-10-22.\n"+
			"[https://www.scribd.com/document/348524873/x Scan] and [[Page 123456789012]]\n"),
	}}

	res := scrubbed(t, d)

	assert.Equal(t, "Mail email-001@example.invalid or [mailto:email-001@example.invalid me], also "+
		"email-002@example.invalid.\nCall 000-000-0001 or 000-000-0001 but not 2008-10-22.\n"+
		"[https://www.scribd.com/document/348524873/x Scan] and [[Page 123456789012]]\n",
		pageByID(t, res.Dump, 1).Wikitext)
	assert.Equal(t, 2, res.Stats.Emails)
	assert.Equal(t, 1, res.Stats.Phones)
}

func TestScrub_UsernamesOnlyInActivityAndUserLinks(t *testing.T) {
	d := mediawiki.Dump{
		Pages: []mediawiki.Page{
			bardPage("Edited by [[User:Fern Handle|Fern Handle]] ([[User talk:Fern Handle|talk]]) and " +
				"[[User:Other Editor/Sandbox]]. Not to be confused with " + bard + ".\n"),
		},
		Activity: []mediawiki.PageActivity{
			{Title: bard, NS: mediawiki.NSMain, Revisions: 3, Users: map[string]int{bard: 1, fernUser: 2}},
			{Title: "User:Fern Handle", NS: mediawiki.NSUser, Revisions: 1, Users: map[string]int{fernUser: 1}},
		},
	}

	res := scrubbed(t, d)

	assert.Contains(t, pageByID(t, res.Dump, 1).Wikitext, "Edited by [[User:Wiki User 01|Wiki User 01]] "+
		"([[User talk:Wiki User 01|Wiki User 01]]) and [[User:Wiki User 03]]. Not to be confused with "+bard+".")
	assert.Equal(t, []mediawiki.PageActivity{{
		Title: bard, NS: mediawiki.NSMain, Revisions: 3, Users: map[string]int{"Wiki User 01": 2, "Wiki User 02": 1},
	}}, res.Dump.Activity)
	assert.Equal(t, 3, res.Stats.WikiUsers)
}

func TestScrub_DropsUnneededNamespaces(t *testing.T) {
	d := mediawiki.Dump{
		Pages: []mediawiki.Page{
			page(1, mediawiki.NSMain, piece, "x"),
			page(2, mediawiki.NSUser, "User:Fern Handle", "about me"),
			page(3, mediawiki.NSTemplate, "Template:Infobox", "{{{1}}}"),
			page(4, mediawiki.NSCategory, "Category:Songs", "y"),
		},
		Categories: map[string]mediawiki.Category{"Songs": {
			Name: "Songs", Pages: []mediawiki.CategoryMember{
				{Title: piece}, {Title: "User:Fern Handle", NS: mediawiki.NSUser}, {Title: "Category:Sub", NS: 14},
			},
		}},
		Redirects: []mediawiki.Redirect{{From: "User:Old", NS: mediawiki.NSUser, To: "User:New"}},
	}

	res := scrubbed(t, d)

	var ids []int64
	for _, p := range res.Dump.Pages {
		ids = append(ids, p.PageID)
	}
	assert.Equal(t, []int64{1, 4}, ids)
	assert.Equal(t, []mediawiki.CategoryMember{{Title: piece}, {Title: "Category:Sub", NS: 14}},
		res.Dump.Categories["Songs"].Pages)
	assert.Empty(t, res.Dump.Redirects)
	assert.Equal(t, 2, res.Stats.PagesKept)
	assert.Equal(t, 2, res.Stats.PagesDropped)
}

// fullDump is a synthetic dump exercising every rule.
func fullDump() mediawiki.Dump {
	return mediawiki.Dump{
		Pages: []mediawiki.Page{
			page(7, mediawiki.NSMain, piece, "{{Infobox Song|author=[["+bard+"]]}}\n== Lyrics ==\n"+poemOf3+
				"\n== Notes ==\nAsk Mr. Smithee or mail quill@example.com. Tune by Ash Exampleton.\n"+
				"== Performances ==\n* Recording by Solohandle on YouTube [https://youtu.be/abc Video]\n"),
			bardPage("Thanks to Mr. Fakename. [[User:Fern Handle]] ~~~~\n"),
		},
		Categories: map[string]mediawiki.Category{"Bards": {Name: "Bards", Pages: []mediawiki.CategoryMember{{
			Title: bard,
		}}}},
		Redirects: []mediawiki.Redirect{{From: legal, NS: mediawiki.NSMain, To: bard}},
		Activity: []mediawiki.PageActivity{{Title: bard, NS: mediawiki.NSMain, Revisions: 1,
			Users: map[string]int{fernUser: 1}, First: lastRev, Last: lastRev}},
	}
}

func TestScrub_Deterministic(t *testing.T) {
	first, err := mediawiki.EncodeDump(scrubbed(t, fullDump()).Dump)
	require.NoError(t, err)
	for range 5 {
		again, err := mediawiki.EncodeDump(scrubbed(t, fullDump()).Dump)
		require.NoError(t, err)
		require.Equal(t, first, again)
	}
	firstList, secondList := scrubbed(t, fullDump()).Denylist.Encode(), scrubbed(t, fullDump()).Denylist.Encode()
	assert.Equal(t, firstList, secondList)
}

func TestScrub_Idempotent(t *testing.T) {
	once := scrubbed(t, fullDump())
	twice := scrubbed(t, once.Dump)

	assert.Equal(t, once.Dump, twice.Dump)
	assert.Zero(t, twice.Denylist.Len(), "placeholder lines are not lyrics")
	for _, c := range twice.Candidates {
		assert.False(t, c.Private, c.Text)
	}
}

func TestScrub_PreservesStructure(t *testing.T) {
	d := fullDump()
	res := scrubbed(t, d)

	require.Len(t, res.Dump.Pages, len(d.Pages))
	for _, after := range res.Dump.Pages {
		var before mediawiki.Page
		for _, p := range d.Pages {
			if p.PageID == after.PageID {
				before = p
			}
		}
		sectionsBefore, sectionsAfter := mediawiki.PageSections(before.Wikitext), mediawiki.PageSections(after.Wikitext)
		require.Len(t, sectionsAfter, len(sectionsBefore))
		for k := range sectionsBefore {
			assert.Equal(t, sectionsBefore[k].Level, sectionsAfter[k].Level)
			assert.Equal(t, mediawiki.KeyOf(sectionsBefore[k]), mediawiki.KeyOf(sectionsAfter[k]))
		}
		assert.Len(t, mediawiki.FindWikilinks(after.Wikitext), len(mediawiki.FindWikilinks(before.Wikitext)))
		assert.Len(t, mediawiki.FindExternalLinks(after.Wikitext), len(mediawiki.FindExternalLinks(before.Wikitext)))
		assert.Len(t, mediawiki.FindTemplates(after.Wikitext), len(mediawiki.FindTemplates(before.Wikitext)))
		assert.Equal(t, mediawiki.TypePage(before), mediawiki.TypePage(after))
	}
}

func TestPublicNamesArePublishedInPlan(t *testing.T) {
	plan, err := os.ReadFile(filepath.Join(wikidumptest.RepositoryRoot(t), "docs", "PLAN.md"))
	require.NoError(t, err)
	for _, name := range mediawiki.PublicNames {
		assert.Contains(t, string(plan), name)
	}
}
