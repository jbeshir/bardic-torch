package mediawiki

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormTitle(t *testing.T) {
	for in, want := range map[string]string{
		"":                        "",
		"wren_placeholder":        "Wren placeholder",
		"  a   b  c ":             "A b c",
		"&amp;x":                  "&x",
		"élan vital":              "Élan vital",
		"Wren Placeholder&#39;s":  "Wren Placeholder's",
		"lasair inghen ui h’airt": "Lasair inghen ui h’airt",
	} {
		assert.Equal(t, want, NormTitle(in), in)
	}
}

func TestDecodeEntities(t *testing.T) {
	for in, want := range map[string]string{
		"a &amp; b":          "a & b",
		"&LT;x&gt;":          "<x>",
		"&#8217;&#x2019;":    "’’",
		"&nbsp;&hellip;":     " …",
		"&unknown;":          "&unknown;",
		"&#1114112;":         "&#1114112;",
		"&#xD800;":           "&#xD800;",
		"&#38;amp;":          "&",
		"&amp;#39;":          "&#39;",
		"plain text, no ref": "plain text, no ref",
	} {
		assert.Equal(t, want, DecodeEntities(in), in)
	}
}

func TestMaskCommentsAndNowiki_KeepOffsets(t *testing.T) {
	text := "a<!-- x\ny -->b<nowiki>[[L]]</nowiki>c<!-- open"
	masked := MaskNowiki(MaskComments(text))

	require.Len(t, masked, len(text))
	assert.Equal(t, "a"+strings.Repeat(" ", 12)+"b"+strings.Repeat(" ", 22)+"c<!-- open", masked)
	assert.Equal(t, "ab<nowiki>[[L]]</nowiki>c<!-- open", StripComments(text))
}

func TestFindWikilinks(t *testing.T) {
	links := FindWikilinks("[[wren placeholder]] [[Category:Songs]] [[:Category:Sad|sad ones]] [[File:x.png|thumb]] " +
		"[[:File:x.png]] [[w:MediaWiki]] [[User:Example]] [[Page#Part|shown]] [[#Lyrics]] [[ :  Category : Bards ]] " +
		"[[Bardic Torch:About]] [[Page|]] [[a [[b]] c]]")
	type got struct {
		Kind     WikilinkKind
		Target   string
		Display  string
		Piped    bool
		NS       int
		Fragment string
		Colon    bool
	}
	var gots []got
	for _, l := range links {
		gots = append(gots, got{l.Kind, l.Target, l.Display, l.Piped, l.NS, l.Fragment, l.LeadingColon})
	}
	assert.Equal(t, []got{
		{WikilinkPage, "Wren placeholder", "", false, 0, "", false},
		{WikilinkCategoryTag, "Songs", "", false, NSCategory, "", false},
		{WikilinkCategoryLink, "Sad", "sad ones", true, NSCategory, "", true},
		{WikilinkFile, "X.png", "thumb", true, NSFile, "", false},
		{WikilinkFileLink, "X.png", "", false, NSFile, "", true},
		{WikilinkInterwiki, "w:MediaWiki", "", false, 0, "", false},
		{WikilinkNamespace, "Example", "", false, NSUser, "", false},
		{WikilinkPage, "Page", "shown", true, 0, "Part", false},
		{WikilinkPage, "", "", false, 0, lyricsHeading, false},
		{WikilinkCategoryLink, CategoryBards, "", false, NSCategory, "", true},
		{WikilinkNamespace, "About", "", false, NSProject, "", false},
		{WikilinkPage, "Page", "", true, 0, "", false},
		{WikilinkPage, "B", "", false, 0, "", false},
	}, gots)
	assert.Empty(t, links[11].Label(), "an empty pipe shows an empty label")
	assert.Equal(t, "B", links[12].Label())
	first := links[0]
	assert.Equal(t, "[[wren placeholder]]", first.Raw)
	assert.Equal(t, 0, first.Start)
	assert.Equal(t, len(first.Raw), first.End)
}

func TestFindExternalLinks(t *testing.T) {
	text := "[https://example.org/a Some text ] [//example.org/b] [www.example.org/c text] " +
		"see https://example.org/d. and (http://example.org/e), x=https://example.org/f " +
		`"https://example.org/g" 'https://example.org/h' /https://example.org/i [https://example.org/j] ` +
		"ahttps://example.org/k HTTPS://EXAMPLE.ORG/L| [mailto:x] https:// " +
		"[http://example.org/m http://example.org/n]"
	links := FindExternalLinks(text)
	type got struct {
		URL       string
		Text      string
		Bracketed bool
	}
	var gots []got
	for _, l := range links {
		gots = append(gots, got{l.URL, l.Text, l.Bracketed})
		assert.Equal(t, l.URL, text[l.Start : l.Start+len(l.URL)+boolInt(l.Bracketed)][boolInt(l.Bracketed):])
	}
	assert.Equal(t, []got{
		{"https://example.org/a", "Some text", true},
		{"//example.org/b", "", true},
		{"www.example.org/c", "text", true},
		{"https://example.org/d", "", false},
		{"http://example.org/e", "", false},
		{"https://example.org/j", "", true},
		{"HTTPS://EXAMPLE.ORG/L", "", false},
		{"http://example.org/m", "http://example.org/n", true},
	}, gots)
}

func boolInt(b bool) int {
	if b {
		return 1
	}

	return 0
}

func TestFindExternalLinks_BareURLAfterEqualsOrQuote(t *testing.T) {
	for in, want := range map[string]int{
		"=https://example.org/":   0,
		`"https://example.org/`:   0,
		"'https://example.org/":   0,
		"[https://example.org/":   0,
		"/https://example.org/":   0,
		"_https://example.org/":   0,
		"= https://example.org/":  1,
		"é https://example.org/":  1,
		"éhttps://example.org/":   1,
		"(https://example.org/)":  1,
		"https://example.org/...": 1,
	} {
		assert.Len(t, FindExternalLinks(in), want, in)
	}
}

func TestFindTemplates_Nested(t *testing.T) {
	text := "x {{Infobox song\n| title = A {{small|B}}\n| lyrics_by = [[Wren Placeholder|W|P]]\n| = empty\n" +
		"| positional\n| x = a = b\n}} {{{param}}} {{Other}} {{unclosed|a"
	templates := FindTemplates(text)
	require.Len(t, templates, 3)

	box := templates[0]
	assert.Equal(t, "Infobox song", box.Name)
	assert.Equal(t, text[box.Start:box.End], box.Raw)
	assert.True(t, strings.HasSuffix(box.Raw, "}}"))
	assert.Equal(t, []TemplateParam{
		{Name: titleParam, Value: "A {{small|B}}", Raw: " title = A {{small|B}}\n"},
		{Name: "lyrics_by", Value: "[[Wren Placeholder|W|P]]", Raw: " lyrics_by = [[Wren Placeholder|W|P]]\n"},
		{Name: "", Value: emptyWord, Raw: " = empty\n"},
		{Name: "1", Value: "positional", Raw: " positional\n"},
		{Name: "x", Value: "a = b", Raw: " x = a = b\n"},
	}, box.Params)
	assert.Equal(t, "Other", templates[1].Name)
	assert.Equal(t, "unclosed", templates[2].Name, "an unclosed call runs to the end, as in the reference")
	assert.Equal(t, len(text), templates[2].End)
}

func TestFindTemplates_NestingCap(t *testing.T) {
	deep := strings.Repeat("{{a|", MaxNestingDepth) + strings.Repeat("}}", MaxNestingDepth)
	require.Len(t, FindTemplates(deep), 1, "nesting up to the cap is parsed")

	tooDeep := strings.Repeat("{{a|", MaxNestingDepth+1) + strings.Repeat("}}", MaxNestingDepth+1)
	templates := FindTemplates(tooDeep)
	require.NotEmpty(t, templates)
	assert.Positive(t, templates[0].Start, "the call nested past the cap is skipped")
}

func TestSplitTopLevel(t *testing.T) {
	assert.Equal(t, []string{"a", " [[b|c]] ", " {{d|e}}", ""}, SplitTopLevel("a| [[b|c]] | {{d|e}}|", '|'))
	assert.Equal(t, []string{"]] a", " b"}, SplitTopLevel("]] a| b", '|'), "a stray closer does not go negative")
}

func TestSections_Levels(t *testing.T) {
	text := "lead\n= Title =\n\n\n== Lyrics ==  \ntext\n=== Sub ===\n==Bad\nnot heading == x\n" +
		"== a = b ==\n=x=\n====\n======= seven =======\n== Last =="
	type got struct {
		Level   int
		Heading string
		Body    string
	}
	var gots []got
	for _, s := range Sections(text) {
		gots = append(gots, got{s.Level, s.Heading, s.Body(text)})
	}
	assert.Equal(t, []got{
		{0, "", "lead\n"},
		{1, "Title", "\n"},
		{2, lyricsHeading, "\ntext\n"},
		{3, "Sub", "\n==Bad\nnot heading == x\n"},
		{2, "a = b", "\n"},
		{1, "x", "\n"},
		{2, "", "\n"},
		{6, "= seven =", "\n"},
		{2, "Last", ""},
	}, gots)
}

func TestSections_NoHeadings(t *testing.T) {
	assert.Equal(t, []Section{{End: 4}}, Sections("text"))
	assert.Equal(t, []Section{{}}, Sections(""))
	assert.Equal(t, []Section{{}, {Level: 1, Heading: "", Start: 0, BodyStart: 2, End: 2}}, Sections("=="))
}

func TestPlain(t *testing.T) {
	for in, want := range map[string]string{
		"[[Page|Shown]] and [[Other]]":      "Shown and Other",
		"[https://example.org/ the site] x": "the site x",
		"[https://example.org/]":            "",
		"'''bold''' ''it'' <br/>&amp; ":     "bold it & ",
		"  <span class=\"a\">x</span>  ":    "x",
	} {
		assert.Equal(t, strings.TrimSpace(want), Plain(in), in)
	}
}
