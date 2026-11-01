package mediawiki

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// matchedTexts returns what each match covers in text.
func matchedTexts(text string, matches []NameMatch) []string {
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, text[m.Start:m.End])
	}

	return out
}

func TestNameMatcher_Forms(t *testing.T) {
	m := NewNameMatcher([]string{wren, "Rowan Q. O’Placeholder", "Ástrið Æsa Placeholder"})
	tests := map[string][]string{
		"by WREN placeholder.":                         {"WREN placeholder"},
		"wren\n  placeholder":                          {"wren\n  placeholder"},
		"https://example.org/wiki/Wren_Placeholder":    {"Wren_Placeholder"},
		"https://example.org/wren-placeholder.html":    {"wren-placeholder"},
		"wren.placeholder@example.invalid":             {"wren.placeholder"},
		"https://wrenplaceholder.example/":             {"wrenplaceholder"},
		"https://youtube.com/@WrenPlaceholderMusic":    {"WrenPlaceholder"},
		"Placeholder, Wren":                            nil,
		"Wren Placeholders":                            nil,
		"Rowan Q. O'Placeholder":                       {"Rowan Q. O'Placeholder"},
		"Rowan Q OPlaceholder":                         {"Rowan Q OPlaceholder"},
		"Rowan O’Placeholder and rowan oplaceholder":   {"Rowan O’Placeholder", "rowan oplaceholder"},
		"rowan-q-o-placeholder":                        {"rowan-q-o-placeholder"},
		rowan:                                          {rowan},
		"Astrid Aesa Placeholder, ástrið placeholder":  {"Astrid Aesa Placeholder", "ástrið placeholder"},
		"astridaesaplaceholder astridplaceholder":      {"astridaesaplaceholder", "astridplaceholder"},
		"no names here, only a wren and a placeholder": nil,
	}
	for text, want := range tests {
		assert.Equal(t, want, nilIfEmpty(matchedTexts(text, m.Find(text))), text)
	}
}

func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}

	return s
}

func TestNameMatcher_ReportsWhichName(t *testing.T) {
	m := NewNameMatcher([]string{ashFirst, wren, "Mr Placeholder"})
	text := "Ash met Mr. Placeholder and Wren Placeholder; ashes stay."
	got := m.Find(text)

	assert.Equal(t, []NameMatch{
		{Name: 0, Start: 0, End: 3},
		{Name: 2, Start: 8, End: 23},
		{Name: 1, Start: 28, End: 44},
	}, got)
}

func TestNameMatcher_LongestFirstAndNoOverlap(t *testing.T) {
	m := NewNameMatcher([]string{wrenFirst, wren, "Placeholder Wren"})
	text := "Wren Placeholder Wren"

	assert.Equal(t, []string{wren, wrenFirst}, matchedTexts(text, m.Find(text)))
}

func TestNameMatcher_ShortConcatenationsOnlyAsWholeWords(t *testing.T) {
	m := NewNameMatcher([]string{"Al Bo"})

	assert.Equal(t, []string{"albo"}, matchedTexts("see albo.example", m.Find("see albo.example")))
	assert.Empty(t, m.Find("the alboran sea"))
	assert.Empty(t, NewNameMatcher([]string{"  ", "--"}).Find("anything -- at all"))
}
