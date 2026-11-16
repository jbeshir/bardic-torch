package mediawiki

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseValue(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want referenceValue
	}{
		{
			name: emptyWord,
			raw:  "  <!-- nothing -->  ",
			want: referenceValue{},
		},
		{
			name: "category link with a parenthetical qualifier",
			raw:  "[[:Category:Patriotic|Patriotic]] [[:Category:Patriotic (Northshield)|(Northshield)]]",
			want: referenceValue{
				Raw: "[[:Category:Patriotic|Patriotic]] [[:Category:Patriotic (Northshield)|(Northshield)]]",
				Items: []referenceItem{
					{Type: ValueCategoryLink, Target: patriotic, Display: patriotic, Label: patriotic},
					{Type: ValueCategoryLink, Target: "Patriotic (Northshield)", Display: "(Northshield)",
						Label: "(Northshield)"},
				},
			},
		},
		{
			name: "verses and chorus",
			raw:  "[[Wren Placeholder]] (verses) / [[Ash Placeholder]] (chorus)",
			want: referenceValue{
				Raw: "[[Wren Placeholder]] (verses) / [[Ash Placeholder]] (chorus)",
				Items: []referenceItem{
					{Type: ValuePageLink, Target: wren, Label: wren},
					{Type: ValueText, Label: "verses", Qualifies: wren, Depth: 1},
					{Type: ValuePageLink, Target: ash, Label: ash},
					{Type: ValueText, Label: "chorus", Qualifies: ash, Depth: 1},
				},
				Separators: []string{"/"},
				FreeText:   "verses | chorus",
			},
		},
		{
			name: "link and anonymous",
			raw:  "[[Wren Placeholder]] & anonymous",
			want: referenceValue{
				Raw: "[[Wren Placeholder]] & anonymous",
				Items: []referenceItem{
					{Type: ValuePageLink, Target: wren, Label: wren},
					{Type: ValueText, Label: "anonymous"},
				},
				Separators: []string{"&"},
				FreeText:   "anonymous",
			},
		},
		{
			name: "unknown, possibly",
			raw:  "Unknown; possibly Wren Placeholder AND Ash With Placeholder",
			want: referenceValue{
				Raw: "Unknown; possibly Wren Placeholder AND Ash With Placeholder",
				Items: []referenceItem{
					{Type: ValueText, Label: "Unknown"},
					{Type: ValueText, Label: "possibly Wren Placeholder"},
					{Type: ValueText, Label: ashFirst},
					{Type: ValueText, Label: "Placeholder"},
				},
				Separators: []string{";", "and", "with"},
				FreeText:   "Unknown | possibly Wren Placeholder | Ash | Placeholder",
			},
		},
		{
			name: "link, probably",
			raw:  "[[Wren Placeholder|Wren]], probably",
			want: referenceValue{
				Raw: "[[Wren Placeholder|Wren]], probably",
				Items: []referenceItem{
					{Type: ValuePageLink, Target: wren, Display: wrenFirst, Label: wrenFirst},
					{Type: ValueText, Label: "probably"},
				},
				Separators: []string{","},
				FreeText:   "probably",
			},
		},
		{
			name: "external link, disputed",
			raw:  "[https://example.org/w Wren &amp; Co] (Disputed)",
			want: referenceValue{
				Raw: "[https://example.org/w Wren &amp; Co] (Disputed)",
				Items: []referenceItem{
					{Type: ValueExternalLink, URL: "https://example.org/w", Label: "Wren & Co"},
					{Type: ValueText, Label: "Disputed", Qualifies: "Wren & Co", Depth: 1},
				},
				FreeText: "Disputed",
			},
		},
		{
			name: "quotes, punctuation, line breaks, and an entity split at its semicolon as in the reference",
			raw:  "''Wren’s'' &amp; - <br />'''Ash'''",
			want: referenceValue{
				Raw: "''Wren’s'' &amp; - <br />'''Ash'''",
				Items: []referenceItem{
					{Type: ValueText, Label: "Wren’s &amp"},
					{Type: ValueText, Label: ashFirst},
				},
				Separators: []string{";", "<br>"},
				FreeText:   "Wren’s &amp | Ash",
			},
		},
		{
			name: "placeholder, interwiki and tag",
			raw:  "[[Category:TagGoesHere]] [[w:Thing]] [https://example.org/]",
			want: referenceValue{
				Raw:         "[[Category:TagGoesHere]] [[w:Thing]] [https://example.org/]",
				Placeholder: true,
				Items: []referenceItem{
					{Type: ValueCategoryTag, Target: "TagGoesHere", Label: "TagGoesHere"},
					{Type: ValueInterwiki, Target: "w:Thing", Label: "w:Thing"},
					{Type: ValueExternalLink, URL: "https://example.org/", Label: "https://example.org/"},
				},
			},
		},
		{
			name: "qualifier with no item before it",
			raw:  "(only) x",
			want: referenceValue{
				Raw:      "(only) x",
				Items:    []referenceItem{{Type: ValueText, Label: "only"}, {Type: ValueText, Label: "x"}},
				FreeText: "only | x",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, referenceParse(tt.raw))
		})
	}
}

func TestParseValue_DepthIsCapped(t *testing.T) {
	raw := "[[Wren Placeholder]] " + strings.Repeat("(", MaxNestingDepth+5) + "deep"
	got := referenceParse(raw)
	assert.Equal(t, MaxNestingDepth, got.Items[1].Depth)
}

// referenceValue is a value in the shape the reference's parseValue gives: ParseValue's
// value with the separators between its items (", ; / & and with <br>", lower case), its text items joined with
// " | " (FreeText), and whether it holds a template placeholder such as TagGoesHere.
type referenceValue struct {
	Raw         string
	Items       []referenceItem
	Separators  []string
	FreeText    string
	Placeholder bool
}

// referenceItem is a ValueItem with a link's Display and, for a qualifying item, its Depth: the number of open
// parentheses, at most MaxNestingDepth.
type referenceItem struct {
	Type      ValueItemType
	Target    string
	Display   string
	URL       string
	Label     string
	Qualifies string
	Depth     int
}

var referencePlaceholder = regexp.MustCompile(`(?i)TagGoesHere|TagNameHere|BardNameHere`)

// referenceParse is ParseValue(raw) as a referenceValue, reading what the reference adds from the value's tokens.
func referenceParse(raw string) referenceValue {
	v := ParseValue(raw)
	out := referenceValue{Raw: v.Raw, Placeholder: referencePlaceholder.MatchString(v.Raw)}
	depth, k, parent := 0, 0, false
	var free []string
	for _, tk := range tokenizeValue(v.Raw) {
		switch tk.kind {
		case tokenOpen:
			depth++

			continue
		case tokenClose:
			depth = max(0, depth-1)

			continue
		case tokenSeparator:
			out.Separators = append(out.Separators, tk.text)

			continue
		case tokenText, tokenWikilink, tokenExternal:
		}
		if _, ok := valueItem(tk); !ok {
			continue
		}
		it := v.Items[k]
		k++
		ref := referenceItem{Type: it.Type, Target: it.Target, URL: it.URL, Label: it.Label, Qualifies: it.Qualifies}
		if tk.kind == tokenWikilink {
			ref.Display = FindWikilinks(tk.text)[0].Display
		}
		if depth > 0 && parent {
			ref.Depth = min(depth, MaxNestingDepth)
		} else {
			parent = true
		}
		if it.Type == ValueText {
			free = append(free, it.Label)
		}
		out.Items = append(out.Items, ref)
	}
	out.FreeText = strings.Join(free, " | ")

	return out
}
