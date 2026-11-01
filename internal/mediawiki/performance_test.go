package mediawiki

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const testMaxYear = 2026

type performanceSummary struct {
	Uploader   string
	Platform   string
	Performers []string
	Event      string
	Year       int
	Piece      string
}

func summarise(item string) performanceSummary {
	p := ParsePerformanceText(item, testMaxYear)
	s := performanceSummary{Uploader: p.Uploader.Text, Platform: p.Platform, Event: p.Event.Text, Year: p.Year,
		Piece: p.Piece}
	for _, l := range p.Performers {
		s.Performers = append(s.Performers, l.Target)
	}
	if p.Uploader.Text != "" && item[p.Uploader.Start:p.Uploader.End] != p.Uploader.Text {
		s.Uploader = "span mismatch"
	}

	return s
}

func TestParsePerformanceText_FullGrammar(t *testing.T) {
	tests := map[string]performanceSummary{
		"[https://example.org/v/2014/1 Recording by Wren Placeholder on YouTube] of [[Ash Placeholder]] singing " +
			"[[A Song]] at Pennsic War, 2019": {
			Uploader: wren, Platform: youTube, Performers: []string{ash},
			Event: "Pennsic War", Year: 2019, Piece: aSong,
		},
		"[https://example.org/v A Song, recorded by WrenSings on YouTube] performed by [[Ash Placeholder]] and " +
			"[[Rowan Placeholder]] at the [[Green Dragon]], 2010": {
			Uploader: "WrenSings", Platform: youTube, Performers: []string{ash, rowan},
			Event: "Green Dragon", Year: 2010,
		},
		"[https://example.org/v A Song, uploaded to YouTube by Wren Placeholder, sung in choir in 2010]": {
			Uploader: wren, Platform: youTube, Year: 2010,
		},
		"[https://example.org/v A Song, recorded for YouTube by Wren Placeholder] sung by [[Ash Placeholder]]": {
			Uploader: wren, Platform: youTube, Performers: []string{ash},
		},
		"[https://example.org/v A Song on YouTube by 'Wren Placeholder'] at Twelfth Night 2012": {
			Uploader: wren, Platform: youTube, Event: "Twelfth Night", Year: 2012,
		},
		"[https://example.org/v A Song by Wren Placeholder on BandCamp]": {
			Uploader: wren, Platform: "BandCamp",
		},
		"[https://example.org/v A Song] recorded by Wren Placeholder, at Crown Tourney 1999": {
			Uploader: wren, Event: "Crown Tourney", Year: 1999,
		},
		"[https://example.org/v A Song] performed by [[Ash Placeholder]] to a tune by [[Rowan Placeholder]]": {
			Performers: []string{ash},
		},
		"[https://example.org/v A Song] sung by [[Ash Placeholder]], [[Rowan Placeholder]] & [[Wren Placeholder]]": {
			Performers: []string{ash, rowan, wren},
		},
		"[https://example.org/v A Song sung by Ash on his lute]": {},
	}
	for item, want := range tests {
		assert.Equal(t, want, summarise(item), item)
	}
}

func TestParsePerformanceText_YearBounds(t *testing.T) {
	for item, want := range map[string]int{
		"[https://example.org/2019/v A Song] in 1965":           0,
		"[https://example.org/v A Song] in 1966":                1966,
		"[https://example.org/v A Song] in 2027":                0,
		"[https://example.org/v A Song] in 2026, again in 2020": 2026,
		"[https://example.org/v A Song] at Event 45 in 12345":   0,
	} {
		assert.Equal(t, want, ParsePerformanceText(item, testMaxYear).Year, item)
	}
}

func TestParsePerformanceText_NoEvent(t *testing.T) {
	p := ParsePerformanceText("[https://example.org/at/v A Song] of [[Ash Placeholder]], 2020", testMaxYear)
	assert.Equal(t, TextSpan{}, p.Event)
	assert.Equal(t, TextSpan{}, ParsePerformanceText("at 2020", testMaxYear).Event)
	assert.Equal(t, TextSpan{}, p.Uploader)
	assert.Empty(t, p.Platform)
}

func TestParsePerformanceText_EventSpan(t *testing.T) {
	item := "[https://example.org/v A Song] at 2009 Spring Revel - a video"
	p := ParsePerformanceText(item, testMaxYear)
	assert.Equal(t, "Spring Revel", p.Event.Text)
	assert.Equal(t, p.Event.Text, item[p.Event.Start:p.Event.End])
	assert.Equal(t, 2009, p.Year)
}
