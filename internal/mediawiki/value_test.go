package mediawiki

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseValue(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want Value
	}{
		{
			name: emptyWord,
			raw:  "  <!-- nothing -->  ",
			want: Value{},
		},
		{
			name: "category link with a parenthetical qualifier",
			raw:  "[[:Category:Patriotic|Patriotic]] [[:Category:Patriotic (Northshield)|(Northshield)]]",
			want: Value{
				Raw: "[[:Category:Patriotic|Patriotic]] [[:Category:Patriotic (Northshield)|(Northshield)]]",
				Items: []ValueItem{
					{Type: ValueCategoryLink, Target: patriotic, Display: patriotic, Label: patriotic},
					{Type: ValueCategoryLink, Target: "Patriotic (Northshield)", Display: "(Northshield)",
						Label: "(Northshield)"},
				},
			},
		},
		{
			name: "verses and chorus",
			raw:  "[[Wren Placeholder]] (verses) / [[Ash Placeholder]] (chorus)",
			want: Value{
				Raw: "[[Wren Placeholder]] (verses) / [[Ash Placeholder]] (chorus)",
				Items: []ValueItem{
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
			want: Value{
				Raw: "[[Wren Placeholder]] & anonymous",
				Items: []ValueItem{
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
			want: Value{
				Raw: "Unknown; possibly Wren Placeholder AND Ash With Placeholder",
				Items: []ValueItem{
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
			want: Value{
				Raw: "[[Wren Placeholder|Wren]], probably",
				Items: []ValueItem{
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
			want: Value{
				Raw: "[https://example.org/w Wren &amp; Co] (Disputed)",
				Items: []ValueItem{
					{Type: ValueExternalLink, URL: "https://example.org/w", Label: "Wren & Co"},
					{Type: ValueText, Label: "Disputed", Qualifies: "Wren & Co", Depth: 1},
				},
				FreeText: "Disputed",
			},
		},
		{
			name: "quotes, punctuation, line breaks, and an entity split at its semicolon as in the reference",
			raw:  "''Wren’s'' &amp; - <br />'''Ash'''",
			want: Value{
				Raw: "''Wren’s'' &amp; - <br />'''Ash'''",
				Items: []ValueItem{
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
			want: Value{
				Raw:         "[[Category:TagGoesHere]] [[w:Thing]] [https://example.org/]",
				Placeholder: true,
				Items: []ValueItem{
					{Type: ValueCategoryTag, Target: "TagGoesHere", Label: "TagGoesHere"},
					{Type: ValueInterwiki, Target: "w:Thing", Label: "w:Thing"},
					{Type: ValueExternalLink, URL: "https://example.org/", Label: "https://example.org/"},
				},
			},
		},
		{
			name: "qualifier with no item before it",
			raw:  "(only) x",
			want: Value{
				Raw:      "(only) x",
				Items:    []ValueItem{{Type: ValueText, Label: "only"}, {Type: ValueText, Label: "x"}},
				FreeText: "only | x",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseValue(tt.raw)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.want.Raw == "", got.Empty())
		})
	}
}

func TestParseValue_DepthIsCapped(t *testing.T) {
	raw := "[[Wren Placeholder]] " + strings.Repeat("(", MaxNestingDepth+5) + "deep"
	got := ParseValue(raw)
	assert.Equal(t, MaxNestingDepth, got.Items[1].Depth)
}
